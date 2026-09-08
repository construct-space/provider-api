// Package models holds the gorm models for provider-api's Postgres
// database. The catalog tables (provider_catalog, provider_catalog_models,
// provider_catalog_version) moved here from source-api on 2026-05-11;
// see construct-app/docs/plans/2026-05-11-construct-provider-credits.md.
package models

import "time"

// ProviderCatalog — top-level metadata for an AI provider (Anthropic,
// OpenAI, etc.). The *catalog* side of the providers system; user keys
// live in source-api's ProviderKey / OrgProviderKey tables.
//
// Two optional mode blocks:
//   - API key: pay-per-token via a key the user pastes (api.anthropic.com…)
//   - Monthly: flat-fee plan via OAuth (Claude Max, ChatGPT Plus, Copilot…)
//
// Legacy columns (AuthType, BaseURL, OAuthConfig, EnvKeys, HasSharedKey,
// DocsURL, SignupURL) survive on the row for backward compatibility with
// old clients. Their values are mirrored from the active mode at read
// time — no backfill runs in provider-api because the dump from source
// is already in the mode-split shape.
type ProviderCatalog struct {
	ID          string `json:"id" gorm:"primarykey;size:64"`
	Slug        string `json:"slug" gorm:"size:64;uniqueIndex;not null"`
	Name        string `json:"name" gorm:"size:120;not null"`
	Description string `json:"description" gorm:"type:text"`

	Icon         string `json:"icon" gorm:"size:120"`
	Capabilities string `json:"capabilities,omitempty" gorm:"type:text"` // JSON array

	Enabled       bool   `json:"enabled" gorm:"not null;default:true"`
	MinAppVersion string `json:"min_app_version" gorm:"size:32"`
	SortOrder     int    `json:"sort_order" gorm:"not null;default:0"`

	// ─── API key mode ────────────────────────────────────────────────────
	ApiKeyEnabled      bool   `json:"api_key_enabled" gorm:"not null;default:false"`
	ApiKeyBaseURL      string `json:"api_key_base_url" gorm:"size:255"`
	ApiKeyEnvKeys      string `json:"api_key_env_keys,omitempty" gorm:"type:text"`
	ApiKeyDocsURL      string `json:"api_key_docs_url" gorm:"size:255"`
	ApiKeySignupURL    string `json:"api_key_signup_url" gorm:"size:255"`
	ApiKeyHasSharedKey bool   `json:"api_key_has_shared_key" gorm:"not null;default:false"`

	// ─── Monthly plan mode ───────────────────────────────────────────────
	MonthlyEnabled     bool   `json:"monthly_enabled" gorm:"not null;default:false"`
	MonthlyAuthType    string `json:"monthly_auth_type" gorm:"size:32"`
	MonthlyBaseURL     string `json:"monthly_base_url" gorm:"size:255"`
	MonthlyOAuthConfig string `json:"monthly_oauth_config,omitempty" gorm:"type:text"`
	MonthlyDocsURL     string `json:"monthly_docs_url" gorm:"size:255"`
	MonthlySignupURL   string `json:"monthly_signup_url" gorm:"size:255"`

	// ─── Legacy (superseded; kept for old-client compat) ─────────────────
	AuthType     string `json:"auth_type,omitempty" gorm:"size:32;default:api_key"`
	BaseURL      string `json:"base_url,omitempty" gorm:"size:255"`
	OAuthConfig  string `json:"oauth_config,omitempty" gorm:"type:text"`
	EnvKeys      string `json:"env_keys,omitempty" gorm:"type:text"`
	DocsURL      string `json:"docs_url,omitempty" gorm:"size:255"`
	SignupURL    string `json:"signup_url,omitempty" gorm:"size:255"`
	HasSharedKey bool   `json:"has_shared_key,omitempty" gorm:"default:false"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ProviderCatalog) TableName() string { return "provider_catalog" }

// ProviderCatalogModel — one model offered by a provider.
//
// AvailableOnApiKey / AvailableOnMonthly gate which of the provider's modes
// exposes this model. Locked flips to true on any admin save through the
// oracle edit drawer — the models.dev sync skips locked rows so hand-tuned
// pricing / request_spec / context isn't clobbered.
type ProviderCatalogModel struct {
	ID         string `json:"id" gorm:"primarykey;size:64"`
	ProviderID string `json:"provider_id" gorm:"size:64;index;not null"`

	ModelID string `json:"model_id" gorm:"size:120;not null;index"`
	Name    string `json:"name" gorm:"size:120;not null"`

	Capabilities    string `json:"capabilities,omitempty" gorm:"type:text"`
	ContextWindow   int    `json:"context_window" gorm:"not null;default:0"`
	MaxOutputTokens int    `json:"max_output_tokens" gorm:"not null;default:0"`

	// RequestSpec is free-form JSON describing how the operator should
	// build requests. Empty → operator uses its conservative default.
	RequestSpec string `json:"request_spec,omitempty" gorm:"type:text"`

	// Column names pinned explicitly; the field names contain "1M" which
	// GORM's snake_case strategy has handled inconsistently across versions.
	InputCostPer1M      float64 `json:"input_cost_per_1m" gorm:"column:input_cost_per_1m;not null;default:0"`
	OutputCostPer1M     float64 `json:"output_cost_per_1m" gorm:"column:output_cost_per_1m;not null;default:0"`
	CacheReadCostPer1M  float64 `json:"cache_read_cost_per_1m" gorm:"column:cache_read_cost_per_1m;not null;default:0"`
	CacheWriteCostPer1M float64 `json:"cache_write_cost_per_1m" gorm:"column:cache_write_cost_per_1m;not null;default:0"`

	AvailableOnApiKey  bool `json:"available_on_api_key" gorm:"not null;default:true"`
	AvailableOnMonthly bool `json:"available_on_monthly" gorm:"not null;default:true"`

	Enabled       bool   `json:"enabled" gorm:"not null;default:true"`
	Default       bool   `json:"default" gorm:"not null;default:false"`
	Deprecated    bool   `json:"deprecated" gorm:"not null;default:false"`
	ReplacedBy    string `json:"replaced_by" gorm:"size:120"`
	MinAppVersion string `json:"min_app_version" gorm:"size:32"`
	SortOrder     int    `json:"sort_order" gorm:"not null;default:0"`

	// TierHint suggests which tier slot in the desktop's per-provider
	// settings should default to this model. Values: "large" | "medium"
	// | "small" | "" (no hint). The desktop is free to override.
	//
	// Many models can carry the same hint — the desktop picks one per
	// slot using the catalog row with that hint and the lowest
	// sort_order. Tiered routing in brain reads from auth.json, never
	// directly from this field.
	TierHint string `json:"tier_hint" gorm:"size:16;default:''"`

	Locked bool `json:"locked" gorm:"column:locked;not null;default:false"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ProviderCatalogModel) TableName() string { return "provider_catalog_models" }

// CatalogVersion — single-row ETag table. Bumped on any CRUD that
// changes the public catalog shape so clients can invalidate caches.
type CatalogVersion struct {
	ID        int       `json:"id" gorm:"primarykey"`
	Version   string    `json:"version" gorm:"size:64;not null"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (CatalogVersion) TableName() string { return "provider_catalog_version" }
