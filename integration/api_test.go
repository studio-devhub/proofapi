package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/languagetool"
	"languagetool-backend/internal/middleware"
)

func buildServer(t *testing.T) *httptest.Server {
	t.Helper()

	ltSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v2/languages" {
			json.NewEncoder(w).Encode([]map[string]any{{"name": "English", "code": "en-US"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"matches": []map[string]any{
				{
					"message": "Grammar issue",
					"offset": 0, "length": 4,
					"replacements": []map[string]any{{"value": "These"}},
					"rule": map[string]any{
						"id": "G1", "issueType": "grammar",
						"category": map[string]any{"id": "G", "name": "Grammar"},
					},
					"context": map[string]any{"text": "This are", "offset": 0, "length": 4},
				},
			},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	t.Cleanup(ltSrv.Close)

	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: ltSrv.URL, Timeout: 5 * time.Second,
	})
	handler := languagetool.NewHandler(client, r, nil)

	router := chi.NewRouter()
	router.Use(middleware.APIKey("test-key"))
	router.Post("/v1/check", handler.Check)
	router.Get("/v1/health", handler.Health)
	router.Get("/v1/languages", handler.Languages)
	router.Delete("/v1/cache", handler.ClearCache)

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

func apiPost(t *testing.T, srv *httptest.Server, path string, body any, key string) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func apiGet(t *testing.T, srv *httptest.Server, path, key string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// ── Integration Tests ─────────────────────────────────────

func TestIntegration_CheckSuccess(t *testing.T) {
	srv := buildServer(t)
	resp := apiPost(t, srv, "/v1/check", map[string]any{
		"text": "This are a test", "language": "en-US",
	}, "test-key")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotNil(t, result["matches"])
	assert.Equal(t, false, result["cached"])
}

func TestIntegration_Unauthorized(t *testing.T) {
	srv := buildServer(t)
	resp := apiPost(t, srv, "/v1/check", map[string]any{"text": "Hello world"}, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_WrongAPIKey(t *testing.T) {
	srv := buildServer(t)
	resp := apiPost(t, srv, "/v1/check", map[string]any{"text": "Hello world"}, "bad-key")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_HealthEndpoint(t *testing.T) {
	srv := buildServer(t)
	resp := apiGet(t, srv, "/v1/health", "") // health bypasses auth
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "ok", result["api"])
}

func TestIntegration_LanguagesEndpoint(t *testing.T) {
	srv := buildServer(t)
	resp := apiGet(t, srv, "/v1/languages", "test-key")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestIntegration_CacheFlow(t *testing.T) {
	srv := buildServer(t)
	body := map[string]any{"text": "This are a test", "language": "en-US"}

	// First request — not cached
	r1 := apiPost(t, srv, "/v1/check", body, "test-key")
	defer r1.Body.Close()
	var resp1 map[string]any
	json.NewDecoder(r1.Body).Decode(&resp1)
	assert.Equal(t, false, resp1["cached"])

	// Wait for async cache write
	time.Sleep(100 * time.Millisecond)

	// Second request — cached
	r2 := apiPost(t, srv, "/v1/check", body, "test-key")
	defer r2.Body.Close()
	var resp2 map[string]any
	json.NewDecoder(r2.Body).Decode(&resp2)
	assert.Equal(t, true, resp2["cached"])
}

func TestIntegration_CacheClear(t *testing.T) {
	srv := buildServer(t)
	body := map[string]any{"text": "This are a test", "language": "en-US"}

	// Populate cache
	r1 := apiPost(t, srv, "/v1/check", body, "test-key")
	r1.Body.Close()
	time.Sleep(100 * time.Millisecond)

	// Clear cache
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/v1/cache", nil)
	req.Header.Set("X-API-Key", "test-key")
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Next request should be cache miss again
	r2 := apiPost(t, srv, "/v1/check", body, "test-key")
	defer r2.Body.Close()
	var result map[string]any
	json.NewDecoder(r2.Body).Decode(&result)
	assert.Equal(t, false, result["cached"])
}

func TestIntegration_ValidationErrors(t *testing.T) {
	srv := buildServer(t)

	tests := []struct {
		name string
		body map[string]any
	}{
		{"empty text", map[string]any{"text": ""}},
		{"short text", map[string]any{"text": "a"}},
		{"missing text", map[string]any{"language": "en-US"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := apiPost(t, srv, "/v1/check", tc.body, "test-key")
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}
