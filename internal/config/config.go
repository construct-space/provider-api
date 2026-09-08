// Package config loads provider-api's runtime configuration from
// environment variables. CapRover sets these via the app's
// Environment Variables panel; .env is honoured locally for parity
// with sibling services but production reads env only.
package config

import (
	"bufio"
	"os"
	"strings"
)

// Config is the provider-api runtime config.
type Config struct {
	Port           string
	AppURL         string
	AllowedOrigins []string

	// Postgres `credits` DB (lives on srv-captain--postgres-db
	// alongside marketplace).
	DBHost string
	DBPort string
	DBName string
	DBUser string
	DBPass string

	// INTERNAL_SHARED_SECRET — gateway trust + oracle control-plane.
	InternalSecret string

	// Symmetric key used to encrypt upstream API keys at rest in
	// credits_upstream_keys.api_key_encrypted. 32-byte base64.
	UpstreamKeySecret string

	// Peers
	OracleURL string
}

func Load() *Config {
	loadEnvFile(".env")

	origins := splitCSV(env(
		"ALLOWED_ORIGINS",
		"https://my.lisaos.dev,tauri://localhost",
	))

	return &Config{
		Port:           env("PORT", "8000"),
		AppURL:         env("APP_URL", "http://localhost:8000"),
		AllowedOrigins: origins,

		DBHost: env("DB_HOST", "srv-captain--postgres-db"),
		DBPort: env("DB_PORT", "5432"),
		DBName: env("DB_NAME", "credits"),
		DBUser: env("DB_USER", "provider_app"),
		DBPass: env("DB_PASS", ""),

		InternalSecret:    env("INTERNAL_SHARED_SECRET", ""),
		UpstreamKeySecret: env("CREDITS_UPSTREAM_KEY_SECRET", ""),

		OracleURL: env("ORACLE_URL", "http://srv-captain--oracle-api"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}
