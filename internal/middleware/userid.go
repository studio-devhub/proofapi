package middleware

import (
	"encoding/json"
	"net/http"
	"regexp"
)

// clientIDPattern: alphanumeric, dash, underscore, dot — max 128 chars.
// Prevents injection and limits cross-tenant lookup surface.
var clientIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]{1,128}$`)

// RequireClientID rejects requests missing or malformed X-Client-ID header.
// Applied only on dictionary management endpoints.
func RequireClientID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Client-ID")
		if id == "" {
			writeClientIDError(w, "X-Client-ID header is required")
			return
		}
		if !clientIDPattern.MatchString(id) {
			writeClientIDError(w, "X-Client-ID must be 1-128 alphanumeric characters (a-z, A-Z, 0-9, -, _, .)")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ValidateClientID returns true if the clientID is safe to use.
// Use this for optional clientID on /v1/check and WebSocket paths.
func ValidateClientID(id string) bool {
	return id == "" || clientIDPattern.MatchString(id)
}

func writeClientIDError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
