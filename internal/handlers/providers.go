package handlers

// Public read side of the provider catalog. Moved here verbatim from
// source-api on 2026-05-11; the request/response shapes are unchanged
// so the operator's modelspec.load and oracle-web's catalog reader
// keep working when the gateway repoints /api/providers to this service.

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"construct/provider/internal/database"
	"construct/provider/internal/models"
)

type providerModePublicApiKey struct {
	Enabled      bool     `json:"enabled"`
	BaseURL      string   `json:"base_url,omitempty"`
	EnvKeys      []string `json:"env_keys,omitempty"`
	DocsURL      string   `json:"docs_url,omitempty"`
	SignupURL    string   `json:"signup_url,omitempty"`
	HasSharedKey bool     `json:"has_shared_key"`
}

type providerModePublicMonthly struct {
	Enabled     bool           `json:"enabled"`
	AuthType    string         `json:"auth_type,omitempty"`
	BaseURL     string         `json:"base_url,omitempty"`
	OAuthConfig map[string]any `json:"oauth_config,omitempty"`
	DocsURL     string         `json:"docs_url,omitempty"`
	SignupURL   string         `json:"signup_url,omitempty"`
}

type providerPublic struct {
	ID            string                     `json:"id"`
	Slug          string                     `json:"slug"`
	Name          string                     `json:"name"`
	Description   string                     `json:"description,omitempty"`
	Icon          string                     `json:"icon,omitempty"`
	Capabilities  []string                   `json:"capabilities,omitempty"`
	MinAppVersion string                     `json:"min_app_version,omitempty"`
	ApiKey        *providerModePublicApiKey  `json:"api_key,omitempty"`
	Monthly       *providerModePublicMonthly `json:"monthly,omitempty"`
	Models        []modelPublic              `json:"models"`

	// Legacy flat fields — mirrored from the active mode for older
	// clients that still read them directly.
	AuthType     string         `json:"auth_type,omitempty"`
	BaseURL      string         `json:"base_url,omitempty"`
	OAuthConfig  map[string]any `json:"oauth_config,omitempty"`
	EnvKeys      []string       `json:"env_keys,omitempty"`
	DocsURL      string         `json:"docs_url,omitempty"`
	SignupURL    string         `json:"signup_url,omitempty"`
	HasSharedKey bool           `json:"has_shared_key"`
}

type modelPublic struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Capabilities       []string        `json:"capabilities,omitempty"`
	ContextWindow      int             `json:"context_window"`
	MaxOutputTokens    int             `json:"max_output_tokens"`
	InputCost          float64         `json:"input_cost_per_1m"`
	OutputCost         float64         `json:"output_cost_per_1m"`
	CacheReadCost      float64         `json:"cache_read_cost_per_1m"`
	CacheWriteCost     float64         `json:"cache_write_cost_per_1m"`
	AvailableOnApiKey  bool            `json:"available_on_api_key"`
	AvailableOnMonthly bool            `json:"available_on_monthly"`
	Default            bool            `json:"default,omitempty"`
	Deprecated         bool            `json:"deprecated,omitempty"`
	ReplacedBy         string          `json:"replaced_by,omitempty"`
	MinAppVersion      string          `json:"min_app_version,omitempty"`
	RequestSpec        json.RawMessage `json:"request_spec,omitempty"`
	// TierHint suggests which tier slot in the desktop's per-provider
	// settings should pick this model as the default. Values: "large"
	// | "medium" | "small" | "" (no hint). The desktop is free to
	// override; this is just the seed value when a user first adds
	// the provider.
	TierHint string `json:"tier_hint,omitempty"`
}

type catalogSnapshot struct {
	Version   string
	Providers []providerPublic
	Fetched   time.Time
}

var (
	catalogCache catalogSnapshot
	catalogMu    sync.RWMutex
)

const catalogStaleAfter = 30 * time.Second

func loadCatalog() catalogSnapshot {
	version := currentCatalogVersion()

	catalogMu.RLock()
	if catalogCache.Version == version && time.Since(catalogCache.Fetched) < catalogStaleAfter {
		snap := catalogCache
		catalogMu.RUnlock()
		return snap
	}
	catalogMu.RUnlock()

	var providerRows []models.ProviderCatalog
	database.DB.Where("enabled = ?", true).Order("sort_order ASC, name ASC").Find(&providerRows)

	var modelRows []models.ProviderCatalogModel
	if len(providerRows) > 0 {
		ids := make([]string, 0, len(providerRows))
		for _, p := range providerRows {
			ids = append(ids, p.ID)
		}
		database.DB.
			Where("provider_id IN ? AND enabled = ?", ids, true).
			Order("sort_order ASC, name ASC").
			Find(&modelRows)
	}

	modelsByProvider := map[string][]modelPublic{}
	for _, m := range modelRows {
		modelsByProvider[m.ProviderID] = append(modelsByProvider[m.ProviderID], modelPublic{
			ID:                 m.ModelID,
			Name:               m.Name,
			Capabilities:       decodeJSONArray(m.Capabilities),
			ContextWindow:      m.ContextWindow,
			MaxOutputTokens:    m.MaxOutputTokens,
			InputCost:          m.InputCostPer1M,
			OutputCost:         m.OutputCostPer1M,
			CacheReadCost:      m.CacheReadCostPer1M,
			CacheWriteCost:     m.CacheWriteCostPer1M,
			AvailableOnApiKey:  m.AvailableOnApiKey,
			AvailableOnMonthly: m.AvailableOnMonthly,
			Default:            m.Default,
			Deprecated:         m.Deprecated,
			ReplacedBy:         m.ReplacedBy,
			MinAppVersion:      m.MinAppVersion,
			RequestSpec:        rawJSONOrNil(m.RequestSpec),
			TierHint:           m.TierHint,
		})
	}

	out := make([]providerPublic, 0, len(providerRows))
	for _, p := range providerRows {
		pub := providerPublic{
			ID:            p.ID,
			Slug:          p.Slug,
			Name:          p.Name,
			Description:   p.Description,
			Icon:          p.Icon,
			Capabilities:  decodeJSONArray(p.Capabilities),
			MinAppVersion: p.MinAppVersion,
			Models:        modelsByProvider[p.ID],
		}
		if p.ApiKeyEnabled || p.ApiKeyBaseURL != "" {
			pub.ApiKey = &providerModePublicApiKey{
				Enabled:      p.ApiKeyEnabled,
				BaseURL:      p.ApiKeyBaseURL,
				EnvKeys:      decodeJSONArray(p.ApiKeyEnvKeys),
				DocsURL:      p.ApiKeyDocsURL,
				SignupURL:    p.ApiKeySignupURL,
				HasSharedKey: p.ApiKeyHasSharedKey,
			}
		}
		if p.MonthlyEnabled || p.MonthlyAuthType != "" {
			pub.Monthly = &providerModePublicMonthly{
				Enabled:     p.MonthlyEnabled,
				AuthType:    p.MonthlyAuthType,
				BaseURL:     p.MonthlyBaseURL,
				OAuthConfig: decodeJSONObject(p.MonthlyOAuthConfig),
				DocsURL:     p.MonthlyDocsURL,
				SignupURL:   p.MonthlySignupURL,
			}
		}

		// Legacy mirror — populate from the *active* mode so old clients
		// reading auth_type/base_url directly still get something useful.
		if pub.ApiKey != nil && pub.ApiKey.Enabled {
			pub.AuthType = "api_key"
			pub.BaseURL = pub.ApiKey.BaseURL
			pub.EnvKeys = pub.ApiKey.EnvKeys
			pub.DocsURL = pub.ApiKey.DocsURL
			pub.SignupURL = pub.ApiKey.SignupURL
			pub.HasSharedKey = pub.ApiKey.HasSharedKey
		} else if pub.Monthly != nil && pub.Monthly.Enabled {
			pub.AuthType = pub.Monthly.AuthType
			pub.BaseURL = pub.Monthly.BaseURL
			pub.OAuthConfig = pub.Monthly.OAuthConfig
			pub.DocsURL = pub.Monthly.DocsURL
			pub.SignupURL = pub.Monthly.SignupURL
		}

		out = append(out, pub)
	}

	snap := catalogSnapshot{
		Version:   version,
		Providers: out,
		Fetched:   time.Now(),
	}
	catalogMu.Lock()
	catalogCache = snap
	catalogMu.Unlock()
	return snap
}

// ListProviders — GET /api/providers
// ETag-aware: returns 304 when the client already has the current version.
func ListProviders(w http.ResponseWriter, r *http.Request) {
	snap := loadCatalog()
	etag := `"` + snap.Version + `"`

	if inm := r.Header.Get("If-None-Match"); inm != "" && inm == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, max-age=30, must-revalidate")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=30, must-revalidate")
	WriteJSON(w, 200, map[string]any{
		"data":    snap.Providers,
		"version": snap.Version,
	})
}

// GetCatalogVersion — GET /api/providers/catalog/version
// Lightweight poll: clients use it to decide whether to refetch.
func GetCatalogVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, max-age=15, must-revalidate")
	WriteJSON(w, 200, map[string]any{"version": currentCatalogVersion()})
}
