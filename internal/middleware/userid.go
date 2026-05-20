package middleware

import (
	"encoding/json"
	"net/http"
)

// RequireClientID rejects requests missing the X-Client-ID header.
// Applied only on dictionary management endpoints.
func RequireClientID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Client-ID") == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "X-Client-ID header is required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
