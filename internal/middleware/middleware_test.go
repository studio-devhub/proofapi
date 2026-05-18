package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"languagetool-backend/internal/middleware"
)

func nextHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// ── API Key ───────────────────────────────────────────────

func TestAPIKey_Valid(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	req.Header.Set("X-API-Key", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPIKey_Invalid(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	req.Header.Set("X-API-Key", "wrong")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKey_Missing(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKey_EmptyKey(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())
	req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	req.Header.Set("X-API-Key", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// ── Rate Limit ────────────────────────────────────────────

func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	handler := middleware.RateLimit(5, time.Minute)(nextHandler())
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "1.2.3.4:1000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "request %d should pass", i+1)
	}
}

func TestRateLimit_BlocksOverLimit(t *testing.T) {
	handler := middleware.RateLimit(3, time.Minute)(nextHandler())

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "9.9.9.9:1000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "9.9.9.9:1000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestRateLimit_DifferentIPs(t *testing.T) {
	handler := middleware.RateLimit(2, time.Minute)(nextHandler())

	for _, ip := range []string{"1.1.1.1:0", "2.2.2.2:0", "3.3.3.3:0"} {
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.RemoteAddr = ip
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	}
}

func TestRateLimit_Concurrent(t *testing.T) {
	handler := middleware.RateLimit(10, time.Minute)(nextHandler())

	var wg sync.WaitGroup
	results := make([]int, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.RemoteAddr = "5.5.5.5:0"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			results[idx] = w.Code
		}(i)
	}
	wg.Wait()

	ok, limited := 0, 0
	for _, code := range results {
		if code == http.StatusOK {
			ok++
		}
		if code == http.StatusTooManyRequests {
			limited++
		}
	}
	assert.Equal(t, 10, ok)
	assert.Equal(t, 10, limited)
}

func TestRateLimit_WindowReset(t *testing.T) {
	handler := middleware.RateLimit(2, 100*time.Millisecond)(nextHandler())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "7.7.7.7:0"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Over limit
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "7.7.7.7:0"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Wait for window reset
	time.Sleep(150 * time.Millisecond)

	// Should pass again
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.RemoteAddr = "7.7.7.7:0"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}
