package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"languagetool-backend/internal/middleware"
)

// ── APIKey Edge Cases ─────────────────────────────────────

func TestAPIKey_CaseSensitive(t *testing.T) {
	handler := middleware.APIKey("MySecretKey")(nextHandler())

	for _, key := range []string{"mysecretkey", "MYSECRETKEY", "MySecretkey"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
		req.Header.Set("X-API-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "key: %s", key)
	}
}

func TestAPIKey_MultipleHeaders(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())

	req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	// Set wrong then correct — HTTP takes first value
	req.Header["X-Api-Key"] = []string{"wrong", "secret"}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	// First value "wrong" should be used → unauthorized
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKey_WhitespaceKey(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())

	req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	req.Header.Set("X-API-Key", " secret ")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKey_AllHTTPMethods(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())

	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodDelete, http.MethodPatch,
	}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/v1/check", nil)
		req.Header.Set("X-API-Key", "secret")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "method: %s", method)
	}
}

// ── APIKeyWS Edge Cases ───────────────────────────────────

func TestAPIKeyWS_QueryParam(t *testing.T) {
	handler := middleware.APIKeyWS("secret")(nextHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/ws?api_key=secret", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPIKeyWS_HeaderTakesPrecedence(t *testing.T) {
	handler := middleware.APIKeyWS("secret")(nextHandler())

	// Header correct, query wrong
	req := httptest.NewRequest(http.MethodGet, "/v1/ws?api_key=wrong", nil)
	req.Header.Set("X-API-Key", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPIKeyWS_BothMissing(t *testing.T) {
	handler := middleware.APIKeyWS("secret")(nextHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/ws", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKeyWS_WrongQueryParam(t *testing.T) {
	handler := middleware.APIKeyWS("secret")(nextHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/ws?api_key=wrong", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// ── RateLimit Edge Cases ──────────────────────────────────

func TestRateLimit_ExactLimit(t *testing.T) {
	handler := middleware.RateLimit(5, time.Minute)(nextHandler())

	// Exactly at limit should pass
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "exact:1000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "req %d", i+1)
	}

	// One over limit
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "exact:1000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestRateLimit_LimitOne(t *testing.T) {
	handler := middleware.RateLimit(1, time.Minute)(nextHandler())

	req1 := httptest.NewRequest(http.MethodPost, "/", nil)
	req1.RemoteAddr = "1.1.1.1:0"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.RemoteAddr = "1.1.1.1:0"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
}

func TestRateLimit_ManyIPs(t *testing.T) {
	handler := middleware.RateLimit(1, time.Minute)(nextHandler())

	// 100 different IPs, each with 1 request — all should pass
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = fmt.Sprintf("10.0.%d.%d:1000", i/256, i%256)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "ip %d", i)
	}
}

func TestRateLimit_ResponseBody(t *testing.T) {
	handler := middleware.RateLimit(1, time.Minute)(nextHandler())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "body:test:1000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		_ = w
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "body:test:1000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Contains(t, w.Body.String(), "rate limit")
}
