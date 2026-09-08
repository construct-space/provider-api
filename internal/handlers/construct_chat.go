package handlers

// POST /api/inference/v1/chat/completions (and legacy aliases) — chat
// dispatcher for Construct picker entries.
// GET  /api/construct/usage — current user's balance.
//
// Auth contract: gateway must have signed the request (X-Internal-Secret +
// X-Auth-User-ID). Without those, 401.
//
// Routed dispatch (Phase 2):
//   1. Resolve picker entry by model id.
//   2. Read picker_entry.route_via_operator → load source_family_routes
//      for that operator, ordered primary first then backups by position.
//   3. For each route: resolve routing_target + upstream provider,
//      build upstream OpenAI request, POST.
//        - Network err / 5xx → log + try next route.
//        - 4xx (incl. 429) → commit to this route, surface as-is.
//        - 2xx → debit (one-time per prompt_id), set
//          X-Construct-Operator / X-Construct-Routing-Target /
//          X-Construct-Upstream headers, stream body to client +
//          tee into the R2 chat-event log.
//   4. All routes failed → 502.
//
// MoA dispatch (Morpheus, aggregator + proposers) is Phase 3 — returns
// 501 with a clear reason when the chosen operator is MoA-shaped.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"construct/provider/internal/chatlog"
	"construct/provider/internal/crypto"
	"construct/provider/internal/database"
	"construct/provider/internal/gwauth"
	"construct/provider/internal/models"

	"gorm.io/gorm"
)

// chatLogger is the process-wide R2 telemetry sink. Set once from
// main.go after chatlog.New; nil means logging is disabled.
var chatLogger *chatlog.Logger //nolint:gochecknoglobals // process-wide sink

// SetChatLogger wires the R2 chat-event logger. Called once at boot.
func SetChatLogger(l *chatlog.Logger) { chatLogger = l }

// gatewayUserID returns the gateway-asserted user id, or "" if the
// request did not come through the gateway. Same predicate used by
// every other session-authed endpoint.
func gatewayUserID(r *http.Request) string {
	id := gwauth.Gateway(r)
	if id == nil {
		return ""
	}
	return id.UserID
}

// ─── Debit ────────────────────────────────────────────────────────────────

// debitResult captures the row returned by the admission UPDATE so the
// handler can include "credits remaining" in the response headers.
type debitResult struct {
	DailyUsed    int
	PaidBalance  int
	DailyAllowance int
}

// debit runs the admission predicate + balance mutation atomically.
// Returns (result, true) on success, (zero, false) on insufficient
// credits / blocked / construct disabled.
//
// The single UPDATE handles daily reset, daily-then-paid fallback, and
// the blocked check in one round trip.
func debit(ctx context.Context, userID string, cost int, dailyAllowance int) (debitResult, bool, error) {
	// Ensure a balance row exists. INSERT ON CONFLICT DO NOTHING is
	// idempotent and concurrent-safe.
	if err := database.DB.WithContext(ctx).Exec(`
		INSERT INTO credits_balance (user_id, daily_used, daily_date, paid_balance, blocked, updated_at)
		VALUES (?, 0, (now() AT TIME ZONE 'UTC')::date, 0, false, now())
		ON CONFLICT (user_id) DO NOTHING
	`, userID).Error; err != nil {
		return debitResult{}, false, err
	}

	var out debitResult
	rows, err := database.DB.WithContext(ctx).Raw(`
		WITH today AS (SELECT (now() AT TIME ZONE 'UTC')::date AS d)
		UPDATE credits_balance b
		SET
		  daily_used   = CASE
		    WHEN b.daily_date < t.d THEN LEAST(?::int, ?::int)
		    ELSE LEAST(b.daily_used + ?::int, ?::int)
		  END,
		  daily_date   = t.d,
		  paid_balance = b.paid_balance - GREATEST(
		    ?::int - (
		      CASE WHEN b.daily_date < t.d THEN ?::int
		           ELSE ?::int - b.daily_used
		      END
		    ), 0),
		  updated_at   = now()
		FROM today t
		WHERE b.user_id = ?
		  AND b.blocked = false
		  AND (
		    CASE WHEN b.daily_date < t.d THEN ?::int
		         ELSE ?::int - b.daily_used
		    END + b.paid_balance
		  ) >= ?::int
		RETURNING b.daily_used, b.paid_balance
	`,
		cost, dailyAllowance,                            // first CASE
		cost, dailyAllowance,                            // ELSE branch
		cost, dailyAllowance, dailyAllowance,            // paid_balance arithmetic
		userID,
		dailyAllowance, dailyAllowance, cost,            // WHERE admission
	).Rows()
	if err != nil {
		return debitResult{}, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return debitResult{}, false, nil // admission denied
	}
	if err := rows.Scan(&out.DailyUsed, &out.PaidBalance); err != nil {
		return debitResult{}, false, err
	}
	out.DailyAllowance = dailyAllowance
	return out, true, nil
}

// ─── Chat handler ─────────────────────────────────────────────────────────

// chatRequestBody captures only the fields we need to read; everything
// else gets re-serialized verbatim into the upstream request.
type chatRequestBody struct {
	Model  string `json:"model"`
	Stream *bool  `json:"stream,omitempty"`
	// Tier picks which operator chain to walk for routed picker entries.
	// Optional. Values: "large" | "medium" | "small". Omitted or
	// unrecognised → medium (the existing route_via_operator column).
	// The field is consumed here, not forwarded to the upstream — strip
	// before re-serializing the body.
	Tier string `json:"tier,omitempty"`
}

// ChatConstruct — POST /api/inference/v1/chat/completions
func ChatConstruct(w http.ResponseWriter, r *http.Request) {
	userID := gatewayUserID(r)
	if userID == "" {
		WriteJSON(w, 401, map[string]any{"error": "unauthorized"})
		return
	}

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		WriteJSON(w, 400, map[string]any{"error": "read body: " + err.Error()})
		return
	}
	var head chatRequestBody
	if err := json.Unmarshal(raw, &head); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	if head.Model == "" {
		WriteJSON(w, 400, map[string]any{"error": "model required"})
		return
	}

	var entry models.ConstructPickerEntry
	if err := database.DB.First(&entry, "id = ?", head.Model).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "unknown model: " + head.Model})
		return
	}
	if !entry.Enabled {
		WriteJSON(w, 503, map[string]any{"error": "model disabled"})
		return
	}

	cfg := loadOrInitConfig()
	if !cfg.Enabled {
		WriteJSON(w, 503, map[string]any{"error": "construct disabled"})
		return
	}

	// Resolve operator from the requested tier. "medium" (or omitted)
	// always uses the picker entry's main route; "large" and "small"
	// override when the corresponding column is set. Unknown tiers
	// fall back to medium so a future tier name doesn't break callers.
	tier := strings.ToLower(strings.TrimSpace(head.Tier))
	operatorID := strings.TrimSpace(entry.RouteViaOperator)
	switch tier {
	case "large":
		if v := strings.TrimSpace(entry.RouteViaOperatorLarge); v != "" {
			operatorID = v
		}
	case "small":
		if v := strings.TrimSpace(entry.RouteViaOperatorSmall); v != "" {
			operatorID = v
		}
	}
	if operatorID == "" {
		operatorID = "tank"
	}

	// MoA operators (Morpheus) carry aggregator + proposer slots
	// instead of primary + backup. Phase 3 ships the fan-out + synthesis;
	// for now refuse with a clear reason so callers know it's a known gap.
	var moaCount int64
	database.DB.Model(&models.SourceFamilyRoute{}).
		Where("operator_id = ? AND slot IN ?", operatorID, []string{"aggregator", "proposer"}).
		Count(&moaCount)
	if moaCount > 0 {
		WriteJSON(w, 501, map[string]any{
			"error":    "MoA dispatch not yet implemented (Phase 3)",
			"operator": operatorID,
		})
		return
	}

	// Single-tier chain: primary first, then backups by position.
	var routes []models.SourceFamilyRoute
	database.DB.
		Where("operator_id = ? AND slot IN ?", operatorID, []string{"primary", "backup"}).
		Order("CASE slot WHEN 'primary' THEN 0 ELSE 1 END ASC, position ASC").
		Find(&routes)
	if len(routes) == 0 {
		WriteJSON(w, 502, map[string]any{
			"error":    "no routes configured for operator",
			"operator": operatorID,
		})
		return
	}

	promptID := r.Header.Get("X-Construct-Prompt-Id")

	for _, route := range routes {
		var target models.ConstructRoutingTarget
		if err := database.DB.First(&target, "id = ?", route.ModelID).Error; err != nil {
			log.Printf("dispatch: operator=%s slot=%s position=%d unresolved model_id=%s",
				operatorID, route.Slot, route.Position, route.ModelID)
			continue
		}
		if !target.Enabled {
			continue
		}
		var upstream models.ConstructUpstreamProvider
		if err := database.DB.First(&upstream, "id = ?", target.UpstreamProviderID).Error; err != nil {
			log.Printf("dispatch: target=%s unresolved upstream_provider_id=%s", target.ID, target.UpstreamProviderID)
			continue
		}
		if !upstream.Enabled {
			continue
		}

		slotLabel := operatorID + "/" + route.Slot
		if route.Slot == "backup" {
			slotLabel = fmt.Sprintf("%s/backup #%d", operatorID, route.Position+1)
		}

		committed := attemptDispatch(w, r, userID, raw, entry, operatorID, slotLabel, target, upstream, promptID, cfg)
		if committed {
			return
		}
	}

	WriteJSON(w, 502, map[string]any{
		"error":    "all routes failed",
		"operator": operatorID,
	})
}

// attemptDispatch tries one routing target. Returns true if the
// response was committed to the client (status written, possibly body
// streamed) — caller must not write further. Returns false on
// fall-through-eligible failures (network err, upstream 5xx before
// stream started).
//
// Important: nothing about the response is sent to the client until
// we know the upstream returned a non-5xx status. That's what makes
// the fallback safe — we never half-stream a primary then switch to
// a backup.
func attemptDispatch(
	w http.ResponseWriter, r *http.Request,
	userID string, raw []byte, entry models.ConstructPickerEntry, operatorID, slotLabel string,
	target models.ConstructRoutingTarget, upstream models.ConstructUpstreamProvider,
	promptID string, cfg models.ConstructConfig,
) bool {
	apiKey, err := crypto.Decrypt(upstream.APIKeyEncrypted)
	if err != nil {
		log.Printf("dispatch: decrypt upstream=%s err=%v", upstream.ID, err)
		return false
	}

	// Rewrite the request body for this target: swap model id, cap
	// max_tokens, inject thinking config.
	var bodyMap map[string]any
	if err := json.Unmarshal(raw, &bodyMap); err != nil {
		// Caller's body is malformed; this is a 400, not a route failure.
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return true
	}
	bodyMap["model"] = target.UpstreamModel
	bodyMap["max_tokens"] = target.MaxOutputTokensPerCredit
	delete(bodyMap, "tier") // Construct-internal hint; upstream doesn't know it.

	// ThinkingMode → reasoning_effort + (optionally) extra_body.thinking.
	// `extra_body.thinking` is a DeepSeek-shape extension that OpenRouter
	// passes through; Fireworks/Together's strict OpenAI validators
	// reject unknown top-level fields. Only inject extra_body for
	// upstreams known to accept it. reasoning_effort is OpenAI-canonical
	// (o-series, gpt-oss) and silently ignored by upstreams that don't
	// read it, so it's always safe to set.
	mode := strings.ToLower(strings.TrimSpace(target.ThinkingMode))
	acceptsExtraBody := upstreamAcceptsExtraBody(upstream.ID)
	switch mode {
	case "low", "medium", "high":
		bodyMap["reasoning_effort"] = mode
		if acceptsExtraBody {
			bodyMap["extra_body"] = map[string]any{"thinking": map[string]any{"type": "enabled"}}
		}
	case "off", "disabled":
		if acceptsExtraBody {
			bodyMap["extra_body"] = map[string]any{"thinking": map[string]any{"type": "disabled"}}
		}
		// On strict upstreams (Fireworks/Together) we silently drop
		// "off" — the upstream's own default behaviour stands in.
	}
	upstreamBody, _ := json.Marshal(bodyMap)

	upURL := strings.TrimRight(upstream.BaseURL, "/") + "/chat/completions"
	upReq, err := http.NewRequestWithContext(r.Context(), "POST", upURL, bytes.NewReader(upstreamBody))
	if err != nil {
		log.Printf("dispatch: build upstream req upstream=%s err=%v", upstream.ID, err)
		return false
	}
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", "text/event-stream")
	switch upstream.AuthHeader {
	case "x-api-key":
		upReq.Header.Set("x-api-key", apiKey)
	default:
		upReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	// Forward the gateway-authenticated user id to the upstream so
	// per-user accounting + audit can happen there (tinker proxy
	// requires this; other upstreams ignore unknown headers).
	if userID != "" {
		upReq.Header.Set("X-Construct-User-Id", userID)
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	tReqStart := time.Now()
	resp, err := client.Do(upReq)
	if err != nil {
		// Network error → eligible for fallback.
		emitChatEvent(promptID, userID, entry, slotLabel, target, upstream,
			502, 0, time.Since(tReqStart), upstreamBody, nil, err.Error())
		return false
	}
	if resp.StatusCode >= 500 {
		// Upstream 5xx → drain body for the log, then fall through.
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		emitChatEvent(promptID, userID, entry, slotLabel, target, upstream,
			resp.StatusCode, 0, time.Since(tReqStart), upstreamBody, body,
			fmt.Sprintf("upstream %d", resp.StatusCode))
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	// From here we commit: status code + headers go to the client and
	// fallback is no longer possible.

	// Debit. Per-prompt dedup so agent fan-out doesn't double-charge.
	var balance debitResult
	alreadyDebited := false
	if promptID != "" {
		var n int64
		database.DB.Model(&models.CreditsLedger{}).
			Where("user_id = ? AND prompt_id = ? AND kind = ?", userID, promptID, "debit_prompt").
			Count(&n)
		alreadyDebited = n > 0
	}
	if alreadyDebited {
		var bal models.CreditsBalance
		_ = database.DB.First(&bal, "user_id = ?", userID).Error
		balance = debitResult{
			DailyUsed:      bal.DailyUsed,
			PaidBalance:    bal.PaidBalance,
			DailyAllowance: cfg.DailyAllowance,
		}
	} else {
		r2, ok, derr := debit(r.Context(), userID, target.CreditsPerPrompt, cfg.DailyAllowance)
		if derr != nil {
			WriteJSON(w, 500, map[string]any{"error": "debit: " + derr.Error()})
			return true
		}
		if !ok {
			var bal models.CreditsBalance
			_ = database.DB.First(&bal, "user_id = ?", userID).Error
			if bal.Blocked {
				WriteJSON(w, 403, map[string]any{"error": "user blocked", "reason": bal.BlockedReason})
				return true
			}
			WriteJSON(w, 402, map[string]any{
				"error":           "insufficient credits",
				"daily_used":      bal.DailyUsed,
				"daily_allowance": cfg.DailyAllowance,
				"paid_balance":    bal.PaidBalance,
				"required":        target.CreditsPerPrompt,
			})
			return true
		}
		balance = r2
	}

	// Surface routing decision in response headers so the desktop can
	// render "via apoc/backup #1 → openrouter/deepseek-v4-pro" under the
	// assistant message.
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Construct-Operator", operatorID)
	w.Header().Set("X-Construct-Routing-Target", target.ID)
	w.Header().Set("X-Construct-Upstream", upstream.ID+"/"+target.UpstreamModel)
	w.Header().Set("X-Construct-Routing-Slot", slotLabel)
	w.Header().Set("X-Construct-Credits-Daily-Used", fmt.Sprintf("%d", balance.DailyUsed))
	w.Header().Set("X-Construct-Credits-Daily-Allowance", fmt.Sprintf("%d", balance.DailyAllowance))
	w.Header().Set("X-Construct-Credits-Paid-Balance", fmt.Sprintf("%d", balance.PaidBalance))
	w.WriteHeader(resp.StatusCode)

	// Write the ledger row before streaming so the debit is durable
	// even if the client disconnects mid-stream. Only on first dispatch
	// for this prompt_id.
	if !alreadyDebited {
		ledgerMeta, _ := json.Marshal(map[string]any{
			"picker_entry":     entry.ID,
			"operator":         entry.RouteViaOperator,
			"routing_target":   target.ID,
			"slot":             slotLabel,
			"upstream":         upstream.ID,
			"upstream_model":   target.UpstreamModel,
			"cost":             target.CreditsPerPrompt,
			"upstream_status":  resp.StatusCode,
		})
		_ = database.DB.Transaction(func(tx *gorm.DB) error {
			return tx.Create(&models.CreditsLedger{
				UserID:    userID,
				Delta:     -target.CreditsPerPrompt,
				Kind:      "debit_prompt",
				PromptID:  promptID,
				Meta:      string(ledgerMeta),
				CreatedAt: time.Now(),
			}).Error
		})
	}

	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	var captured bytes.Buffer
	var ttfb time.Duration
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if ttfb == 0 {
				ttfb = time.Since(tReqStart)
			}
			captured.Write(buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				emitChatEvent(promptID, userID, entry, slotLabel, target, upstream,
					resp.StatusCode, ttfb, time.Since(tReqStart), upstreamBody, captured.Bytes(),
					"client disconnected")
				return true
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr == io.EOF {
			emitChatEvent(promptID, userID, entry, slotLabel, target, upstream,
				resp.StatusCode, ttfb, time.Since(tReqStart), upstreamBody, captured.Bytes(), "")
			return true
		}
		if rerr != nil {
			emitChatEvent(promptID, userID, entry, slotLabel, target, upstream,
				resp.StatusCode, ttfb, time.Since(tReqStart), upstreamBody, captured.Bytes(),
				rerr.Error())
			return true
		}
	}
}

// upstreamAcceptsExtraBody returns true when the upstream's chat
// endpoint allows non-OpenAI top-level fields ("extra_body" carrying
// DeepSeek-shape thinking config). OpenRouter is a passthrough so it
// accepts anything the downstream model accepts; DeepSeek + Moonshot
// + Z.AI + Qwen all use the extra_body shape natively. Fireworks +
// Together + Mistral run strict OpenAI validators that reject unknown
// fields.
//
// Conservative default for unknown upstreams: do not inject. Add new
// upstream ids here as we verify them.
func upstreamAcceptsExtraBody(upstreamID string) bool {
	switch upstreamID {
	case "openrouter", "deepseek", "moonshot", "zai", "qwen":
		return true
	default:
		return false
	}
}

// emitChatEvent ships one R2 chat-event capturing the routing decision
// and outcome. Safe to call when the logger is nil.
func emitChatEvent(
	promptID, userID string,
	entry models.ConstructPickerEntry, slotLabel string,
	target models.ConstructRoutingTarget, upstream models.ConstructUpstreamProvider,
	status int, ttfb, total time.Duration, reqBody, respBody []byte, errStr string,
) {
	if chatLogger == nil {
		return
	}
	promptTok, completionTok, cachedTok, toolCalls := parseUsage(respBody)
	chatLogger.Log(chatlog.Event{
		PromptID:         promptID,
		UserID:           userID,
		Model:            entry.ID,
		Operator:         slotLabel,
		Upstream:         upstream.ID,
		UpstreamModel:    target.UpstreamModel,
		Status:           status,
		TTFBMs:           ttfb.Milliseconds(),
		DurationMs:       total.Milliseconds(),
		PromptTokens:     promptTok,
		CompletionTokens: completionTok,
		CachedTokens:     cachedTok,
		CostCredits:      target.CreditsPerPrompt,
		ToolCalls:        toolCalls,
		RequestBody:      json.RawMessage(reqBody),
		ResponseBody:     string(respBody),
		Error:            errStr,
	})
}

// parseUsage walks the captured upstream body and pulls token counts +
// tool-call count. Works for both SSE streams (scans `data: {...}`
// chunks, takes the last one with a `usage` field) and one-shot JSON
// responses (single object with `usage` at the top level).
func parseUsage(body []byte) (promptTok, completionTok, cachedTok, toolCalls int) {
	var lastUsage map[string]any
	var seenToolCalls int

	type chunk struct {
		Usage   map[string]any `json:"usage"`
		Choices []struct {
			Delta struct {
				ToolCalls []any `json:"tool_calls"`
			} `json:"delta"`
			Message struct {
				ToolCalls []any `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}

	tryParse := func(data []byte) {
		data = bytes.TrimSpace(data)
		if len(data) == 0 || data[0] != '{' {
			return
		}
		var c chunk
		if err := json.Unmarshal(data, &c); err != nil {
			return
		}
		if c.Usage != nil {
			lastUsage = c.Usage
		}
		for _, ch := range c.Choices {
			seenToolCalls += len(ch.Delta.ToolCalls) + len(ch.Message.ToolCalls)
		}
	}

	// One-shot JSON case: whole body is a single object.
	tryParse(body)

	// SSE case: scan `data: ...` lines.
	for line := range bytes.SplitSeq(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		tryParse(payload)
	}

	if lastUsage != nil {
		promptTok = intFromAny(lastUsage["prompt_tokens"])
		completionTok = intFromAny(lastUsage["completion_tokens"])
		if pd, ok := lastUsage["prompt_tokens_details"].(map[string]any); ok {
			cachedTok = intFromAny(pd["cached_tokens"])
		}
		if cachedTok == 0 {
			cachedTok = intFromAny(lastUsage["cached_tokens"])
		}
	}
	return promptTok, completionTok, cachedTok, seenToolCalls
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	}
	return 0
}

// ─── Usage handler ────────────────────────────────────────────────────────

// UsageConstruct — GET /api/construct/usage
func UsageConstruct(w http.ResponseWriter, r *http.Request) {
	userID := gatewayUserID(r)
	if userID == "" {
		WriteJSON(w, 401, map[string]any{"error": "unauthorized"})
		return
	}
	cfg := loadOrInitConfig()

	// Read current balance, applying lazy daily reset for display purposes.
	var bal models.CreditsBalance
	if err := database.DB.First(&bal, "user_id = ?", userID).Error; err == gorm.ErrRecordNotFound {
		bal = models.CreditsBalance{UserID: userID, DailyDate: time.Now().UTC()}
	}
	dailyUsed := bal.DailyUsed
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if bal.DailyDate.Before(today) {
		dailyUsed = 0
	}

	WriteJSON(w, 200, map[string]any{
		"daily_used":       dailyUsed,
		"daily_allowance":  cfg.DailyAllowance,
		"paid_balance":     bal.PaidBalance,
		"reset_at":         today.Add(24 * time.Hour).Format(time.RFC3339),
		"blocked":          bal.Blocked,
		"blocked_reason":   bal.BlockedReason,
		"construct_enabled": cfg.Enabled,
	})
}
