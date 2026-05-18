package languagetool_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/languagetool"
)

func mockLTServer(t *testing.T, statusCode int, response any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v2/check", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(response)
	}))
}

func newTestClient(serverURL string) *languagetool.Client {
	return languagetool.NewClient(languagetool.Config{
		BaseURL: serverURL,
		Timeout: 5 * time.Second,
	})
}

func TestClient_Check_Success(t *testing.T) {
	mockResponse := map[string]any{
		"matches": []map[string]any{
			{
				"message": "Did you mean 'is'?",
				"offset":  5,
				"length":  3,
				"replacements": []map[string]any{
					{"value": "is"},
				},
				"rule": map[string]any{
					"id":          "GRAMMAR_001",
					"description": "Grammar error",
					"issueType":   "grammar",
					"category":    map[string]any{"id": "GRAMMAR", "name": "Grammar"},
				},
				"context": map[string]any{
					"text": "This are a test", "offset": 5, "length": 3,
				},
			},
		},
		"language": map[string]any{"name": "English (US)", "code": "en-US"},
	}

	srv := mockLTServer(t, http.StatusOK, mockResponse)
	defer srv.Close()

	result, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "This are a test", Language: "en-US", Level: "default",
	})

	require.NoError(t, err)
	assert.Len(t, result.Matches, 1)
	assert.Equal(t, "Did you mean 'is'?", result.Matches[0].Message)
	assert.Equal(t, 5, result.Matches[0].Offset)
	assert.Equal(t, "grammar", result.Matches[0].Rule.IssueType)
	assert.False(t, result.Cached)
}

func TestClient_Check_NoMatches(t *testing.T) {
	srv := mockLTServer(t, http.StatusOK, map[string]any{
		"matches":  []any{},
		"language": map[string]any{"name": "English (US)", "code": "en-US"},
	})
	defer srv.Close()

	result, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "This is correct.", Language: "en-US",
	})
	require.NoError(t, err)
	assert.Empty(t, result.Matches)
}

func TestClient_Check_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Check(context.Background(), languagetool.CheckRequest{
		Text: "test", Language: "en-US",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestClient_Check_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	client := languagetool.NewClient(languagetool.Config{
		BaseURL: srv.URL,
		Timeout: 50 * time.Millisecond,
	})

	_, err := client.Check(context.Background(), languagetool.CheckRequest{
		Text: "test", Language: "en-US",
	})
	assert.Error(t, err)
}

func TestClient_Check_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := newTestClient(srv.URL).Check(ctx, languagetool.CheckRequest{
		Text: "test", Language: "en-US",
	})
	assert.Error(t, err)
}

func TestClient_Ping_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode([]any{})
	}))
	defer srv.Close()
	assert.True(t, newTestClient(srv.URL).Ping(context.Background()))
}

func TestClient_Ping_Fail(t *testing.T) {
	assert.False(t, newTestClient("http://localhost:19999").Ping(context.Background()))
}
