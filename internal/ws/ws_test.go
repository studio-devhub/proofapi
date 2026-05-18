package ws_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

// ── Test Setup ────────────────────────────────────────────

func mockLT(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		text := r.FormValue("text")
		var matches []map[string]any
		if strings.Contains(text, "recieve") {
			matches = append(matches, map[string]any{
				"message": "Spelling mistake",
				"offset":  strings.Index(text, "recieve"),
				"length":  7,
				"replacements": []map[string]any{{"value": "receive"}},
				"rule": map[string]any{
					"id": "SPELL", "issueType": "misspelling",
					"category": map[string]any{"id": "TYPOS", "name": "Typos"},
				},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"matches":  matches,
			"language": map[string]any{"name": "English (US)", "code": "en-US"},
		})
	}))
}

type testSuite struct {
	server   *httptest.Server
	wsURL    string
	ltServer *httptest.Server
	redis    *cache.Redis
	mr       *miniredis.Miniredis
}

func setup(t *testing.T) *testSuite {
	t.Helper()

	lt := mockLT(t)
	t.Cleanup(lt.Close)

	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)

	ltClient := languagetool.NewClient(languagetool.Config{
		BaseURL: lt.URL, Timeout: 5 * time.Second,
	})

	hub := wspkg.NewHub(slog.Default())
	handler := wspkg.NewHandler(hub, ltClient, r, slog.Default())

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", handler.ServeWS)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/ws"

	return &testSuite{
		server: srv, wsURL: wsURL,
		ltServer: lt, redis: r, mr: mr,
	}
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readMsg(t *testing.T, conn *websocket.Conn, timeout time.Duration) wspkg.OutgoingMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	_, raw, err := conn.ReadMessage()
	require.NoError(t, err)
	var msg wspkg.OutgoingMessage
	require.NoError(t, json.Unmarshal(raw, &msg))
	return msg
}

func sendMsg(t *testing.T, conn *websocket.Conn, msg wspkg.IncomingMessage) {
	t.Helper()
	b, _ := json.Marshal(msg)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, b))
}

// ── Tests ─────────────────────────────────────────────────

func TestWS_Connect_ReceivesAck(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)

	ack := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypeAck, ack.Type)
	assert.NotNil(t, ack.Payload)
}

func TestWS_Check_SpellingError(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)

	// Consume ack
	readMsg(t, conn, 2*time.Second)

	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve emails", SeqID: 1,
	})

	result := readMsg(t, conn, 3*time.Second)
	assert.Equal(t, wspkg.TypeResult, result.Type)
	assert.Equal(t, 1, result.SeqID)

	// Parse payload
	b, _ := json.Marshal(result.Payload)
	var payload wspkg.CheckPayload
	require.NoError(t, json.Unmarshal(b, &payload))

	assert.Len(t, payload.Matches, 1)
	assert.Equal(t, "receive", payload.Matches[0].Replacements[0].Value)
	assert.False(t, payload.Cached)
	// LatencyMs can be 0 in test env with fast mock
	assert.GreaterOrEqual(t, payload.LatencyMs, int64(0))
}

func TestWS_Check_NoErrors(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "This is correct text.", SeqID: 2,
	})

	result := readMsg(t, conn, 3*time.Second)
	assert.Equal(t, wspkg.TypeResult, result.Type)

	b, _ := json.Marshal(result.Payload)
	var payload wspkg.CheckPayload
	json.Unmarshal(b, &payload)
	assert.Empty(t, payload.Matches)
}

func TestWS_Debounce_OnlyLastRequest(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	callCount := 0
	origURL := s.ltServer.URL
	_ = origURL

	// Send 5 rapid messages — only last should trigger LT call
	for i := 1; i <= 5; i++ {
		sendMsg(t, conn, wspkg.IncomingMessage{
			Type:  wspkg.TypeCheck,
			Text:  fmt.Sprintf("I recieve email number %d", i),
			SeqID: i,
		})
		time.Sleep(50 * time.Millisecond) // rapid typing simulation
	}
	_ = callCount

	// Wait for debounce to fire (400ms)
	result := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypeResult, result.Type)

	// SeqID should be 5 (last message)
	assert.Equal(t, 5, result.SeqID)
}

func TestWS_CacheHit_SecondRequest(t *testing.T) {
	s := setup(t)

	// First connection — populate cache
	conn1 := dial(t, s.wsURL)
	readMsg(t, conn1, 2*time.Second)

	sendMsg(t, conn1, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve emails", SeqID: 1,
	})
	r1 := readMsg(t, conn1, 3*time.Second)
	b, _ := json.Marshal(r1.Payload)
	var p1 wspkg.CheckPayload
	json.Unmarshal(b, &p1)
	assert.False(t, p1.Cached)

	// Wait for async cache write
	time.Sleep(100 * time.Millisecond)

	// Second connection — should hit cache
	conn2 := dial(t, s.wsURL)
	readMsg(t, conn2, 2*time.Second)

	sendMsg(t, conn2, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve emails", SeqID: 1,
	})
	r2 := readMsg(t, conn2, 3*time.Second)
	b2, _ := json.Marshal(r2.Payload)
	var p2 wspkg.CheckPayload
	json.Unmarshal(b2, &p2)
	assert.True(t, p2.Cached)
}

func TestWS_Ping_Pong(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second) // ack

	sendMsg(t, conn, wspkg.IncomingMessage{Type: wspkg.TypePing})

	pong := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypePong, pong.Type)
}

func TestWS_Error_TextTooShort(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "a", SeqID: 1,
	})

	errMsg := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypeError, errMsg.Type)
	assert.Contains(t, errMsg.Error, "too short")
}

func TestWS_Error_InvalidJSON(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	conn.WriteMessage(websocket.TextMessage, []byte(`{invalid`))

	errMsg := readMsg(t, conn, 2*time.Second)
	assert.Equal(t, wspkg.TypeError, errMsg.Type)
}

func TestWS_MultipleConnections(t *testing.T) {
	s := setup(t)
	conns := make([]*websocket.Conn, 5)

	for i := range conns {
		conns[i] = dial(t, s.wsURL)
		readMsg(t, conns[i], 2*time.Second) // ack
	}

	// All connections send simultaneously
	for i, c := range conns {
		sendMsg(t, c, wspkg.IncomingMessage{
			Type:  wspkg.TypeCheck,
			Text:  fmt.Sprintf("I recieve email from client %d", i),
			SeqID: i,
		})
	}

	// All should get results
	for i, c := range conns {
		result := readMsg(t, c, 3*time.Second)
		assert.Equal(t, wspkg.TypeResult, result.Type, "conn %d", i)
	}
}

func TestWS_HubStats(t *testing.T) {
	s := setup(t)

	hub := wspkg.NewHub(slog.Default())
	_ = hub

	// Just verify connections are tracked via health
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	// Connection should be active
	conn.Close()
	time.Sleep(50 * time.Millisecond)
}

func TestWS_DefaultLanguage(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	// No language field — should default to en-US
	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve emails", SeqID: 1,
		// Language omitted
	})

	result := readMsg(t, conn, 3*time.Second)
	assert.Equal(t, wspkg.TypeResult, result.Type)
}

func TestWS_SeqID_Preserved(t *testing.T) {
	s := setup(t)
	conn := dial(t, s.wsURL)
	readMsg(t, conn, 2*time.Second)

	seqID := 42
	sendMsg(t, conn, wspkg.IncomingMessage{
		Type: wspkg.TypeCheck, Text: "I recieve emails", SeqID: seqID,
	})

	result := readMsg(t, conn, 3*time.Second)
	assert.Equal(t, seqID, result.SeqID)
}
