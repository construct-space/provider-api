package handlers

// Per-model sync from models.dev. Ported verbatim from source-api.
// Lock semantics: admin saves flip locked=true; sync skips locked rows
// with 409. Unlock explicitly via POST /unlock.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"construct/provider/internal/database"
	"construct/provider/internal/models"
)

const modelsDevURL = "https://models.dev/api.json"

type modelsDevModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Attachment  bool   `json:"attachment"`
	Reasoning   bool   `json:"reasoning"`
	ToolCall    bool   `json:"tool_call"`
	Temperature bool   `json:"temperature"`
	Cost        struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"cost"`
	Limit struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
}

type modelsDevProvider struct {
	ID     string                    `json:"id"`
	Models map[string]modelsDevModel `json:"models"`
}

var fetchModelsDev = func() (map[string]modelsDevProvider, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(modelsDevURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("models.dev returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out map[string]modelsDevProvider
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode models.dev: %w", err)
	}
	return out, nil
}

func capabilitiesFromModelsDev(m modelsDevModel) []string {
	caps := []string{}
	if m.ToolCall {
		caps = append(caps, "tools")
	}
	if m.Attachment {
		caps = append(caps, "vision")
	}
	if m.Reasoning {
		caps = append(caps, "reasoning")
	}
	return caps
}

// POST /api/admin/providers/{id}/models/{modelId}/sync
func AdminSyncProviderModel(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	providerID := r.PathValue("id")
	modelRowID := r.PathValue("modelId")

	var row models.ProviderCatalogModel
	if err := database.DB.Where("provider_id = ? AND id = ?", providerID, modelRowID).First(&row).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "model not found"})
		return
	}

	if row.Locked {
		WriteJSON(w, 409, map[string]any{
			"error":  "model is locked — admin edits present",
			"locked": true,
		})
		return
	}

	var prov models.ProviderCatalog
	if err := database.DB.Where("id = ?", providerID).First(&prov).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "provider not found"})
		return
	}
	slug := prov.Slug
	if slug == "" {
		slug = providerID
	}

	catalog, err := fetchModelsDev()
	if err != nil {
		WriteJSON(w, 502, map[string]any{"error": "models.dev unreachable: " + err.Error()})
		return
	}

	remoteProv, ok := catalog[slug]
	if !ok {
		knownProviders := make([]string, 0, len(catalog))
		for id := range catalog {
			knownProviders = append(knownProviders, id)
		}
		sort.Strings(knownProviders)
		WriteJSON(w, 404, map[string]any{
			"error": fmt.Sprintf("slug %q not on models.dev. Upstream providers: %s",
				slug, strings.Join(knownProviders, ", ")),
			"searched":        slug,
			"known_providers": knownProviders,
		})
		return
	}

	remoteModel, ok := remoteProv.Models[row.ModelID]
	if !ok {
		wanted := strings.ToLower(row.ModelID)
		for id, m := range remoteProv.Models {
			if strings.ToLower(id) == wanted {
				remoteModel = m
				ok = true
				break
			}
		}
	}
	if !ok {
		availableModels := make([]string, 0, len(remoteProv.Models))
		for id := range remoteProv.Models {
			availableModels = append(availableModels, id)
		}
		sort.Strings(availableModels)
		WriteJSON(w, 404, map[string]any{
			"error": fmt.Sprintf("model %q not on models.dev under %q. Upstream has: %s",
				row.ModelID, slug, strings.Join(availableModels, ", ")),
			"searched":         slug + "/" + row.ModelID,
			"available_models": availableModels,
		})
		return
	}

	updates := map[string]any{
		"name":                    remoteModel.Name,
		"capabilities":            encodeJSON(capabilitiesFromModelsDev(remoteModel)),
		"context_window":          remoteModel.Limit.Context,
		"max_output_tokens":       remoteModel.Limit.Output,
		"input_cost_per_1m":       remoteModel.Cost.Input,
		"output_cost_per_1m":      remoteModel.Cost.Output,
		"cache_read_cost_per_1m":  remoteModel.Cost.CacheRead,
		"cache_write_cost_per_1m": remoteModel.Cost.CacheWrite,
	}

	if err := database.DB.Model(&row).Updates(updates).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "sync failed: " + err.Error()})
		return
	}
	database.DB.Where("provider_id = ? AND id = ?", providerID, modelRowID).First(&row)
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"model": adminModelRow(row)})
}

// POST /api/admin/providers/{id}/models/{modelId}/unlock
func AdminUnlockProviderModel(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	providerID := r.PathValue("id")
	modelRowID := r.PathValue("modelId")

	var row models.ProviderCatalogModel
	if err := database.DB.Where("provider_id = ? AND id = ?", providerID, modelRowID).First(&row).Error; err != nil {
		WriteJSON(w, 404, map[string]any{"error": "model not found"})
		return
	}
	if err := database.DB.Model(&row).Update("locked", false).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": "unlock failed"})
		return
	}
	row.Locked = false
	bumpCatalogVersion()
	WriteJSON(w, 200, map[string]any{"model": adminModelRow(row)})
}
