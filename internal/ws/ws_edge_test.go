package ws_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func setupEdge(t *testing.T, delay time.Duration) *testSuite {
	t.Helper()

	ltSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		r.ParseForm()
		text := r.FormValue("text")
		var matches []map[string]any
		if strings.Contains(text, "recieve") {
			matches = append(matches, map[string]any{
				"message": "Spelling",
				"offset":  strings.Index(text, "recieve"), "length": 7,
				"replacements": []map[string]any{{"value": "receive"}},
				"rule": map[string]any{"id": "S1", "issueType": "misspelling",
					"category": map[string]any{"id": "T", "name": "Typos"}},
				"context": map[string]any{"text": text, "offset": 0, "length": 7},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  matches,
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
	t.Cleanup(ltSrv.Close)

	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)

	ltClient := languagetool.NewClient(languagetool.Config{
		BaseURL: ltSrv.URL, Timeout: 5 * time.Second,
	})
	hub := wspkg.NewHub(slog.Default())
	handler := wspkg.NewHandler(hub, ltClient, r, nil, slog.Default())

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", handler.ServeWS)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &testSuite{
		server:   srv,
		wsURL:    "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/ws",
		ltServer: ltSrv,
		redis:    r,
		mr:       mr,
	}
}

// ── Edge Cases ─────────────────────────────────────────────

func TestWS_TextTooLong(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	longText := strings.Repeat("a", 20001)
	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: longText, SeqID: 1,
	})

	errMsg := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypeError, errMsg.Type)
	assert.Contains(t, errMsg.Error, "too long")
}

func TestWS_EmptyText(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "", SeqID: 1,
	})

	errMsg := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypeError, errMsg.Type)
}

func TestWS_WhitespaceText(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "  ", SeqID: 1,
	})

	errMsg := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypeError, errMsg.Type)
}

func TestWS_UnknownMessageType(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	// Unknown type — should be silently ignored (no crash)
	b, _ := json.Marshal(wspkg.IncomingMessage{Type: "unknown_type", Text: "test", SeqID: 1})
	conn.WriteMessage(websocket.TextMessage, b)

	// Connection should still be alive — send ping
	sendMsg(t, conn, wspkg.IncomingMessage{Type: wspkg.TypePing})
	pong := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypePong, pong.Type)
}

func TestWS_RapidFireMessages(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	// Send 20 messages rapidly
	for i := 1; i <= 20; i++ {
		sendMsg(t, conn, wspkg.IncomingMessage{
			Type:  wspkg.TypeCheck,
			Text:  fmt.Sprintf("I recieve email %d from sender", i),
			SeqID: i,
		})
	}

	// Debounce — only one result expected
	result := readMsg(t, conn, 3*time.Second)
	assert.Equal(t, wspkg.TypeResult, result.Type)
	assert.Equal(t, 20, result.SeqID) // last message
}

func TestWS_DebounceResetOnNewMessage(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	// Send, wait 100ms (less than 150ms debounce), send again
	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve email", SeqID: 1,
	})
	time.Sleep(100 * time.Millisecond)
	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve emails now", SeqID: 2,
	})

	// Only second message should fire
	result := readMsg(t, conn, 3*time.Second)
	assert.Equal(t, 2, result.SeqID)
}

func TestWS_MultipleSequentialChecks(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	texts := []string{
		"I recieve first email",
		"She is definately sure",
		"The goverment decided",
	}

	for i, text := range texts {
		sendMsg(t, conn, wspkg.IncomingMessage{
			Type: wspkg.TypeCheck, Text: text, SeqID: i + 1,
		})
		// Wait for debounce + response
		result := readMsg(t, conn, 3*time.Second)
		assert.Equal(t, wspkg.TypeResult, result.Type)
		assert.Equal(t, i+1, result.SeqID)
	}
}

func TestWS_50ConcurrentConnections(t *testing.T) {
	s := setup(t)

	var wg sync.WaitGroup
	errors := make(chan error, 50)
	results := make(chan wspkg.OutgoingMessage, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			conn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
			if err != nil {
				errors <- err
				return
			}
			defer conn.Close()

			// Read ack
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, _, err = conn.ReadMessage()
			if err != nil {
				errors <- err
				return
			}

			// Send check
			msg, _ := json.Marshal(wspkg.IncomingMessage{
				Type: wspkg.TypeCheck,
				Text: fmt.Sprintf("I recieve email from client %d", i),
				SeqID: i,
			})
			conn.WriteMessage(websocket.TextMessage, msg)

			// Read result
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				errors <- err
				return
			}

			var result wspkg.OutgoingMessage
			json.Unmarshal(raw, &result)
			results <- result
		}(i)
	}

	wg.Wait()
	close(errors)
	close(results)

	errCount := 0
	for err := range errors {
		t.Logf("error: %v", err)
		errCount++
	}

	resultCount := 0
	for r := range results {
		assert.Equal(t, wspkg.TypeResult, r.Type)
		resultCount++
	}

	assert.Equal(t, 0, errCount)
	assert.Equal(t, 50, resultCount)
}

func TestWS_ConnectionClosedMidCheck(t *testing.T) {
	// LT server with delay
	s := setupEdge(t, 200*time.Millisecond)

	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve emails", SeqID: 1,
	})

	// Close immediately — server should handle gracefully (no panic)
	conn.Close()
	time.Sleep(300 * time.Millisecond)
}

func TestWS_ReconnectAfterDisconnect(t *testing.T) {
	s := setup(t)

	// First connection
	conn1 := dial(t, s.wsURL)
	ack1 := readMsg(t, conn1, 2*time.Second)
	assert.Equal(t, wspkg.TypeAck, ack1.Type)
	conn1.Close()

	time.Sleep(50 * time.Millisecond)

	// Second connection — fresh state
	conn2 := dial(t, s.wsURL)
	ack2 := readMsg(t, conn2, 2*time.Second)
	assert.Equal(t, wspkg.TypeAck, ack2.Type)

	// Should work normally
	sendMsg(t, conn2, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve emails", SeqID: 1,
	})
	result := readMsg(t, conn2, 3*time.Second)
	assert.Equal(t, wspkg.TypeResult, result.Type)
}

func TestWS_CacheHit_FasterLatency(t *testing.T) {
	// LT server with 100ms delay
	s := setupEdge(t, 100*time.Millisecond)

	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	text := "I recieve emails from server"

	// First request — cache miss, expect >=100ms
	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: text, SeqID: 1,
	})
	r1 := readMsg(t, conn, 3*time.Second)
	b1, _ := json.Marshal(r1.Payload)
	var p1 wspkg.CheckPayload
	json.Unmarshal(b1, &p1)
	assert.False(t, p1.Cached)
	assert.GreaterOrEqual(t, p1.LatencyMs, int64(100))

	time.Sleep(100 * time.Millisecond)

	// Second request — cache hit, expect much faster
	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: text, SeqID: 2,
	})
	r2 := readMsg(t, conn, 2*time.Second)
	b2, _ := json.Marshal(r2.Payload)
	var p2 wspkg.CheckPayload
	json.Unmarshal(b2, &p2)
	assert.True(t, p2.Cached)
	assert.Less(t, p2.LatencyMs, p1.LatencyMs) // cache must be faster
}

func TestWS_MaxMessageSize(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	// 32KB + 1 byte — should be rejected by server
	oversized := strings.Repeat("a", 32*1024+1)
	err := conn.WriteMessage(websocket.TextMessage, []byte(oversized))
	// Either write fails or server closes connection
	if err == nil {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, readErr := conn.ReadMessage()
		// Connection should be closed by server
		assert.Error(t, readErr)
	}
}

func TestWS_HubTracksConnections(t *testing.T) {
	hub := wspkg.NewHub(slog.Default())

	initial := hub.Stats()
	assert.Equal(t, int64(0), initial["active"])
	assert.Equal(t, int64(0), initial["total"])
}

func TestWS_SpecialCharsInText(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	specialTexts := []string{
		"Hello! How's it going? It's \"great\".",
		"Price: $100 & more <stuff>",
		"Email: user@example.com",
		"Code: x = y + z * 2",
	}

	for i, text := range specialTexts {
		sendMsg(t, conn, wspkg.IncomingMessage{
			Type: wspkg.TypeCheck, Text: text, SeqID: i + 1,
		})
		result := readMsg(t, conn, 3*time.Second)
		assert.Equal(t, wspkg.TypeResult, result.Type, "text: %s", text)
	}
}
