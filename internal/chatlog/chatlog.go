// Package chatlog ships per-chat-call telemetry to Cloudflare R2 as
// gzipped JSONL. One event per upstream completion captures: who,
// when, model + operator chosen, upstream + upstream model, status,
// timings, token counts (parsed from the final SSE usage chunk),
// tool calls, and full prompt + response bodies.
//
// Events are buffered in memory and flushed every 60s or 5MB,
// whichever comes first. On crash we lose at most one window — chat
// requests are not blocked on flush.
//
// R2 config is env-driven so the same binary runs locally (no
// R2_BUCKET → logger is a no-op) and on the server.
package chatlog

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Event is the per-chat-call record. Field order chosen so JSONL is
// readable when piped through a tail.
type Event struct {
	TS              string `json:"ts"`
	PromptID        string `json:"prompt_id,omitempty"`
	UserID          string `json:"user_id"`

	Model           string `json:"model"`
	Operator        string `json:"operator,omitempty"`
	Upstream        string `json:"upstream"`
	UpstreamModel   string `json:"upstream_model"`

	Status          int    `json:"status"`
	TTFBMs          int64  `json:"ttfb_ms"`
	DurationMs      int64  `json:"duration_ms"`

	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	CachedTokens     int `json:"cached_tokens,omitempty"`

	CostCredits int `json:"cost_credits"`
	ToolCalls   int `json:"tool_calls,omitempty"`

	RequestBody  json.RawMessage `json:"request_body,omitempty"`
	ResponseBody string          `json:"response_body,omitempty"`

	Error string `json:"error,omitempty"`
}

// Config is populated from environment at startup.
type Config struct {
	Endpoint    string // e.g. https://<acct>.r2.cloudflarestorage.com
	Bucket      string
	Region      string // R2 ignores; default "auto"
	AccessKey   string
	SecretKey   string
	Prefix      string        // optional path prefix inside the bucket
	UploadEvery time.Duration // optional override; defaults to 60s when zero
}

// FromEnv reads R2_* env vars. Returns nil if R2_BUCKET is empty so
// callers can skip Init in local dev.
//
// Accepts inference-api's name scheme (R2_API_KEY/R2_API_SECRET,
// R2_UPLOAD_EVERY) so the server's existing env block ports over with
// only the prefix change. The AWS-standard names are also honoured for
// fresh deployments.
func FromEnv() *Config {
	bucket := os.Getenv("R2_BUCKET")
	if bucket == "" {
		return nil
	}
	access := firstNonEmpty(os.Getenv("R2_API_KEY"), os.Getenv("R2_ACCESS_KEY_ID"))
	secret := firstNonEmpty(os.Getenv("R2_API_SECRET"), os.Getenv("R2_SECRET_ACCESS_KEY"))
	c := &Config{
		Endpoint:  os.Getenv("R2_ENDPOINT"),
		Bucket:    bucket,
		Region:    os.Getenv("R2_REGION"),
		AccessKey: access,
		SecretKey: secret,
		Prefix:    strings.Trim(os.Getenv("R2_PREFIX"), "/"),
	}
	if c.Region == "" {
		c.Region = "auto"
	}
	if every := os.Getenv("R2_UPLOAD_EVERY"); every != "" {
		if d, err := time.ParseDuration(every); err == nil {
			c.UploadEvery = d
		}
	}
	return c
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Logger buffers events and ships them to R2.
type Logger struct {
	cfg    *Config
	client *s3.Client

	mu      sync.Mutex
	buf     []Event
	bufSize int // approximate bytes (sum of marshaled sizes), used for size-based flush
	podID   string
	seq     uint64

	flushBytes int           // hard size trigger
	flushEvery time.Duration // wall-clock trigger
	stopCh     chan struct{}
}

const (
	defaultFlushBytes = 5 * 1024 * 1024
	defaultFlushEvery = 60 * time.Second
)

// New initialises the logger. Returns nil + nil error when cfg is nil
// (logger disabled) so callers can pass the result straight through.
func New(ctx context.Context, cfg *Config) (*Logger, error) {
	if cfg == nil {
		return nil, nil
	}
	if cfg.Bucket == "" || cfg.Endpoint == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("chatlog: incomplete R2 config (need R2_ENDPOINT, R2_BUCKET, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY)")
	}
	creds := credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
	client := s3.New(s3.Options{
		Region:       cfg.Region,
		Credentials:  creds,
		BaseEndpoint: aws.String(cfg.Endpoint),
		UsePathStyle: true,
	})
	pod := make([]byte, 4)
	_, _ = rand.Read(pod)
	flushEvery := defaultFlushEvery
	if cfg.UploadEvery > 0 {
		flushEvery = cfg.UploadEvery
	}
	l := &Logger{
		cfg:        cfg,
		client:     client,
		podID:      hex.EncodeToString(pod),
		flushBytes: defaultFlushBytes,
		flushEvery: flushEvery,
		stopCh:     make(chan struct{}),
	}
	go l.loop(ctx)
	log.Printf("chatlog: R2 enabled bucket=%s endpoint=%s pod=%s", cfg.Bucket, cfg.Endpoint, l.podID)
	return l, nil
}

// Log appends an event. Safe to call from any goroutine. Triggers an
// async flush when the buffer crosses the size threshold.
func (l *Logger) Log(e Event) {
	if l == nil {
		return
	}
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	// Rough size estimate — full marshal is exact but expensive; the
	// length of bodies dominates so this is close enough.
	approx := len(e.RequestBody) + len(e.ResponseBody) + 256
	l.mu.Lock()
	l.buf = append(l.buf, e)
	l.bufSize += approx
	overflow := l.bufSize >= l.flushBytes
	l.mu.Unlock()
	if overflow {
		go l.flush(context.Background())
	}
}

func (l *Logger) loop(ctx context.Context) {
	t := time.NewTicker(l.flushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			l.flush(context.Background())
			return
		case <-l.stopCh:
			l.flush(context.Background())
			return
		case <-t.C:
			l.flush(ctx)
		}
	}
}

// Stop drains the buffer and uploads a final object. Safe to call on
// nil receivers (no-op).
func (l *Logger) Stop() {
	if l == nil {
		return
	}
	close(l.stopCh)
}

func (l *Logger) flush(ctx context.Context) {
	l.mu.Lock()
	if len(l.buf) == 0 {
		l.mu.Unlock()
		return
	}
	events := l.buf
	l.buf = nil
	l.bufSize = 0
	l.seq++
	seq := l.seq
	l.mu.Unlock()

	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			log.Printf("chatlog: encode failed: %v", err)
		}
	}
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	if _, err := gw.Write(raw.Bytes()); err != nil {
		log.Printf("chatlog: gzip write failed: %v", err)
		return
	}
	if err := gw.Close(); err != nil {
		log.Printf("chatlog: gzip close failed: %v", err)
		return
	}

	now := time.Now().UTC()
	key := fmt.Sprintf("chat-events/%04d/%02d/%02d/%02d/%s-%d-%d.jsonl.gz",
		now.Year(), now.Month(), now.Day(), now.Hour(),
		l.podID, now.Unix(), seq)
	if l.cfg.Prefix != "" {
		key = l.cfg.Prefix + "/" + key
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := l.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:          aws.String(l.cfg.Bucket),
		Key:             aws.String(key),
		Body:            bytes.NewReader(gz.Bytes()),
		ContentType:     aws.String("application/jsonl+gzip"),
		ContentEncoding: aws.String("gzip"),
	})
	if err != nil {
		// Don't drop events on failure — push them back to the head of
		// the buffer so the next flush retries. If this keeps failing
		// the buffer will grow until it bursts the process; that is
		// preferable to silently losing telemetry.
		l.mu.Lock()
		l.buf = append(events, l.buf...)
		for i := range events {
			l.bufSize += len(events[i].RequestBody) + len(events[i].ResponseBody) + 256
		}
		l.mu.Unlock()
		log.Printf("chatlog: PutObject failed key=%s err=%v (requeued %d events)", key, err, len(events))
		return
	}
	log.Printf("chatlog: flushed %d events to s3://%s/%s (gz=%d bytes)", len(events), l.cfg.Bucket, key, gz.Len())
}
