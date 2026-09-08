package handlers

// Admin CRUD for Construct — the platform-managed multi-upstream
// provider. Oracle is the only caller; all endpoints sit behind
// requireInternalSecret. Mutations have no audit logging here —
// oracle-api wraps proxy calls with its own audit pipeline.
//
// Layout:
//   /api/admin/construct/upstreams        (CRUD on upstream providers + keys)
//   /api/admin/construct/picker-entries   (CRUD on user-facing picker rows)
//   /api/admin/construct/routing-targets  (CRUD on internal aliases used by source-family routing)
//   /api/admin/construct/config           (singleton: daily allowance + kill switch)
//   /api/admin/construct/users/{id}       (balance, grant, block, unblock)

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"construct/provider/internal/crypto"
	"construct/provider/internal/database"
	"construct/provider/internal/models"

	"gorm.io/gorm"
)

// ─── Upstream providers ───────────────────────────────────────────────────

type upstreamBody struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	BaseURL    string  `json:"base_url"`
	AuthHeader string  `json:"auth_header"` // "authorization-bearer" | "x-api-key"
	APIKey     string  `json:"api_key"`     // raw key from admin; encrypted on save
	Enabled    *bool   `json:"enabled,omitempty"`
}

func upstreamRow(u models.ConstructUpstreamProvider) map[string]any {
	keyHint := ""
	if u.APIKeyEncrypted != "" {
		// The key the admin pasted is unrecoverable in plaintext from here;
		// surface a fixed "configured" marker instead. The set/replace
		// dialog never reads the current value.
		keyHint = "configured"
	}
	return map[string]any{
		"id":          u.ID,
		"label":       u.Label,
		"base_url":    u.BaseURL,
		"auth_header": u.AuthHeader,
		"enabled":     u.Enabled,
		"api_key":     keyHint,
		"updated_at":  u.UpdatedAt.Format(time.RFC3339),
		"updated_by":  u.UpdatedBy,
	}
}

// GET /api/admin/construct/upstreams
func AdminListConstructUpstreams(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	var rows []models.ConstructUpstreamProvider
	database.DB.Order("label ASC").Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, u := range rows { out = append(out, upstreamRow(u)) }
	WriteJSON(w, 200, map[string]any{"upstreams": out, "total": len(out)})
}

// POST /api/admin/construct/upstreams
func AdminCreateConstructUpstream(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	var body upstreamBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"}); return
	}
	body.ID = strings.TrimSpace(body.ID)
	body.Label = strings.TrimSpace(body.Label)
	body.BaseURL = strings.TrimSpace(body.BaseURL)
	body.AuthHeader = strings.TrimSpace(body.AuthHeader)
	if body.ID == "" || body.Label == "" || body.BaseURL == "" || body.APIKey == "" {
		WriteJSON(w, 400, map[string]any{"error": "id, label, base_url, api_key required"}); return
	}
	if body.AuthHeader == "" { body.AuthHeader = "authorization-bearer" }
	enc, err := crypto.Encrypt(body.APIKey)
	if err != nil { WriteJSON(w, 500, map[string]any{"error": "encrypt: " + err.Error()}); return }
	u := models.ConstructUpstreamProvider{
		ID:              body.ID,
		Label:           body.Label,
		BaseURL:         body.BaseURL,
		AuthHeader:      body.AuthHeader,
		APIKeyEncrypted: enc,
		Enabled:         derefBool(body.Enabled, true),
	}
	if err := database.DB.Create(&u).Error; err != nil {
		WriteJSON(w, 400, map[string]any{"error": "create failed: " + err.Error()}); return
	}
	WriteJSON(w, 201, map[string]any{"upstream": upstreamRow(u)})
}

// PUT /api/admin/construct/upstreams/{id}
// API key is only updated when the body carries a non-empty `api_key`.
func AdminUpdateConstructUpstream(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	id := r.PathValue("id")
	var u models.ConstructUpstreamProvider
	if err := database.DB.First(&u, "id = ?", id).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "upstream not found"}); return
	}
	var body upstreamBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"}); return
	}
	updates := map[string]any{}
	if body.Label != ""      { updates["label"]       = body.Label }
	if body.BaseURL != ""    { updates["base_url"]    = body.BaseURL }
	if body.AuthHeader != "" { updates["auth_header"] = body.AuthHeader }
	if body.Enabled != nil   { updates["enabled"]     = *body.Enabled }
	if body.APIKey != "" {
		enc, err := crypto.Encrypt(body.APIKey)
		if err != nil { WriteJSON(w, 500, map[string]any{"error": "encrypt: " + err.Error()}); return }
		updates["api_key_encrypted"] = enc
	}
	if err := database.DB.Model(&u).Updates(updates).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "update failed"}); return
	}
	database.DB.First(&u, "id = ?", id)
	WriteJSON(w, 200, map[string]any{"upstream": upstreamRow(u)})
}

// DELETE /api/admin/construct/upstreams/{id}
func AdminDeleteConstructUpstream(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	id := r.PathValue("id")
	// Reject if any routing target still references this upstream —
	// admins must reassign or delete the targets first.
	var refs int64
	database.DB.Model(&models.ConstructRoutingTarget{}).Where("upstream_provider_id = ?", id).Count(&refs)
	if refs > 0 {
		WriteJSON(w, 409, map[string]any{"error": "upstream is in use", "routing_target_count": refs}); return
	}
	if err := database.DB.Delete(&models.ConstructUpstreamProvider{}, "id = ?", id).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "delete failed"}); return
	}
	WriteJSON(w, 200, map[string]any{"ok": true})
}

// ─── Picker entries ───────────────────────────────────────────────────────
//
// User-facing chat entries shown in the desktop picker. Today there is
// one row, `source`, which fans out via source_family routing.

type pickerEntryBody struct {
	ID                    string   `json:"id"`
	Label                 string   `json:"label"`
	Description           string   `json:"description"`
	Icon                  string   `json:"icon"`
	RouteViaOperator      string   `json:"route_via_operator"`
	RouteViaOperatorLarge string   `json:"route_via_operator_large"`
	RouteViaOperatorSmall string   `json:"route_via_operator_small"`
	Capabilities          []string `json:"capabilities,omitempty"`
	Enabled               *bool    `json:"enabled,omitempty"`
	SortOrder             int      `json:"sort_order"`
}

func pickerEntryRow(p models.ConstructPickerEntry) map[string]any {
	return map[string]any{
		"id":                       p.ID,
		"label":                    p.Label,
		"description":              p.Description,
		"icon":                     p.Icon,
		"route_via_operator":       p.RouteViaOperator,
		"route_via_operator_large": p.RouteViaOperatorLarge,
		"route_via_operator_small": p.RouteViaOperatorSmall,
		"capabilities":             decodeJSONArray(p.Capabilities),
		"enabled":                  p.Enabled,
		"sort_order":               p.SortOrder,
		"created_at":               p.CreatedAt.Format(time.RFC3339),
		"updated_at":               p.UpdatedAt.Format(time.RFC3339),
	}
}

// GET /api/admin/construct/picker-entries
func AdminListPickerEntries(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	var rows []models.ConstructPickerEntry
	database.DB.Order("sort_order ASC, label ASC").Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		out = append(out, pickerEntryRow(p))
	}
	WriteJSON(w, 200, map[string]any{"entries": out, "total": len(out)})
}

// POST /api/admin/construct/picker-entries
func AdminCreatePickerEntry(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	var body pickerEntryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	if body.ID == "" || body.Label == "" {
		WriteJSON(w, 400, map[string]any{"error": "id, label required"})
		return
	}
	routeOp := strings.TrimSpace(body.RouteViaOperator)
	if routeOp == "" {
		routeOp = "tank"
	}
	p := models.ConstructPickerEntry{
		ID:                    body.ID,
		Label:                 body.Label,
		Description:           body.Description,
		Icon:                  body.Icon,
		RouteViaOperator:      routeOp,
		RouteViaOperatorLarge: strings.TrimSpace(body.RouteViaOperatorLarge),
		RouteViaOperatorSmall: strings.TrimSpace(body.RouteViaOperatorSmall),
		Capabilities:          encodeJSON(body.Capabilities),
		Enabled:               derefBool(body.Enabled, true),
		SortOrder:             body.SortOrder,
	}
	if err := database.DB.Create(&p).Error; err != nil {
		WriteJSON(w, 400, map[string]any{"error": "create failed: " + err.Error()})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 201, map[string]any{"entry": pickerEntryRow(p)})
}

// PUT /api/admin/construct/picker-entries/{id}
func AdminUpdatePickerEntry(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := r.PathValue("id")
	var p models.ConstructPickerEntry
	if err := database.DB.First(&p, "id = ?", id).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "picker entry not found"})
		return
	}
	var body pickerEntryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	updates := map[string]any{}
	if body.Label != "" {
		updates["label"] = body.Label
	}
	if body.Description != "" {
		updates["description"] = body.Description
	}
	if body.Icon != "" {
		updates["icon"] = body.Icon
	}
	if body.RouteViaOperator != "" {
		updates["route_via_operator"] = body.RouteViaOperator
	}
	// Large / small are optional tier overrides — admins can clear them
	// by sending an empty string ("fall back to the medium route"), so
	// these always overwrite. Same applies to anyone unsetting a tier.
	updates["route_via_operator_large"] = strings.TrimSpace(body.RouteViaOperatorLarge)
	updates["route_via_operator_small"] = strings.TrimSpace(body.RouteViaOperatorSmall)
	if body.Capabilities != nil {
		updates["capabilities"] = encodeJSON(body.Capabilities)
	}
	if body.Enabled != nil {
		updates["enabled"] = *body.Enabled
	}
	if body.SortOrder != 0 {
		updates["sort_order"] = body.SortOrder
	}
	if err := database.DB.Model(&p).Updates(updates).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "update failed"})
		return
	}
	database.DB.First(&p, "id = ?", id)
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"entry": pickerEntryRow(p)})
}

// DELETE /api/admin/construct/picker-entries/{id}
func AdminDeletePickerEntry(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := database.DB.Delete(&models.ConstructPickerEntry{}, "id = ?", id).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "delete failed"})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"ok": true})
}

// ─── Routing targets ──────────────────────────────────────────────────────
//
// Internal aliases referenced by source_family_routes. Each binds a
// stable name (Apoc, Trinity, …) to an upstream + model id + cost.

type routingTargetBody struct {
	ID                       string   `json:"id"`
	UpstreamProviderID       string   `json:"upstream_provider_id"`
	UpstreamModel            string   `json:"upstream_model"`
	Label                    string   `json:"label"`
	Description              string   `json:"description"`
	Icon                     string   `json:"icon"`
	CreditsPerPrompt         int      `json:"credits_per_prompt"`
	MaxToolCallsPerCredit    int      `json:"max_tool_calls_per_credit"`
	MaxOutputTokensPerCredit int      `json:"max_output_tokens_per_credit"`
	ThinkingMode             string   `json:"thinking_mode"`
	Capabilities             []string `json:"capabilities,omitempty"`
	Enabled                  *bool    `json:"enabled,omitempty"`
	SortOrder                int      `json:"sort_order"`
}

func routingTargetRow(t models.ConstructRoutingTarget) map[string]any {
	return map[string]any{
		"id":                           t.ID,
		"upstream_provider_id":         t.UpstreamProviderID,
		"upstream_model":               t.UpstreamModel,
		"label":                        t.Label,
		"description":                  t.Description,
		"icon":                         t.Icon,
		"credits_per_prompt":           t.CreditsPerPrompt,
		"max_tool_calls_per_credit":    t.MaxToolCallsPerCredit,
		"max_output_tokens_per_credit": t.MaxOutputTokensPerCredit,
		"thinking_mode":                t.ThinkingMode,
		"capabilities":                 decodeJSONArray(t.Capabilities),
		"enabled":                      t.Enabled,
		"sort_order":                   t.SortOrder,
		"created_at":                   t.CreatedAt.Format(time.RFC3339),
		"updated_at":                   t.UpdatedAt.Format(time.RFC3339),
	}
}

// GET /api/admin/construct/routing-targets
func AdminListRoutingTargets(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	var rows []models.ConstructRoutingTarget
	database.DB.Order("sort_order ASC, label ASC").Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		out = append(out, routingTargetRow(t))
	}
	WriteJSON(w, 200, map[string]any{"targets": out, "total": len(out)})
}

// POST /api/admin/construct/routing-targets
func AdminCreateRoutingTarget(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	var body routingTargetBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	body.UpstreamProviderID = strings.TrimSpace(body.UpstreamProviderID)
	if body.ID == "" || body.UpstreamProviderID == "" || body.UpstreamModel == "" || body.Label == "" {
		WriteJSON(w, 400, map[string]any{"error": "id, upstream_provider_id, upstream_model, label required"})
		return
	}
	var ucount int64
	database.DB.Model(&models.ConstructUpstreamProvider{}).Where("id = ?", body.UpstreamProviderID).Count(&ucount)
	if ucount == 0 {
		WriteJSON(w, 400, map[string]any{"error": "unknown upstream_provider_id"})
		return
	}
	t := models.ConstructRoutingTarget{
		ID:                       body.ID,
		UpstreamProviderID:       body.UpstreamProviderID,
		UpstreamModel:            body.UpstreamModel,
		Label:                    body.Label,
		Description:              body.Description,
		Icon:                     body.Icon,
		CreditsPerPrompt:         clampInt(body.CreditsPerPrompt, 1, 1000, 1),
		MaxToolCallsPerCredit:    clampInt(body.MaxToolCallsPerCredit, 1, 1000, 20),
		MaxOutputTokensPerCredit: clampInt(body.MaxOutputTokensPerCredit, 1, 1<<20, 4096),
		ThinkingMode:             body.ThinkingMode,
		Capabilities:             encodeJSON(body.Capabilities),
		Enabled:                  derefBool(body.Enabled, true),
		SortOrder:                body.SortOrder,
	}
	if err := database.DB.Create(&t).Error; err != nil {
		WriteJSON(w, 400, map[string]any{"error": "create failed: " + err.Error()})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 201, map[string]any{"target": routingTargetRow(t)})
}

// PUT /api/admin/construct/routing-targets/{id}
func AdminUpdateRoutingTarget(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := r.PathValue("id")
	var t models.ConstructRoutingTarget
	if err := database.DB.First(&t, "id = ?", id).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "routing target not found"})
		return
	}
	var body routingTargetBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	updates := map[string]any{}
	if body.UpstreamProviderID != "" {
		var ucount int64
		database.DB.Model(&models.ConstructUpstreamProvider{}).Where("id = ?", body.UpstreamProviderID).Count(&ucount)
		if ucount == 0 {
			WriteJSON(w, 400, map[string]any{"error": "unknown upstream_provider_id"})
			return
		}
		updates["upstream_provider_id"] = body.UpstreamProviderID
	}
	if body.UpstreamModel != "" {
		updates["upstream_model"] = body.UpstreamModel
	}
	if body.Label != "" {
		updates["label"] = body.Label
	}
	if body.Description != "" {
		updates["description"] = body.Description
	}
	if body.Icon != "" {
		updates["icon"] = body.Icon
	}
	if body.CreditsPerPrompt > 0 {
		updates["credits_per_prompt"] = body.CreditsPerPrompt
	}
	if body.MaxToolCallsPerCredit > 0 {
		updates["max_tool_calls_per_credit"] = body.MaxToolCallsPerCredit
	}
	if body.MaxOutputTokensPerCredit > 0 {
		updates["max_output_tokens_per_credit"] = body.MaxOutputTokensPerCredit
	}
	if body.ThinkingMode != "" {
		updates["thinking_mode"] = body.ThinkingMode
	}
	if body.Capabilities != nil {
		updates["capabilities"] = encodeJSON(body.Capabilities)
	}
	if body.Enabled != nil {
		updates["enabled"] = *body.Enabled
	}
	if body.SortOrder != 0 {
		updates["sort_order"] = body.SortOrder
	}
	if err := database.DB.Model(&t).Updates(updates).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "update failed"})
		return
	}
	database.DB.First(&t, "id = ?", id)
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"target": routingTargetRow(t)})
}

// DELETE /api/admin/construct/routing-targets/{id}
// 409s when source_family_routes still reference this target.
func AdminDeleteRoutingTarget(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := r.PathValue("id")
	var refs int64
	database.DB.Model(&models.SourceFamilyRoute{}).Where("model_id = ?", id).Count(&refs)
	if refs > 0 {
		WriteJSON(w, 409, map[string]any{"error": "routing target is in use by source-family routes", "route_count": refs})
		return
	}
	if err := database.DB.Delete(&models.ConstructRoutingTarget{}, "id = ?", id).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "delete failed"})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"ok": true})
}

// ─── Config ───────────────────────────────────────────────────────────────

type configBody struct {
	DailyAllowance *int  `json:"daily_allowance,omitempty"`
	Enabled        *bool `json:"enabled,omitempty"`
	UpdatedBy      string `json:"updated_by,omitempty"`
}

func loadOrInitConfig() models.ConstructConfig {
	var c models.ConstructConfig
	if err := database.DB.First(&c, 1).Error; err == gorm.ErrRecordNotFound {
		c = models.ConstructConfig{ID: 1, DailyAllowance: 100, Enabled: true, UpdatedAt: time.Now()}
		database.DB.Create(&c)
	}
	return c
}

// GET /api/admin/construct/config
func AdminGetConstructConfig(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	c := loadOrInitConfig()
	WriteJSON(w, 200, map[string]any{"config": c})
}

// PUT /api/admin/construct/config
func AdminUpdateConstructConfig(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	c := loadOrInitConfig()
	var body configBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"}); return
	}
	updates := map[string]any{"updated_at": time.Now()}
	if body.DailyAllowance != nil { updates["daily_allowance"] = clampInt(*body.DailyAllowance, 0, 100000, 100) }
	if body.Enabled != nil        { updates["enabled"]         = *body.Enabled }
	if body.UpdatedBy != ""       { updates["updated_by"]      = body.UpdatedBy }
	if err := database.DB.Model(&c).Where("id = ?", 1).Updates(updates).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "update failed"}); return
	}
	database.DB.First(&c, 1)
	WriteJSON(w, 200, map[string]any{"config": c})
}

// ─── Users ────────────────────────────────────────────────────────────────

// GET /api/admin/construct/users/{id}
// Returns the user's balance and last 50 ledger rows.
func AdminGetConstructUser(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	id := r.PathValue("id")

	var bal models.CreditsBalance
	if err := database.DB.First(&bal, "user_id = ?", id).Error; err == gorm.ErrRecordNotFound {
		bal = models.CreditsBalance{UserID: id, DailyUsed: 0, DailyDate: time.Now().UTC()}
	}

	var ledger []models.CreditsLedger
	database.DB.Where("user_id = ?", id).Order("created_at DESC").Limit(50).Find(&ledger)

	WriteJSON(w, 200, map[string]any{"balance": bal, "ledger": ledger})
}

type grantBody struct {
	Delta  int    `json:"delta"`
	Reason string `json:"reason"`
}

// POST /api/admin/construct/users/{id}/grant
func AdminGrantConstructCredits(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	id := r.PathValue("id")
	var body grantBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"}); return
	}
	if body.Delta == 0 {
		WriteJSON(w, 400, map[string]any{"error": "delta must be non-zero"}); return
	}
	// upsert balance + insert ledger row in a transaction.
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var bal models.CreditsBalance
		if err := tx.First(&bal, "user_id = ?", id).Error; err == gorm.ErrRecordNotFound {
			bal = models.CreditsBalance{UserID: id, DailyDate: time.Now().UTC()}
			if err := tx.Create(&bal).Error; err != nil { return err }
		}
		newPaid := max(bal.PaidBalance+body.Delta, 0)
		if err := tx.Model(&bal).Where("user_id = ?", id).Updates(map[string]any{
			"paid_balance": newPaid,
			"updated_at":   time.Now(),
		}).Error; err != nil { return err }
		return tx.Create(&models.CreditsLedger{
			UserID:    id,
			Delta:     body.Delta,
			Kind:      "admin_grant",
			Meta:      encodeJSON(map[string]any{"reason": body.Reason}),
			CreatedAt: time.Now(),
		}).Error
	})
	if err != nil { WriteJSON(w, 500, map[string]any{"error": err.Error()}); return }
	var bal models.CreditsBalance
	database.DB.First(&bal, "user_id = ?", id)
	WriteJSON(w, 200, map[string]any{"balance": bal})
}

type blockBody struct {
	Reason string `json:"reason"`
}

// POST /api/admin/construct/users/{id}/block
func AdminBlockConstructUser(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	id := r.PathValue("id")
	var body blockBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := database.DB.Where("user_id = ?", id).Assign(models.CreditsBalance{
		UserID:        id,
		DailyDate:     time.Now().UTC(),
		Blocked:       true,
		BlockedReason: body.Reason,
	}).FirstOrCreate(&models.CreditsBalance{}).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": err.Error()}); return
	}
	WriteJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/admin/construct/users/{id}/unblock
func AdminUnblockConstructUser(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) { return }
	id := r.PathValue("id")
	if err := database.DB.Model(&models.CreditsBalance{}).Where("user_id = ?", id).Updates(map[string]any{
		"blocked":         false,
		"blocked_reason":  "",
		"updated_at":      time.Now(),
	}).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": err.Error()}); return
	}
	WriteJSON(w, 200, map[string]any{"ok": true})
}

// clampInt returns v if it's within [min,max], else `def`. Used to keep
// admin inputs sane without rejecting the whole request.
func clampInt(v, min, max, def int) int {
	if v < min || v > max { return def }
	return v
}
