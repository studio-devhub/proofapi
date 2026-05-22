package languagetool_test

import (
	"sync"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/languagetool"
)

// ── Client Edge Cases ─────────────────────────────────────

func TestClient_Check_MultipleMatches(t *testing.T) {
	srv := mockLTServer(t, http.StatusOK, map[string]any{
		"matches": []map[string]any{
			{
				"message": "Spelling error",
				"offset": 2, "length": 7,
				"replacements": []map[string]any{{"value": "receive"}},
				"rule": map[string]any{"id": "S1", "issueType": "misspelling",
					"category": map[string]any{"id": "T", "name": "Typos"}},
				"context": map[string]any{"text": "I recieve wierd msgs", "offset": 2, "length": 7},
			},
			{
				"message": "Spelling error",
				"offset": 10, "length": 5,
				"replacements": []map[string]any{{"value": "weird"}},
				"rule": map[string]any{"id": "S2", "issueType": "misspelling",
					"category": map[string]any{"id": "T", "name": "Typos"}},
				"context": map[string]any{"text": "I recieve wierd msgs", "offset": 10, "length": 5},
			},
		},
		"language": map[string]any{"name": "English (US)", "code": "en-US"},
	})
	defer srv.Close()

	result, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "I recieve wierd msgs", Language: "en-US",
	})
	require.NoError(t, err)
	assert.Len(t, result.Matches, 2)
	assert.Equal(t, "receive", result.Matches[0].Replacements[0].Value)
	assert.Equal(t, "weird", result.Matches[1].Replacements[0].Value)
}

func TestClient_Check_EmptyReplacements(t *testing.T) {
	srv := mockLTServer(t, http.StatusOK, map[string]any{
		"matches": []map[string]any{
			{
				"message":      "Unknown word",
				"offset":       0, "length": 4,
				"replacements": []map[string]any{}, // no suggestions
				"rule": map[string]any{"id": "S1", "issueType": "misspelling",
					"category": map[string]any{"id": "T", "name": "Typos"}},
				"context": map[string]any{"text": "xyzw is unknown", "offset": 0, "length": 4},
			},
		},
		"language": map[string]any{"name": "English (US)", "code": "en-US"},
	})
	defer srv.Close()

	result, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "xyzw is unknown", Language: "en-US",
	})
	require.NoError(t, err)
	assert.Len(t, result.Matches, 1)
	assert.Empty(t, result.Matches[0].Replacements)
}

func TestClient_Check_LargeText(t *testing.T) {
	srv := mockLTServer(t, http.StatusOK, map[string]any{
		"matches":  []any{},
		"language": map[string]any{"name": "English (US)", "code": "en-US"},
	})
	defer srv.Close()

	largeText := strings.Repeat("This is a sentence. ", 1000) // ~20000 chars
	result, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: largeText, Language: "en-US",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Matches)
}

func TestClient_Check_SpecialCharsInText(t *testing.T) {
	received := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		received = r.FormValue("text")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  []any{},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	defer srv.Close()

	text := "Hello! How are you? It's a \"test\" & more <stuff>."
	_, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: text, Language: "en-US",
	})
	require.NoError(t, err)
	assert.Equal(t, text, received)
}

func TestClient_Check_DifferentLanguages(t *testing.T) {
	receivedLang := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedLang = r.FormValue("language")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  []any{},
			"language": map[string]any{"name": "German", "code": "de-DE"},
		})
	}))
	defer srv.Close()

	langs := []string{"en-US", "en-GB", "de-DE", "fr-FR", "es-ES"}
	for _, lang := range langs {
		_, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
			Text: "Test text", Language: lang,
		})
		require.NoError(t, err)
		assert.Equal(t, lang, receivedLang, "language mismatch for %s", lang)
	}
}

func TestClient_Check_LevelIsHardcoded(t *testing.T) {
	receivedLevel := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		receivedLevel = r.FormValue("level")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  []any{},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "Test", Language: "en-US",
	})
	require.NoError(t, err)
	assert.Equal(t, "default", receivedLevel)
}

func TestClient_Check_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "test", Language: "en-US",
	})
	assert.Error(t, err)
}

func TestClient_Check_BadGateway(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "test", Language: "en-US",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestClient_Check_SlowResponse_WithinTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  []any{},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL,
		Timeout: 500 * time.Millisecond,
	})

	result, err := client.Check(context.Background(), languagetool.CheckRequest{
		Text: "test", Language: "en-US",
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestClient_Check_CheckedAtTimestamp(t *testing.T) {
	srv := mockLTServer(t, http.StatusOK, map[string]any{
		"matches":  []any{},
		"language": map[string]any{"name": "English (US)", "code": "en-US"},
	})
	defer srv.Close()

	before := time.Now().UTC().Add(-time.Second) // 1s tolerance
	result, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "test", Language: "en-US",
	})
	after := time.Now().UTC().Add(time.Second)

	require.NoError(t, err)
	checkedAt, err := time.Parse(time.RFC3339, result.CheckedAt)
	require.NoError(t, err)
	assert.True(t, !checkedAt.Before(before), "checkedAt should be after before")
	assert.True(t, !checkedAt.After(after), "checkedAt should be before after")
}

func TestClient_Languages_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v2/languages", r.URL.Path)
		json.NewEncoder(w).Encode([]map[string]any{
			{"name": "English (US)", "code": "en-US"},
			{"name": "German",       "code": "de-DE"},
		})
	}))
	defer srv.Close()

	langs, err := newTestClient(srv.URL).Languages(context.Background())
	require.NoError(t, err)
	assert.Len(t, langs, 2)
}

func TestClient_Languages_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Languages(context.Background())
	assert.Error(t, err)
}

func TestClient_Ping_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	assert.False(t, newTestClient(srv.URL).Ping(context.Background()))
}

func TestClient_Concurrency(t *testing.T) {
	callCount := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  []any{},
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	var wg sync.WaitGroup
	errors := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.Check(context.Background(), languagetool.CheckRequest{
				Text: "concurrent test", Language: "en-US",
			})
			if err != nil {
				errors <- err
			}
		}()
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		assert.NoError(t, err)
	}
	assert.Equal(t, 50, callCount)
}
