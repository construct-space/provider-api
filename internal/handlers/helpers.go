package handlers

import (
	"encoding/json"
	"net/http"

	"construct/provider/internal/gwauth"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// requireInternalSecret gates admin endpoints. Oracle (the only caller
// today) signs requests with X-Internal-Secret. Unlike source-api, we
// don't honour a legacy SERVICE_API_KEY env var — only the unified
// INTERNAL_SHARED_SECRET.
func requireInternalSecret(w http.ResponseWriter, r *http.Request) bool {
	if !gwauth.Trusted(r) {
		WriteJSON(w, 401, map[string]any{"error": "unauthorized"})
		return false
	}
	return true
}
