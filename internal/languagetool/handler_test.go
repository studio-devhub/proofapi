package languagetool_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/languagetool"
)

type handlerSuite struct {
	handler  *languagetool.Handler
	redis    *cache.Redis
	mr       *miniredis.Miniredis
	ltServer *httptest.Server
}

func setupHandler(t *testing.T, ltResponse any, ltStatus int) *handlerSuite {
	t.Helper()

	ltSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ltStatus)
		json.NewEncoder(w).Encode(ltResponse)
	}))
	t.Cleanup(ltSrv.Close)

	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: ltSrv.URL, Timeout: 5 * time.Second,
	})

	handler := languagetool.NewHandler(client, r, slog.Default())
	return &handlerSuite{handler: handler, redis: r, mr: mr, ltServer: ltSrv}
}

func doPost(t *testing.T, fn http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/check", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	fn(w, req)
	return w
}

func ltOKResponse() map[string]any {
	return map[string]any{
		"matches": []map[string]any{
			{
				"message": "Grammar error",
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
	}
}

// ── Check ─────────────────────────────────────────────────

func TestHandler_Check_Success(t *testing.T) {
	s := setupHandler(t, ltOKResponse(), http.StatusOK)

	w := doPost(t, s.handler.Check, map[string]any{
		"text": "This are a test", "language": "en-US",
	})

	assert.Equal(t, http.StatusOK, w.Code)
	var resp languagetool.CheckResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Matches, 1)
	assert.False(t, resp.Cached)
}

func TestHandler_Check_CacheHit(t *testing.T) {
	callCount := 0
	ltSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  []any{},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	defer ltSrv.Close()

	mr := miniredis.RunT(t)
	r, _ := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	client := languagetool.NewClient(languagetool.Config{BaseURL: ltSrv.URL, Timeout: 5 * time.Second})
	handler := languagetool.NewHandler(client, r, slog.Default())

	body := map[string]any{"text": "Hello world", "language": "en-US"}

	// First — cache miss
	w1 := doPost(t, handler.Check, body)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Wait for async goroutine to write cache
	time.Sleep(100 * time.Millisecond)

	// Second — cache hit
	w2 := doPost(t, handler.Check, body)
	assert.Equal(t, http.StatusOK, w2.Code)

	var resp languagetool.CheckResponse
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.True(t, resp.Cached)
	assert.Equal(t, 1, callCount) // LT called only once
}

func TestHandler_Check_InvalidJSON(t *testing.T) {
	s := setupHandler(t, nil, http.StatusOK)

	req := httptest.NewRequest(http.MethodPost, "/v1/check", bytes.NewBufferString(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handler.Check(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Check_TextTooShort(t *testing.T) {
	s := setupHandler(t, nil, http.StatusOK)
	w := doPost(t, s.handler.Check, map[string]any{"text": "a"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Check_TextTooLong(t *testing.T) {
	s := setupHandler(t, nil, http.StatusOK)
	long := make([]byte, 20001)
	for i := range long {
		long[i] = 'a'
	}
	w := doPost(t, s.handler.Check, map[string]any{"text": string(long)})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Check_EmptyText(t *testing.T) {
	s := setupHandler(t, nil, http.StatusOK)
	w := doPost(t, s.handler.Check, map[string]any{"text": ""})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Check_WhitespaceOnly(t *testing.T) {
	s := setupHandler(t, nil, http.StatusOK)
	w := doPost(t, s.handler.Check, map[string]any{"text": "   "})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Check_LTUnavailable(t *testing.T) {
	mr := miniredis.RunT(t)
	r, _ := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	client := languagetool.NewClient(languagetool.Config{
		BaseURL: "http://localhost:19998",
		Timeout: 100 * time.Millisecond,
	})
	handler := languagetool.NewHandler(client, r, slog.Default())

	w := doPost(t, handler.Check, map[string]any{"text": "Hello world", "language": "en-US"})
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandler_Check_DefaultLanguage(t *testing.T) {
	receivedLang := ""
	ltSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedLang = r.FormValue("language")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  []any{},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	defer ltSrv.Close()

	mr := miniredis.RunT(t)
	r, _ := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	client := languagetool.NewClient(languagetool.Config{BaseURL: ltSrv.URL, Timeout: 5 * time.Second})
	handler := languagetool.NewHandler(client, r, slog.Default())

	doPost(t, handler.Check, map[string]any{"text": "Hello world"})
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, "en-US", receivedLang)
}

// ── Cache Clear ───────────────────────────────────────────

func TestHandler_ClearCache(t *testing.T) {
	s := setupHandler(t, ltOKResponse(), http.StatusOK)
	ctx := context.Background()

	s.redis.Set(ctx, "lt:check:aaa", "val1", time.Minute)
	s.redis.Set(ctx, "lt:check:bbb", "val2", time.Minute)
	s.redis.Set(ctx, "other:key", "val3", time.Minute)

	req := httptest.NewRequest(http.MethodDelete, "/v1/cache", nil)
	w := httptest.NewRecorder()
	s.handler.ClearCache(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, float64(2), resp["deleted"])

	// other:key must still exist
	var val string
	hit, _ := s.redis.Get(ctx, "other:key", &val)
	assert.True(t, hit)
}

func TestHandler_ClearCache_Empty(t *testing.T) {
	s := setupHandler(t, nil, http.StatusOK)

	req := httptest.NewRequest(http.MethodDelete, "/v1/cache", nil)
	w := httptest.NewRecorder()
	s.handler.ClearCache(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, float64(0), resp["deleted"])
}
