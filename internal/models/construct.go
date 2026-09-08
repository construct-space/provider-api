package models

import "time"

// ConstructUpstreamProvider — a real upstream the platform holds an API
// key for. Many Construct models can share one upstream (e.g. one OpenAI
// key powering gpt-5.5-mini + gpt-5.5-nano + gpt-5.4-codex Construct
// rows). Decoupled from `provider_catalog` because Construct's upstreams
// are platform secrets, not part of the user-facing BYOK catalog.
type ConstructUpstreamProvider struct {
	ID          string `json:"id" gorm:"primarykey;size:64"`        // "openai-prod"
	Label       string `json:"label" gorm:"size:120;not null"`      // "OpenAI"
	BaseURL     string `json:"base_url" gorm:"size:255;not null"`   // "https://api.openai.com/v1"
	AuthHeader  string `json:"auth_header" gorm:"size:32;not null;default:'authorization-bearer'"`
	// APIKeyEncrypted is AES-GCM(base64) ciphertext, decrypted only when
	// proxying a request. Never returned to clients.
	APIKeyEncrypted string `json:"-" gorm:"size:512;not null"`

	Enabled   bool      `json:"enabled" gorm:"not null;default:true"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by" gorm:"size:120"`
	CreatedAt time.Time `json:"created_at"`
}

func (ConstructUpstreamProvider) TableName() string { return "construct_upstream_providers" }

// ConstructPickerEntry — a user-facing chat model exposed on the
// Construct provider. This is what end users see in the picker.
// Today there is exactly one row, `source`, which fans out via
// source_family routing. Decoupled from ConstructRoutingTarget
// because the picker is about user identity ("which model am I
// talking to?") while routing targets are about upstream resolution
// ("which upstream API serves this turn?"). No upstream/cost fields:
// dispatch resolves via source_family_routes, debit reflects the
// target that actually served.
type ConstructPickerEntry struct {
	ID          string `json:"id" gorm:"primarykey;size:64"` // "source"
	Label       string `json:"label" gorm:"size:120;not null"`
	Description string `json:"description" gorm:"type:text"`
	Icon        string `json:"icon" gorm:"size:120"`

	// RouteViaOperator names the Source-family operator this picker
	// entry dispatches through for the medium (default) tier. The chat
	// handler loads source_family_routes where operator_id = this, then
	// walks primary → backups in order until one succeeds. Default
	// 'tank' (the entry-point operator).
	//
	// RouteViaOperatorLarge / RouteViaOperatorSmall are optional
	// per-tier overrides. When the chat request body carries
	// `tier: 'large'` or `'small'` and the matching column is set, the
	// dispatcher walks that operator's chain instead. When the column
	// is empty, the request falls back to the medium operator above.
	RouteViaOperator      string `json:"route_via_operator" gorm:"size:32;not null;default:'tank'"`
	RouteViaOperatorLarge string `json:"route_via_operator_large" gorm:"size:32;default:''"`
	RouteViaOperatorSmall string `json:"route_via_operator_small" gorm:"size:32;default:''"`

	// Capabilities is a JSON-encoded string array stored as text
	// (matches provider_catalog_models.capabilities). Standard tags:
	// "tools", "vision", "reasoning".
	Capabilities string `json:"capabilities" gorm:"type:text"`

	Enabled   bool      `json:"enabled" gorm:"not null;default:true"`
	SortOrder int       `json:"sort_order" gorm:"not null;default:0"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ConstructPickerEntry) TableName() string { return "construct_picker_entries" }

// ConstructRoutingTarget — a named alias for upstream + model id,
// referenced by source_family_routes. Each row binds a stable name
// (Apoc, Trinity, Morpheus aggregator, …) to one upstream provider +
// one upstream model id, plus the per-operator credit cost, caps, and
// thinking-mode config the chat dispatcher reads.
//
// Source-family operators reference these by id, not by raw upstream
// strings — so swapping which Together model "Trinity" maps to is one
// row change here, never N edits across the routing table.
type ConstructRoutingTarget struct {
	ID string `json:"id" gorm:"primarykey;size:64"` // "construct-apoc"

	UpstreamProviderID string `json:"upstream_provider_id" gorm:"size:64;index;not null"`
	UpstreamModel      string `json:"upstream_model" gorm:"size:120;not null"`

	Label       string `json:"label" gorm:"size:120;not null"`
	Description string `json:"description" gorm:"type:text"`
	Icon        string `json:"icon" gorm:"size:120"`

	CreditsPerPrompt         int `json:"credits_per_prompt" gorm:"not null;default:1"`
	MaxToolCallsPerCredit    int `json:"max_tool_calls_per_credit" gorm:"not null;default:20"`
	MaxOutputTokensPerCredit int `json:"max_output_tokens_per_credit" gorm:"not null;default:4096"`

	// ThinkingMode controls reasoning_effort + extra_body.thinking on
	// the upstream request. Empty / "auto" = don't inject. "off" =
	// actively disable. "low" / "medium" / "high" set reasoning_effort
	// and enable thinking (Deepseek extra_body.thinking.type='enabled'
	// shape; OpenAI o-series reads reasoning_effort directly).
	ThinkingMode string `json:"thinking_mode" gorm:"size:16;default:''"`

	Capabilities string `json:"capabilities" gorm:"type:text"`

	Enabled   bool      `json:"enabled" gorm:"not null;default:true"`
	SortOrder int       `json:"sort_order" gorm:"not null;default:0"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ConstructRoutingTarget) TableName() string { return "construct_routing_targets" }

// ConstructConfig — singleton (id=1). Daily allowance + global kill switch.
// autoIncrement:false stops GORM from emitting smallserial for the
// primary key (Postgres rejects "smallserial DEFAULT 1" because
// smallserial already implies a sequence default).
type ConstructConfig struct {
	ID             int       `json:"id" gorm:"primarykey;autoIncrement:false;default:1;check:id = 1"`
	DailyAllowance int       `json:"daily_allowance" gorm:"not null;default:100"`
	Enabled        bool      `json:"enabled" gorm:"not null;default:true"`
	UpdatedAt      time.Time `json:"updated_at"`
	UpdatedBy      string    `json:"updated_by" gorm:"size:120"`
}

func (ConstructConfig) TableName() string { return "construct_config" }

// CreditsBalance — one row per user. `daily_used` + `daily_date` form a
// lazy reset: any debit on a new UTC date overwrites them. `paid_balance`
// stores top-ups (never expire). `blocked` short-circuits admission for
// admin-blocked users.
type CreditsBalance struct {
	UserID        string    `json:"user_id" gorm:"primarykey;size:64"`
	DailyUsed     int       `json:"daily_used" gorm:"not null;default:0"`
	DailyDate     time.Time `json:"daily_date" gorm:"type:date;not null;default:(now() at time zone 'utc')::date"`
	PaidBalance   int       `json:"paid_balance" gorm:"not null;default:0"`
	Blocked       bool      `json:"blocked" gorm:"not null;default:false"`
	BlockedReason string    `json:"blocked_reason" gorm:"type:text"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (CreditsBalance) TableName() string { return "credits_balance" }

// CreditsLedger — append-only history of debits + grants. Never refunds
// (see plan doc). `kind` is one of: 'debit_prompt' | 'grant_paid' |
// 'admin_grant'. `meta` carries per-event details (model id, prompt id,
// upstream provider, etc.).
type CreditsLedger struct {
	ID        int64     `json:"id" gorm:"primarykey;autoIncrement"`
	UserID    string    `json:"user_id" gorm:"size:64;index;not null"`
	Delta     int       `json:"delta" gorm:"not null"`         // negative = debit, positive = grant
	Kind      string    `json:"kind" gorm:"size:32;not null"`
	PromptID  string    `json:"prompt_id" gorm:"size:64;index"`
	Meta      string    `json:"meta" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`
}

func (CreditsLedger) TableName() string { return "credits_ledger" }
