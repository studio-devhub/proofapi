package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/middleware"
)

// ── jsonError: Content-Type header ───────────────────────

func TestAPIKey_ErrorResponse_ContentType(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())

	req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	req.Header.Set("X-API-Key", "wrong")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestAPIKey_ErrorResponse_ValidJSON(t *testing.T) {
	handler := middleware.APIKey("secret")(nextHandler())

	req := httptest.NewRequest(http.MethodPost, "/v1/check", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "unauthorized", body["error"])
}

func TestAPIKeyWS_ErrorResponse_ContentType(t *testing.T) {
	handler := middleware.APIKeyWS("secret")(nextHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/ws", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestRateLimit_ErrorResponse_ContentType(t *testing.T) {
	handler := middleware.RateLimit(1, time.Minute)(nextHandler())

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.RemoteAddr = "ct-test:1000"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		_ = w
	}

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "ct-test:1000"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "rate limit exceeded", body["error"])
}
