package handlers

// Public surface for the Construct provider.
//
// OpenAI-compatible (preferred):
//   GET  /api/inference/v1/models             — OpenAI list-models shape
//   POST /api/inference/v1/chat/completions   — see construct_chat.go
//
// Legacy aliases (kept for the desktop release that still calls them):
//   GET  /api/construct/models                — bag-of-fields snapshot
//
// Both URLs read the same picker-entries table. Cached briefly.

import (
	"net/http"
	"sync"
	"time"

	"construct/provider/internal/database"
	"construct/provider/internal/models"
)

type constructModelPublic struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	Description  string   `json:"description,omitempty"`
	Icon         string   `json:"icon,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type constructPublicSnap struct {
	DailyAllowance int                    `json:"daily_allowance"`
	Enabled        bool                   `json:"enabled"`
	Models         []constructModelPublic `json:"models"`
	fetched        time.Time
}

var (
	publicCache constructPublicSnap
	publicMu    sync.RWMutex
)

const publicStaleAfter = 30 * time.Second

func loadConstructPublic() constructPublicSnap {
	publicMu.RLock()
	if !publicCache.fetched.IsZero() && time.Since(publicCache.fetched) < publicStaleAfter {
		snap := publicCache
		publicMu.RUnlock()
		return snap
	}
	publicMu.RUnlock()

	cfg := loadOrInitConfig()
	var rows []models.ConstructPickerEntry
	if cfg.Enabled {
		database.DB.Where("enabled = ?", true).Order("sort_order ASC, label ASC").Find(&rows)
	}

	out := make([]constructModelPublic, 0, len(rows))
	for _, p := range rows {
		out = append(out, constructModelPublic{
			ID:           p.ID,
			Label:        p.Label,
			Description:  p.Description,
			Icon:         p.Icon,
			Capabilities: decodeJSONArray(p.Capabilities),
		})
	}

	snap := constructPublicSnap{
		DailyAllowance: cfg.DailyAllowance,
		Enabled:        cfg.Enabled,
		Models:         out,
		fetched:        time.Now(),
	}
	publicMu.Lock()
	publicCache = snap
	publicMu.Unlock()
	return snap
}

// ListConstructModels — GET /api/construct/models (legacy shape).
// Kept so the existing desktop build doesn't need a release; new code
// should hit /api/inference/v1/models.
func ListConstructModels(w http.ResponseWriter, r *http.Request) {
	snap := loadConstructPublic()
	w.Header().Set("Cache-Control", "public, max-age=30")
	WriteJSON(w, 200, map[string]any{
		"models":          snap.Models,
		"daily_allowance": snap.DailyAllowance,
		"enabled":         snap.Enabled,
	})
}

// ListInferenceModels — GET /api/inference/v1/models
// OpenAI list-models shape so any OpenAI SDK works against this URL
// without configuration:
//
//	{"object": "list", "data": [{"id": "source", "object": "model",
//	  "owned_by": "construct", "created": <unix>}]}
func ListInferenceModels(w http.ResponseWriter, r *http.Request) {
	snap := loadConstructPublic()
	now := time.Now().Unix()
	data := make([]map[string]any, 0, len(snap.Models))
	for _, m := range snap.Models {
		data = append(data, map[string]any{
			"id":       m.ID,
			"object":   "model",
			"owned_by": "construct",
			"created":  now,
		})
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	WriteJSON(w, 200, map[string]any{
		"object": "list",
		"data":   data,
	})
}
