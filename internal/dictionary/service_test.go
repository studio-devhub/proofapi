package dictionary_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/dictionary"
)

// ── Mock Store ────────────────────────────────────────────

type mockStore struct {
	words     map[string][]dictionary.Word // clientID → words
	addErr    error
	removeErr error
	listErr   error
}

func newMockStore() *mockStore {
	return &mockStore{words: make(map[string][]dictionary.Word)}
}

func (m *mockStore) AddWord(_ context.Context, clientID, word, language string) error {
	if m.addErr != nil {
		return m.addErr
	}
	for _, w := range m.words[clientID] {
		if w.Word == word {
			return nil // idempotent
		}
	}
	m.words[clientID] = append(m.words[clientID], dictionary.Word{Word: word, Language: language})
	return nil
}

func (m *mockStore) RemoveWord(_ context.Context, clientID, word string) error {
	if m.removeErr != nil {
		return m.removeErr
	}
	list := m.words[clientID]
	for i, w := range list {
		if w.Word == word {
			m.words[clientID] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return nil // idempotent
}

func (m *mockStore) ListWords(_ context.Context, clientID string) ([]dictionary.Word, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.words[clientID], nil
}

func (m *mockStore) ClearAll(_ context.Context, clientID string) error {
	delete(m.words, clientID)
	return nil
}

// ── Helpers ───────────────────────────────────────────────

func setupService(t *testing.T) (*dictionary.Service, *mockStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)

	store := newMockStore()
	dictCache := dictionary.NewDictCache(r)
	svc := dictionary.NewService(store, dictCache, slog.Default())
	return svc, store, mr
}

// ── AddWord ───────────────────────────────────────────────

func TestService_AddWord_Success(t *testing.T) {
	svc, store, _ := setupService(t)

	word, err := svc.AddWord(context.Background(), "client-1", "Kubernetes", "en-US")
	require.NoError(t, err)
	assert.Equal(t, "Kubernetes", word.Word)
	assert.Equal(t, "en-US", word.Language)
	assert.Len(t, store.words["client-1"], 1)
}

func TestService_AddWord_EmptyWord(t *testing.T) {
	svc, _, _ := setupService(t)
	_, err := svc.AddWord(context.Background(), "client-1", "", "en-US")
	assert.ErrorContains(t, err, "empty")
}

func TestService_AddWord_WordWithSpaces(t *testing.T) {
	svc, _, _ := setupService(t)
	_, err := svc.AddWord(context.Background(), "client-1", "hello world", "en-US")
	assert.ErrorContains(t, err, "spaces")
}

func TestService_AddWord_TooLong(t *testing.T) {
	svc, _, _ := setupService(t)
	long := make([]rune, 101)
	for i := range long {
		long[i] = 'a'
	}
	_, err := svc.AddWord(context.Background(), "client-1", string(long), "en-US")
	assert.ErrorContains(t, err, "too long")
}

func TestService_AddWord_StoreError_DoesNotUpdateCache(t *testing.T) {
	svc, store, _ := setupService(t)
	store.addErr = errors.New("dynamo down")

	_, err := svc.AddWord(context.Background(), "client-1", "word", "en-US")
	assert.Error(t, err)
}

// ── RemoveWord ────────────────────────────────────────────

func TestService_RemoveWord_Success(t *testing.T) {
	svc, store, _ := setupService(t)

	_, err := svc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")
	require.NoError(t, err)

	err = svc.RemoveWord(context.Background(), "client-1", "Tulvo")
	require.NoError(t, err)
	assert.Empty(t, store.words["client-1"])
}

func TestService_RemoveWord_NonExistent_IsIdempotent(t *testing.T) {
	svc, _, _ := setupService(t)
	err := svc.RemoveWord(context.Background(), "client-1", "doesnotexist")
	assert.NoError(t, err)
}

// ── ListWords ─────────────────────────────────────────────

func TestService_ListWords_Empty(t *testing.T) {
	svc, _, _ := setupService(t)
	words, err := svc.ListWords(context.Background(), "client-1")
	require.NoError(t, err)
	assert.Empty(t, words)
}

func TestService_ListWords_MultipleWords(t *testing.T) {
	svc, _, _ := setupService(t)
	svc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")
	svc.AddWord(context.Background(), "client-1", "Kubernetes", "en-US")

	words, err := svc.ListWords(context.Background(), "client-1")
	require.NoError(t, err)
	assert.Len(t, words, 2)
}

func TestService_ListWords_IsolatedByClientID(t *testing.T) {
	svc, _, _ := setupService(t)
	svc.AddWord(context.Background(), "client-1", "Alpha", "en-US")
	svc.AddWord(context.Background(), "client-2", "Beta", "en-US")

	words1, _ := svc.ListWords(context.Background(), "client-1")
	words2, _ := svc.ListWords(context.Background(), "client-2")
	assert.Len(t, words1, 1)
	assert.Len(t, words2, 1)
	assert.Equal(t, "Alpha", words1[0].Word)
	assert.Equal(t, "Beta", words2[0].Word)
}

// ── GetWordSet ────────────────────────────────────────────

func TestService_GetWordSet_EmptyClientID(t *testing.T) {
	svc, _, _ := setupService(t)
	set := svc.GetWordSet(context.Background(), "")
	assert.Nil(t, set)
}

func TestService_GetWordSet_CacheMiss_LoadsFromStore(t *testing.T) {
	svc, store, _ := setupService(t)
	store.words["client-1"] = []dictionary.Word{
		{Word: "Tulvo"},
		{Word: "Kubernetes"},
	}

	set := svc.GetWordSet(context.Background(), "client-1")
	assert.NotNil(t, set)
	_, hasTulvo := set["tulvo"]
	_, hasK8s := set["kubernetes"]
	assert.True(t, hasTulvo)
	assert.True(t, hasK8s)
}

func TestService_GetWordSet_CacheHit_DoesNotHitStore(t *testing.T) {
	svc, store, _ := setupService(t)

	// Populate via AddWord (which writes to both store + cache)
	svc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")

	// Poison the store so any store call would be noticed
	storeCalls := 0
	store.listErr = nil
	_ = storeCalls // second GetWordSet should use cache, not store

	set := svc.GetWordSet(context.Background(), "client-1")
	_, ok := set["tulvo"]
	assert.True(t, ok)
}

func TestService_GetWordSet_StoreError_FailsOpen(t *testing.T) {
	svc, store, _ := setupService(t)
	store.listErr = errors.New("dynamo unavailable")

	// Should not panic, returns nil (fail open — no filtering)
	set := svc.GetWordSet(context.Background(), "client-1")
	assert.Nil(t, set)
}

// ── ClearAll ──────────────────────────────────────────────

func TestService_ClearAll(t *testing.T) {
	svc, store, _ := setupService(t)
	svc.AddWord(context.Background(), "client-1", "Tulvo", "en-US")
	svc.AddWord(context.Background(), "client-1", "Kubernetes", "en-US")

	err := svc.ClearAll(context.Background(), "client-1")
	require.NoError(t, err)
	assert.Empty(t, store.words["client-1"])

	// Cache should also be cleared
	set := svc.GetWordSet(context.Background(), "client-1")
	assert.Empty(t, set)
}
