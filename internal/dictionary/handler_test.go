package dictionary_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/alicebob/miniredis/v2"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/dictionary"
)

func setupHTTPHandler(t *testing.T) (*dictionary.HTTPHandler, *mockStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)

	store := newMockStore()
	dictCache := dictionary.NewDictCache(r)
	svc := dictionary.NewService(store, dictCache, slog.Default())
	return dictionary.NewHTTPHandler(svc, slog.Default()), store
}

func doRequest(t *testing.T, method, path, clientID string, body any, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	if clientID != "" {
		req.Header.Set("X-Client-ID", clientID)
	}
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// ── AddWord ───────────────────────────────────────────────

func TestHTTPHandler_AddWord_Success(t *testing.T) {
	h, store := setupHTTPHandler(t)
	w := doRequest(t, http.MethodPost, "/v1/dictionary/words", "client-1",
		map[string]any{"word": "Kubernetes", "language": "en-US"}, h.AddWord)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp dictionary.Word
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Kubernetes", resp.Word)
	assert.Len(t, store.words["client-1"], 1)
}

func TestHTTPHandler_AddWord_EmptyWord(t *testing.T) {
	h, _ := setupHTTPHandler(t)
	w := doRequest(t, http.MethodPost, "/v1/dictionary/words", "client-1",
		map[string]any{"word": ""}, h.AddWord)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHTTPHandler_AddWord_WordWithSpaces(t *testing.T) {
	h, _ := setupHTTPHandler(t)
	w := doRequest(t, http.MethodPost, "/v1/dictionary/words", "client-1",
		map[string]any{"word": "hello world"}, h.AddWord)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHTTPHandler_AddWord_InvalidJSON(t *testing.T) {
	h, _ := setupHTTPHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/dictionary/words", bytes.NewBufferString(`{invalid`))
	req.Header.Set("X-Client-ID", "client-1")
	w := httptest.NewRecorder()
	h.AddWord(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ── ListWords ─────────────────────────────────────────────

func TestHTTPHandler_ListWords_Empty(t *testing.T) {
	h, _ := setupHTTPHandler(t)
	w := doRequest(t, http.MethodGet, "/v1/dictionary/words", "client-1", nil, h.ListWords)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp dictionary.ListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "client-1", resp.ClientID)
	assert.Empty(t, resp.Words)
	assert.Equal(t, 0, resp.Count)
}

func TestHTTPHandler_ListWords_WithWords(t *testing.T) {
	h, store := setupHTTPHandler(t)
	store.words["client-1"] = []dictionary.Word{
		{Word: "Tulvo", Language: "en-US"},
		{Word: "Kubernetes", Language: "en-US"},
	}

	w := doRequest(t, http.MethodGet, "/v1/dictionary/words", "client-1", nil, h.ListWords)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp dictionary.ListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Count)
}

// ── RemoveWord ────────────────────────────────────────────

func TestHTTPHandler_RemoveWord_Success(t *testing.T) {
	h, store := setupHTTPHandler(t)
	store.words["client-1"] = []dictionary.Word{{Word: "Tulvo"}}

	// Use chi router to inject URL param
	r := chi.NewRouter()
	r.Delete("/v1/dictionary/words/{word}", h.RemoveWord)

	req := httptest.NewRequest(http.MethodDelete, "/v1/dictionary/words/Tulvo", nil)
	req.Header.Set("X-Client-ID", "client-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Tulvo", resp["removed"])
	assert.Empty(t, store.words["client-1"])
}

func TestHTTPHandler_RemoveWord_NonExistent_Returns200(t *testing.T) {
	h, _ := setupHTTPHandler(t)

	r := chi.NewRouter()
	r.Delete("/v1/dictionary/words/{word}", h.RemoveWord)

	req := httptest.NewRequest(http.MethodDelete, "/v1/dictionary/words/notexist", nil)
	req.Header.Set("X-Client-ID", "client-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ── ClearAll ──────────────────────────────────────────────

func TestHTTPHandler_ClearAll(t *testing.T) {
	h, store := setupHTTPHandler(t)
	store.words["client-1"] = []dictionary.Word{
		{Word: "Tulvo"}, {Word: "Kubernetes"},
	}

	w := doRequest(t, http.MethodDelete, "/v1/dictionary", "client-1", nil, h.ClearAll)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["cleared"])
	assert.Empty(t, store.words["client-1"])
}
