package handlers

// Admin CRUD for the provider catalog. Ported verbatim from source-api;
// X-Internal-Secret-gated. Oracle is the only caller.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"construct/provider/internal/database"
	"construct/provider/internal/models"

	"gorm.io/gorm"
)

// ─── Catalog version ──────────────────────────────────────────────────────

func currentCatalogVersion() string {
	var row models.CatalogVersion
	err := database.DB.First(&row, 1).Error
	if err == gorm.ErrRecordNotFound {
		row = models.CatalogVersion{ID: 1, Version: newVersionToken(), UpdatedAt: time.Now()}
		database.DB.Create(&row)
	}
	return row.Version
}

func bumpCatalogVersion() string {
	v := newVersionToken()
	database.DB.Where("id = ?", 1).Assign(models.CatalogVersion{
		ID: 1, Version: v, UpdatedAt: time.Now(),
	}).FirstOrCreate(&models.CatalogVersion{})
	return v
}

func newVersionToken() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ─── Providers ────────────────────────────────────────────────────────────

type adminModeApiKeyBody struct {
	Enabled      *bool    `json:"enabled,omitempty"`
	BaseURL      *string  `json:"base_url,omitempty"`
	EnvKeys      []string `json:"env_keys,omitempty"`
	DocsURL      *string  `json:"docs_url,omitempty"`
	SignupURL    *string  `json:"signup_url,omitempty"`
	HasSharedKey *bool    `json:"has_shared_key,omitempty"`
}

type adminModeMonthlyBody struct {
	Enabled     *bool           `json:"enabled,omitempty"`
	AuthType    *string         `json:"auth_type,omitempty"`
	BaseURL     *string         `json:"base_url,omitempty"`
	OAuthConfig *map[string]any `json:"oauth_config,omitempty"`
	DocsURL     *string         `json:"docs_url,omitempty"`
	SignupURL   *string         `json:"signup_url,omitempty"`
}

type adminProviderBody struct {
	ID            string                `json:"id"`
	Slug          string                `json:"slug"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	Icon          string                `json:"icon"`
	Capabilities  []string              `json:"capabilities,omitempty"`
	Enabled       *bool                 `json:"enabled,omitempty"`
	MinAppVersion string                `json:"min_app_version"`
	SortOrder     int                   `json:"sort_order"`
	ApiKey        *adminModeApiKeyBody  `json:"api_key,omitempty"`
	Monthly       *adminModeMonthlyBody `json:"monthly,omitempty"`

	// Legacy flat fields accepted for old-client compat.
	AuthType     string         `json:"auth_type,omitempty"`
	BaseURL      string         `json:"base_url,omitempty"`
	OAuthConfig  map[string]any `json:"oauth_config,omitempty"`
	EnvKeys      []string       `json:"env_keys,omitempty"`
	DocsURL      string         `json:"docs_url,omitempty"`
	SignupURL    string         `json:"signup_url,omitempty"`
	HasSharedKey *bool          `json:"has_shared_key,omitempty"`
}

func (b *adminProviderBody) normalizeLegacy() {
	if b.AuthType == "" && b.BaseURL == "" && b.OAuthConfig == nil && b.EnvKeys == nil {
		return
	}

	isMonthly := strings.HasPrefix(b.AuthType, "oauth_") ||
		b.AuthType == "anthropic_oauth" ||
		b.AuthType == "copilot_device"

	if isMonthly {
		if b.Monthly == nil {
			b.Monthly = &adminModeMonthlyBody{}
		}
		yes := true
		b.Monthly.Enabled = &yes
		if b.AuthType != "" {
			at := b.AuthType
			b.Monthly.AuthType = &at
		}
		if b.BaseURL != "" {
			u := b.BaseURL
			b.Monthly.BaseURL = &u
		}
		if b.OAuthConfig != nil {
			cfg := b.OAuthConfig
			b.Monthly.OAuthConfig = &cfg
		}
		if b.DocsURL != "" {
			u := b.DocsURL
			b.Monthly.DocsURL = &u
		}
		if b.SignupURL != "" {
			u := b.SignupURL
			b.Monthly.SignupURL = &u
		}
		return
	}

	if b.ApiKey == nil {
		b.ApiKey = &adminModeApiKeyBody{}
	}
	yes := true
	b.ApiKey.Enabled = &yes
	if b.BaseURL != "" {
		u := b.BaseURL
		b.ApiKey.BaseURL = &u
	}
	if b.EnvKeys != nil {
		b.ApiKey.EnvKeys = b.EnvKeys
	}
	if b.DocsURL != "" {
		u := b.DocsURL
		b.ApiKey.DocsURL = &u
	}
	if b.SignupURL != "" {
		u := b.SignupURL
		b.ApiKey.SignupURL = &u
	}
	if b.HasSharedKey != nil {
		b.ApiKey.HasSharedKey = b.HasSharedKey
	}
}

// GET /api/admin/providers
func AdminListProviders(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	var rows []models.ProviderCatalog
	database.DB.Order("sort_order ASC, name ASC").Find(&rows)

	items := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		var modelCount int64
		database.DB.Model(&models.ProviderCatalogModel{}).Where("provider_id = ?", p.ID).Count(&modelCount)
		items = append(items, adminProviderRow(p, modelCount))
	}
	WriteJSON(w, 200, map[string]any{
		"providers": items,
		"total":     len(items),
		"version":   currentCatalogVersion(),
	})
}

// GET /api/admin/providers/{id}
func AdminGetProvider(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := r.PathValue("id")
	var p models.ProviderCatalog
	if err := database.DB.First(&p, "id = ?", id).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "provider not found"})
		return
	}
	var modelRows []models.ProviderCatalogModel
	database.DB.Where("provider_id = ?", p.ID).Order("sort_order ASC, name ASC").Find(&modelRows)
	modelsOut := make([]map[string]any, 0, len(modelRows))
	for _, m := range modelRows {
		modelsOut = append(modelsOut, adminModelRow(m))
	}
	WriteJSON(w, 200, map[string]any{
		"provider": adminProviderRow(p, int64(len(modelRows))),
		"models":   modelsOut,
	})
}

// POST /api/admin/providers
func AdminCreateProvider(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	var body adminProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	body.Slug = strings.TrimSpace(body.Slug)
	if body.ID == "" || body.Slug == "" || body.Name == "" {
		WriteJSON(w, 400, map[string]any{"error": "id, slug, name required"})
		return
	}
	body.normalizeLegacy()

	p := models.ProviderCatalog{
		ID:            body.ID,
		Slug:          body.Slug,
		Name:          body.Name,
		Description:   body.Description,
		Icon:          body.Icon,
		Capabilities:  encodeJSON(body.Capabilities),
		Enabled:       derefBool(body.Enabled, true),
		MinAppVersion: body.MinAppVersion,
		SortOrder:     body.SortOrder,
	}
	applyApiKeyMode(&p, body.ApiKey)
	applyMonthlyMode(&p, body.Monthly)
	if !p.ApiKeyEnabled && !p.MonthlyEnabled {
		p.ApiKeyEnabled = true
	}
	if err := database.DB.Create(&p).Error; err != nil {
		WriteJSON(w, 400, map[string]any{"error": "create failed: " + err.Error()})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 201, map[string]any{"provider": adminProviderRow(p, 0)})
}

// PUT /api/admin/providers/{id}
func AdminUpdateProvider(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := r.PathValue("id")
	var p models.ProviderCatalog
	if err := database.DB.First(&p, "id = ?", id).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "provider not found"})
		return
	}
	var body adminProviderBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	body.normalizeLegacy()

	updates := map[string]any{}
	if body.Slug != "" {
		updates["slug"] = body.Slug
	}
	if body.Name != "" {
		updates["name"] = body.Name
	}
	if body.Description != "" {
		updates["description"] = body.Description
	}
	if body.Icon != "" {
		updates["icon"] = body.Icon
	}
	if body.Capabilities != nil {
		updates["capabilities"] = encodeJSON(body.Capabilities)
	}
	if body.Enabled != nil {
		updates["enabled"] = *body.Enabled
	}
	if body.MinAppVersion != "" {
		updates["min_app_version"] = body.MinAppVersion
	}
	if body.SortOrder != 0 {
		updates["sort_order"] = body.SortOrder
	}
	if body.ApiKey != nil {
		if body.ApiKey.Enabled != nil {
			updates["api_key_enabled"] = *body.ApiKey.Enabled
		}
		if body.ApiKey.BaseURL != nil {
			updates["api_key_base_url"] = *body.ApiKey.BaseURL
		}
		if body.ApiKey.EnvKeys != nil {
			updates["api_key_env_keys"] = encodeJSON(body.ApiKey.EnvKeys)
		}
		if body.ApiKey.DocsURL != nil {
			updates["api_key_docs_url"] = *body.ApiKey.DocsURL
		}
		if body.ApiKey.SignupURL != nil {
			updates["api_key_signup_url"] = *body.ApiKey.SignupURL
		}
		if body.ApiKey.HasSharedKey != nil {
			updates["api_key_has_shared_key"] = *body.ApiKey.HasSharedKey
		}
	}
	if body.Monthly != nil {
		if body.Monthly.Enabled != nil {
			updates["monthly_enabled"] = *body.Monthly.Enabled
		}
		if body.Monthly.AuthType != nil {
			updates["monthly_auth_type"] = *body.Monthly.AuthType
		}
		if body.Monthly.BaseURL != nil {
			updates["monthly_base_url"] = *body.Monthly.BaseURL
		}
		if body.Monthly.OAuthConfig != nil {
			updates["monthly_o_auth_config"] = encodeJSON(*body.Monthly.OAuthConfig)
		}
		if body.Monthly.DocsURL != nil {
			updates["monthly_docs_url"] = *body.Monthly.DocsURL
		}
		if body.Monthly.SignupURL != nil {
			updates["monthly_signup_url"] = *body.Monthly.SignupURL
		}
	}
	if err := database.DB.Model(&p).Updates(updates).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "update failed"})
		return
	}
	database.DB.First(&p, "id = ?", id)
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"provider": adminProviderRow(p, 0)})
}

// DELETE /api/admin/providers/{id}
func AdminDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := r.PathValue("id")
	database.DB.Where("provider_id = ?", id).Delete(&models.ProviderCatalogModel{})
	if err := database.DB.Delete(&models.ProviderCatalog{}, "id = ?", id).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "delete failed"})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"ok": true})
}

func applyApiKeyMode(p *models.ProviderCatalog, m *adminModeApiKeyBody) {
	if m == nil {
		return
	}
	if m.Enabled != nil {
		p.ApiKeyEnabled = *m.Enabled
	}
	if m.BaseURL != nil {
		p.ApiKeyBaseURL = *m.BaseURL
	}
	if m.EnvKeys != nil {
		p.ApiKeyEnvKeys = encodeJSON(m.EnvKeys)
	}
	if m.DocsURL != nil {
		p.ApiKeyDocsURL = *m.DocsURL
	}
	if m.SignupURL != nil {
		p.ApiKeySignupURL = *m.SignupURL
	}
	if m.HasSharedKey != nil {
		p.ApiKeyHasSharedKey = *m.HasSharedKey
	}
}

func applyMonthlyMode(p *models.ProviderCatalog, m *adminModeMonthlyBody) {
	if m == nil {
		return
	}
	if m.Enabled != nil {
		p.MonthlyEnabled = *m.Enabled
	}
	if m.AuthType != nil {
		p.MonthlyAuthType = *m.AuthType
	}
	if m.BaseURL != nil {
		p.MonthlyBaseURL = *m.BaseURL
	}
	if m.OAuthConfig != nil {
		p.MonthlyOAuthConfig = encodeJSON(*m.OAuthConfig)
	}
	if m.DocsURL != nil {
		p.MonthlyDocsURL = *m.DocsURL
	}
	if m.SignupURL != nil {
		p.MonthlySignupURL = *m.SignupURL
	}
}

// ─── Models ───────────────────────────────────────────────────────────────

type adminModelBody struct {
	ID                  string          `json:"id"`
	ModelID             string          `json:"model_id"`
	Name                string          `json:"name"`
	Capabilities        []string        `json:"capabilities,omitempty"`
	ContextWindow       int             `json:"context_window"`
	MaxOutputTokens     int             `json:"max_output_tokens"`
	InputCostPer1M      *float64        `json:"input_cost_per_1m,omitempty"`
	OutputCostPer1M     *float64        `json:"output_cost_per_1m,omitempty"`
	CacheReadCostPer1M  *float64        `json:"cache_read_cost_per_1m,omitempty"`
	CacheWriteCostPer1M *float64        `json:"cache_write_cost_per_1m,omitempty"`
	AvailableOnApiKey   *bool           `json:"available_on_api_key,omitempty"`
	AvailableOnMonthly  *bool           `json:"available_on_monthly,omitempty"`
	Enabled             *bool           `json:"enabled,omitempty"`
	Default             *bool           `json:"default,omitempty"`
	Deprecated          *bool           `json:"deprecated,omitempty"`
	ReplacedBy          string          `json:"replaced_by"`
	MinAppVersion       string          `json:"min_app_version"`
	SortOrder           int             `json:"sort_order"`
	RequestSpec         json.RawMessage `json:"request_spec,omitempty"`
	TierHint            string          `json:"tier_hint"`
}

// GET /api/admin/providers/{id}/models
func AdminListProviderModels(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	providerID := r.PathValue("id")
	var rows []models.ProviderCatalogModel
	database.DB.Where("provider_id = ?", providerID).Order("sort_order ASC, name ASC").Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, m := range rows {
		out = append(out, adminModelRow(m))
	}
	WriteJSON(w, 200, map[string]any{"models": out, "total": len(out)})
}

// POST /api/admin/providers/{id}/models
func AdminCreateProviderModel(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	providerID := r.PathValue("id")
	var p models.ProviderCatalog
	if err := database.DB.First(&p, "id = ?", providerID).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "provider not found"})
		return
	}
	var body adminModelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	body.ModelID = strings.TrimSpace(body.ModelID)
	if body.ID == "" || body.ModelID == "" || body.Name == "" {
		WriteJSON(w, 400, map[string]any{"error": "id, model_id, name required"})
		return
	}
	m := models.ProviderCatalogModel{
		ID:                  body.ID,
		ProviderID:          providerID,
		ModelID:             body.ModelID,
		Name:                body.Name,
		Capabilities:        encodeJSON(body.Capabilities),
		ContextWindow:       body.ContextWindow,
		MaxOutputTokens:     body.MaxOutputTokens,
		InputCostPer1M:      derefFloat(body.InputCostPer1M, 0),
		OutputCostPer1M:     derefFloat(body.OutputCostPer1M, 0),
		CacheReadCostPer1M:  derefFloat(body.CacheReadCostPer1M, 0),
		CacheWriteCostPer1M: derefFloat(body.CacheWriteCostPer1M, 0),
		AvailableOnApiKey:   derefBool(body.AvailableOnApiKey, true),
		AvailableOnMonthly:  derefBool(body.AvailableOnMonthly, true),
		Enabled:             derefBool(body.Enabled, true),
		Default:             derefBool(body.Default, false),
		Deprecated:          derefBool(body.Deprecated, false),
		ReplacedBy:          body.ReplacedBy,
		MinAppVersion:       body.MinAppVersion,
		SortOrder:           body.SortOrder,
		RequestSpec:         stringifyRawJSON(body.RequestSpec),
		TierHint:            strings.ToLower(strings.TrimSpace(body.TierHint)),
	}
	if err := database.DB.Create(&m).Error; err != nil {
		WriteJSON(w, 400, map[string]any{"error": "create failed: " + err.Error()})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 201, map[string]any{"model": adminModelRow(m)})
}

// PUT /api/admin/providers/{id}/models/{modelId}
func AdminUpdateProviderModel(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	providerID := r.PathValue("id")
	modelRowID := r.PathValue("modelId")
	var m models.ProviderCatalogModel
	if err := database.DB.Where("provider_id = ? AND id = ?", providerID, modelRowID).First(&m).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "model not found"})
		return
	}
	var body adminModelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	updates := map[string]any{}
	if body.ModelID != "" {
		updates["model_id"] = body.ModelID
	}
	if body.Name != "" {
		updates["name"] = body.Name
	}
	if body.Capabilities != nil {
		updates["capabilities"] = encodeJSON(body.Capabilities)
	}
	if body.ContextWindow != 0 {
		updates["context_window"] = body.ContextWindow
	}
	if body.MaxOutputTokens != 0 {
		updates["max_output_tokens"] = body.MaxOutputTokens
	}
	if body.InputCostPer1M != nil {
		updates["input_cost_per_1m"] = *body.InputCostPer1M
	}
	if body.OutputCostPer1M != nil {
		updates["output_cost_per_1m"] = *body.OutputCostPer1M
	}
	if body.CacheReadCostPer1M != nil {
		updates["cache_read_cost_per_1m"] = *body.CacheReadCostPer1M
	}
	if body.CacheWriteCostPer1M != nil {
		updates["cache_write_cost_per_1m"] = *body.CacheWriteCostPer1M
	}
	if body.AvailableOnApiKey != nil {
		updates["available_on_api_key"] = *body.AvailableOnApiKey
	}
	if body.AvailableOnMonthly != nil {
		updates["available_on_monthly"] = *body.AvailableOnMonthly
	}
	if body.Enabled != nil {
		updates["enabled"] = *body.Enabled
	}
	if body.Default != nil {
		updates["default"] = *body.Default
	}
	if body.Deprecated != nil {
		updates["deprecated"] = *body.Deprecated
	}
	if body.ReplacedBy != "" {
		updates["replaced_by"] = body.ReplacedBy
	}
	if body.MinAppVersion != "" {
		updates["min_app_version"] = body.MinAppVersion
	}
	if body.RequestSpec != nil {
		updates["request_spec"] = stringifyRawJSON(body.RequestSpec)
	}
	// TierHint is a small enum; treat any provided value (incl. "") as
	// authoritative so admins can clear the hint via Oracle.
	updates["tier_hint"] = strings.ToLower(strings.TrimSpace(body.TierHint))
	if body.SortOrder != 0 {
		updates["sort_order"] = body.SortOrder
	}
	// Any admin save locks the row — protects from the next models.dev sync.
	updates["locked"] = true
	if err := database.DB.Model(&m).Updates(updates).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "update failed"})
		return
	}
	database.DB.Where("provider_id = ? AND id = ?", providerID, modelRowID).First(&m)
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"model": adminModelRow(m)})
}

// DELETE /api/admin/providers/{id}/models/{modelId}
func AdminDeleteProviderModel(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	providerID := r.PathValue("id")
	modelRowID := r.PathValue("modelId")
	if err := database.DB.Where("provider_id = ? AND id = ?", providerID, modelRowID).Delete(&models.ProviderCatalogModel{}).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "delete failed"})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"ok": true})
}

// ─── JSON / scalar helpers ────────────────────────────────────────────────

func encodeJSON(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case []string:
		if len(x) == 0 {
			return ""
		}
	case map[string]any:
		if len(x) == 0 {
			return ""
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func decodeJSONArray(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func decodeJSONObject(s string) map[string]any {
	if s == "" {
		return nil
	}
	var out map[string]any
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func rawJSONOrNil(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if !json.Valid([]byte(s)) {
		return nil
	}
	return json.RawMessage(s)
}

func stringifyRawJSON(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	if !json.Valid(r) {
		return ""
	}
	return string(r)
}

func derefBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func derefFloat(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

func adminProviderRow(p models.ProviderCatalog, modelCount int64) map[string]any {
	return map[string]any{
		"id":              p.ID,
		"slug":            p.Slug,
		"name":            p.Name,
		"description":     p.Description,
		"icon":            p.Icon,
		"capabilities":    decodeJSONArray(p.Capabilities),
		"enabled":         p.Enabled,
		"min_app_version": p.MinAppVersion,
		"sort_order":      p.SortOrder,
		"api_key": map[string]any{
			"enabled":        p.ApiKeyEnabled,
			"base_url":       p.ApiKeyBaseURL,
			"env_keys":       decodeJSONArray(p.ApiKeyEnvKeys),
			"docs_url":       p.ApiKeyDocsURL,
			"signup_url":     p.ApiKeySignupURL,
			"has_shared_key": p.ApiKeyHasSharedKey,
		},
		"monthly": map[string]any{
			"enabled":      p.MonthlyEnabled,
			"auth_type":    p.MonthlyAuthType,
			"base_url":     p.MonthlyBaseURL,
			"oauth_config": decodeJSONObject(p.MonthlyOAuthConfig),
			"docs_url":     p.MonthlyDocsURL,
			"signup_url":   p.MonthlySignupURL,
		},
		"model_count": modelCount,
		"created_at":  p.CreatedAt.Format(time.RFC3339),
		"updated_at":  p.UpdatedAt.Format(time.RFC3339),
	}
}

func adminModelRow(m models.ProviderCatalogModel) map[string]any {
	return map[string]any{
		"id":                      m.ID,
		"provider_id":             m.ProviderID,
		"model_id":                m.ModelID,
		"name":                    m.Name,
		"capabilities":            decodeJSONArray(m.Capabilities),
		"context_window":          m.ContextWindow,
		"max_output_tokens":       m.MaxOutputTokens,
		"input_cost_per_1m":       m.InputCostPer1M,
		"output_cost_per_1m":      m.OutputCostPer1M,
		"cache_read_cost_per_1m":  m.CacheReadCostPer1M,
		"cache_write_cost_per_1m": m.CacheWriteCostPer1M,
		"available_on_api_key":    m.AvailableOnApiKey,
		"available_on_monthly":    m.AvailableOnMonthly,
		"enabled":                 m.Enabled,
		"default":                 m.Default,
		"deprecated":              m.Deprecated,
		"replaced_by":             m.ReplacedBy,
		"min_app_version":         m.MinAppVersion,
		"sort_order":              m.SortOrder,
		"locked":                  m.Locked,
		"request_spec":            rawJSONOrNil(m.RequestSpec),
		"tier_hint":               m.TierHint,
		"created_at":              m.CreatedAt.Format(time.RFC3339),
		"updated_at":              m.UpdatedAt.Format(time.RFC3339),
	}
}
