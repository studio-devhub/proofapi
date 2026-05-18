package middleware

import (
	"net/http"
	"sync"
	"time"
)

type visitor struct {
	count    int
	windowAt time.Time
}

func RateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	mu := sync.Mutex{}
	visitors := make(map[string]*visitor)

	// Purge stale entries every 5 minutes to prevent unbounded memory growth
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for ip, v := range visitors {
				if now.Sub(v.windowAt) > window {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			now := time.Now()

			mu.Lock()
			v, ok := visitors[ip]
			if !ok || now.Sub(v.windowAt) > window {
				visitors[ip] = &visitor{count: 1, windowAt: now}
				mu.Unlock()
				next.ServeHTTP(w, r)
				return
			}
			v.count++
			if v.count > limit {
				mu.Unlock()
				jsonError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}
