// Package crypto wraps AES-256-GCM for at-rest encryption of upstream
// API keys. The 32-byte key comes from one of two env vars:
//
//   CREDITS_UPSTREAM_KEY_SECRET  (base64, decodes to exactly 32 bytes)
//     Explicit override. Use this if you want the upstream-key
//     encryption to rotate independently of inter-service auth.
//
//   INTERNAL_SHARED_SECRET  (any non-empty string)
//     Fallback. We SHA-256 this to derive a 32-byte AES key so you
//     don't need a second env var. Trade-off: rotating
//     INTERNAL_SHARED_SECRET also rotates the encryption key, which
//     means every encrypted upstream key in the DB becomes unreadable
//     until re-encrypted. For most deploys that's fine because the
//     shared secret rarely rotates; if you do plan to rotate it,
//     set CREDITS_UPSTREAM_KEY_SECRET explicitly.
//
// Format: 12-byte GCM nonce prepended to ciphertext, base64-wrapped.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

const (
	envExplicit = "CREDITS_UPSTREAM_KEY_SECRET"
	envFallback = "INTERNAL_SHARED_SECRET"
)

func loadKey() ([]byte, error) {
	if raw := os.Getenv(envExplicit); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, errors.New(envExplicit + " is not valid base64")
		}
		if len(key) != 32 {
			return nil, errors.New(envExplicit + " must decode to 32 bytes (use: openssl rand -base64 32)")
		}
		return key, nil
	}
	// Fallback: derive a deterministic 32-byte key from the inter-service
	// shared secret. Sufficient strength when the shared secret is itself
	// strong (high-entropy hex/base64), which is the convention.
	if raw := os.Getenv(envFallback); raw != "" {
		sum := sha256.Sum256([]byte(raw))
		return sum[:], nil
	}
	return nil, errors.New("neither " + envExplicit + " nor " + envFallback + " is set; cannot encrypt/decrypt upstream keys")
}

// Encrypt returns base64(nonce || ciphertext) for the given plaintext.
func Encrypt(plaintext string) (string, error) {
	key, err := loadKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nil, nonce, []byte(plaintext), nil)
	out := append(nonce, ct...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt reverses Encrypt. Returns an error if the input is not valid
// base64 or the auth tag does not verify.
func Decrypt(blob string) (string, error) {
	key, err := loadKey()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// Mask returns "•••• " + last 4 chars for display.
func Mask(s string) string {
	if len(s) <= 4 {
		return "••••"
	}
	return "•••• " + s[len(s)-4:]
}
