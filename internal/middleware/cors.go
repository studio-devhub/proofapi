package middleware

import "net/http"

const (
	allowOrigin  = "*"
	allowMethods = "GET, POST, DELETE, OPTIONS"
	allowHeaders = "Content-Type, X-API-Key, X-Client-ID, X-User-ID, Authorization"
	maxAge       = "86400"
)

func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", allowMethods)
		w.Header().Set("Access-Control-Allow-Headers", allowHeaders)

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Max-Age", maxAge)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
