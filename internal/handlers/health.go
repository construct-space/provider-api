package handlers

import (
	"encoding/json"
	"net/http"

	"construct/provider/internal/database"
)

// Health returns 200 + {status:"ok"} when the process can talk to
// the credits DB. CapRover health check + load balancer probe both
// use this.
func Health(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if database.DB != nil {
		sql, err := database.DB.DB()
		if err != nil || sql.Ping() != nil {
			status = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  status,
		"service": "provider-api",
	})
}
