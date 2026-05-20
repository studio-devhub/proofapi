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
	"languagetool-backend/internal/dictionary"
	"languagetool-backend/internal/languagetool"
)

// ltResponseWithWord returns a fake LT response that flags the given word at offset 0.
func ltResponseWithWord(word string) map[string]any {
	return map[string]any{
		"matches": []map[string]any{
			{
				"message": "Unknown word",
				"offset":  0,
				"length":  len([]rune(word)),
				"replacements": []map[string]any{},
				"rule": map[string]any{
					"id": "MORFOLOGIK_RULE_EN_US", "issueType": "misspelling",
					"category": map[string]any{"id": "TYPOS", "name": "Possible Typo"},
				},
				"context": map[string]any{"text": word, "offset": 0, "length": len([]rune(word))},
			},
		},
		"language": map[string]any{"name": "English (US)", "code": "en-US"},
	}
}

func setupHandlerWithDict(t *testing.T, ltResponse any) (*languagetool.Handler, *cache.Redis, *dictionary.Service, *miniredis.Miniredis) {
	t.Helper()

	ltSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ltResponse)
	}))
	t.Cleanup(ltSrv.Close)

	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)

	// Use an in-memory store for the dictionary
	store := &inMemoryStore{words: make(map[string][]dictionary.Word)}
	dictCache := dictionary.NewDictCache(r)
	dictSvc := dictionary.NewService(store, dictCache, slog.Default())

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: ltSrv.URL, Timeout: 5 * time.Second,
	})
	handler := languagetool.NewHandler(client, r, dictSvc, slog.Default())
	return handler, r, dictSvc, mr
}

// inMemoryStore satisfies dictionary.Store without DynamoDB.
type inMemoryStore struct {
	words map[string][]dictionary.Word
}

func (s *inMemoryStore) AddWord(_ context.Context, clientID, word, language string) error {
	s.words[clientID] = append(s.words[clientID], dictionary.Word{Word: word, Language: language})
	return nil
}
func (s *inMemoryStore) RemoveWord(_ context.Context, clientID, word string) error {
	list := s.words[clientID]
	for i, w := range list {
		if w.Word == word {
			s.words[clientID] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return nil
}
func (s *inMemoryStore) ListWords(_ context.Context, clientID string) ([]dictionary.Word, error) {
	return s.words[clientID], nil
}
func (s *inMemoryStore) ClearAll(_ context.Context, clientID string) error {
	delete(s.words, clientID)
	return nil
}

func doCheckWithClient(t *testing.T, handler *languagetool.Handler, text, clientID string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"text": text, "language": "en-US"})
	req := httptest.NewRequest(http.MethodPost, "/v1/check", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if clientID != "" {
		req.Header.Set("X-Client-ID", clientID)
	}
	w := httptest.NewRecorder()
	handler.Check(w, req)
	return w
}

// ── Dictionary Filter Integration ────────────────────────

func TestHandler_Check_DictionaryFiltersMatch(t *testing.T) {
	handler, _, dictSvc, _ := setupHandlerWithDict(t, ltResponseWithWord("Tulvo"))

	// Add "Tulvo" to client-1's dictionary
	_, err := dictSvc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")
	require.NoError(t, err)

	// Check with clientId — "Tulvo" should be filtered out
	w := doCheckWithClient(t, handler, "Tulvo is great", "client-1")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp languagetool.CheckResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Matches, "match should be filtered by dictionary")
}

func TestHandler_Check_WithoutClientID_NoFiltering(t *testing.T) {
	handler, _, dictSvc, _ := setupHandlerWithDict(t, ltResponseWithWord("Tulvo"))

	// Add to dict but DON'T send X-Client-ID header
	dictSvc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")

	w := doCheckWithClient(t, handler, "Tulvo is great", "") // no clientId
	assert.Equal(t, http.StatusOK, w.Code)

	var resp languagetool.CheckResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Matches, 1, "match should NOT be filtered without clientId")
}

func TestHandler_Check_DifferentClients_Isolated(t *testing.T) {
	handler, _, dictSvc, _ := setupHandlerWithDict(t, ltResponseWithWord("Tulvo"))

	// Only client-1 has "Tulvo" in dictionary, not client-2
	dictSvc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")

	w1 := doCheckWithClient(t, handler, "Tulvo is great", "client-1")
	w2 := doCheckWithClient(t, handler, "Tulvo is great", "client-2")

	var resp1, resp2 languagetool.CheckResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	json.Unmarshal(w2.Body.Bytes(), &resp2)

	assert.Empty(t, resp1.Matches, "client-1 should have match filtered")
	assert.Len(t, resp2.Matches, 1, "client-2 should still see the match")
}

func TestHandler_Check_CachedResult_StillFiltered(t *testing.T) {
	handler, _, dictSvc, _ := setupHandlerWithDict(t, ltResponseWithWord("Tulvo"))

	const text = "Tulvo is great"

	// First request WITHOUT clientId — populates cache (unfiltered result)
	w1 := doCheckWithClient(t, handler, text, "")
	var resp1 languagetool.CheckResponse
	json.Unmarshal(w1.Body.Bytes(), &resp1)
	assert.Len(t, resp1.Matches, 1)

	// Wait for async cache write
	time.Sleep(100 * time.Millisecond)

	// Add word to dictionary
	dictSvc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")

	// Second request WITH clientId — should hit cache but STILL filter
	w2 := doCheckWithClient(t, handler, text, "client-1")
	var resp2 languagetool.CheckResponse
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	assert.True(t, resp2.Cached, "should be from cache")
	assert.Empty(t, resp2.Matches, "cached result should still be filtered by dictionary")
}

func TestHandler_Check_ClientID_InRequestBody(t *testing.T) {
	handler, _, dictSvc, _ := setupHandlerWithDict(t, ltResponseWithWord("Tulvo"))

	dictSvc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")

	// Pass clientId in body instead of header
	body, _ := json.Marshal(map[string]any{
		"text":     "Tulvo is great",
		"language": "en-US",
		"clientId": "client-1",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/check", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.Check(w, req)

	var resp languagetool.CheckResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Empty(t, resp.Matches, "clientId from body should also trigger filtering")
}
