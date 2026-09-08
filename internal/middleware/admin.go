package middleware

import (
	"net/http"

	"construct/provider/internal/gwauth"
)

// AdminAuth accepts requests carrying a matching X-Internal-Secret
// header. Used by oracle's control-plane endpoints and any other
// internal caller; never reachable from the public internet.
func AdminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gwauth.Trusted(r) {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
