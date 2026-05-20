package ws_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/languagetool"
	wspkg "languagetool-backend/internal/ws"
)

func newLTClient(t *testing.T, url string) *languagetool.Client {
	t.Helper()
	return languagetool.NewClient(languagetool.Config{BaseURL: url, Timeout: 5 * time.Second})
}

// helper: spin up a WS test server with a given ALLOWED_ORIGINS env value
func setupCORSServer(t *testing.T, allowedOrigins string) *httptest.Server {
	t.Helper()
	if allowedOrigins != "" {
		t.Setenv("ALLOWED_ORIGINS", allowedOrigins)
	} else {
		t.Setenv("ALLOWED_ORIGINS", "")
	}

	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)

	ltSrv := mockLT(t)
	lt := newLTClient(t, ltSrv.URL)

	hub := wspkg.NewHub(slog.Default())
	handler := wspkg.NewHandler(hub, lt, r, nil, slog.Default())

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", handler.ServeWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestWS_CORS_AllowsWhenNoRestriction(t *testing.T) {
	srv := setupCORSServer(t, "") // empty = allow all

	wsURL := "ws" + srv.URL[4:] + "/v1/ws"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, http.Header{
		"Origin": []string{"https://any-origin.com"},
	})

	require.NoError(t, err, "connection should succeed when ALLOWED_ORIGINS is empty")
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	conn.Close()
}

func TestWS_CORS_AllowsMatchingOrigin(t *testing.T) {
	srv := setupCORSServer(t, "https://app.example.com,https://admin.example.com")

	wsURL := "ws" + srv.URL[4:] + "/v1/ws"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, http.Header{
		"Origin": []string{"https://app.example.com"},
	})

	require.NoError(t, err, "matching origin should be allowed")
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	conn.Close()
}

func TestWS_CORS_AllowsSecondOriginInList(t *testing.T) {
	srv := setupCORSServer(t, "https://app.example.com,https://admin.example.com")

	wsURL := "ws" + srv.URL[4:] + "/v1/ws"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, http.Header{
		"Origin": []string{"https://admin.example.com"},
	})

	require.NoError(t, err, "second origin in list should be allowed")
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	conn.Close()
}

func TestWS_CORS_BlocksUnknownOrigin(t *testing.T) {
	srv := setupCORSServer(t, "https://app.example.com")

	wsURL := "ws" + srv.URL[4:] + "/v1/ws"
	dialer := websocket.Dialer{}
	_, resp, err := dialer.Dial(wsURL, http.Header{
		"Origin": []string{"https://evil.com"},
	})

	assert.Error(t, err, "unknown origin should be blocked")
	if resp != nil {
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	}
}

func TestWS_CORS_AllowsNonBrowserClient(t *testing.T) {
	// No Origin header = server-to-server / CLI client — always allowed
	srv := setupCORSServer(t, "https://app.example.com")

	wsURL := "ws" + srv.URL[4:] + "/v1/ws"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, nil) // no Origin header

	require.NoError(t, err, "non-browser client without Origin header should always connect")
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	conn.Close()
}
