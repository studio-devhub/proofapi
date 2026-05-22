package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/dictionary"
	"languagetool-backend/internal/languagetool"
)

var clientIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]{1,128}$`)


const (
	debounceMs     = 150 * time.Millisecond // optimized: 400ms → 150ms
	writeTimeout   = 10 * time.Second
	pongTimeout    = 60 * time.Second
	pingInterval   = 30 * time.Second
	maxMessageSize = 32 * 1024 // 32KB
	cacheTTL       = 30 * time.Minute
	cachePrefix    = "lt:ws"
)

type Conn struct {
	id      string
	apiKey  string
	conn    *websocket.Conn
	lt      *languagetool.Client
	redis   cache.CheckCache
	dictSvc *dictionary.Service
	logger  *slog.Logger

	send   chan OutgoingMessage
	ctx    context.Context
	cancel context.CancelFunc

	mu            sync.Mutex
	debounceTimer *time.Timer
	pendingMsg    *IncomingMessage
}

func NewConn(
	id string,
	apiKey string,
	conn *websocket.Conn,
	lt *languagetool.Client,
	redis cache.CheckCache,
	dictSvc *dictionary.Service,
	logger *slog.Logger,
) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		id:      id,
		apiKey:  apiKey,
		conn:    conn,
		lt:      lt,
		redis:   redis,
		dictSvc: dictSvc,
		logger:  logger,
		send:    make(chan OutgoingMessage, 64),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (c *Conn) Run() {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); c.writePump() }()
	go func() { defer wg.Done(); c.readPump() }()
	wg.Wait()
	c.cleanup()
}

// ── Read Pump ─────────────────────────────────────────────

func (c *Conn) readPump() {
	defer c.cancel()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongTimeout))
		return nil
	})

	// First message must be {"type":"auth","key":"<api-key>"}.
	// This avoids embedding the API key in the URL query string where it
	// would appear in nginx access logs and browser history.
	c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, raw, err := c.conn.ReadMessage()
	if err != nil {
		return
	}
	var authMsg AuthMessage
	if err := json.Unmarshal(raw, &authMsg); err != nil || authMsg.Type != TypeAuth {
		c.sendError("first message must be {\"type\":\"auth\",\"key\":\"<api-key>\"}", 0)
		return
	}
	if authMsg.Key != c.apiKey {
		c.sendError("unauthorized", 0)
		c.logger.Warn("ws auth failed", "conn", c.id)
		return
	}
	c.conn.SetReadDeadline(time.Now().Add(pongTimeout))

	// Auth passed — send ack now (not before, so failed auth never gets ack)
	c.safeSend(OutgoingMessage{
		Type: TypeAck,
		Payload: map[string]string{
			"connId": c.id,
			"status": "connected",
		},
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
			) {
				c.logger.Warn("ws read error", "conn", c.id, "err", err)
			}
			return
		}

		var msg IncomingMessage
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&msg); err != nil {
			c.sendError("invalid message: "+err.Error(), 0)
			continue
		}

		switch msg.Type {
		case TypePing:
			c.safeSend(OutgoingMessage{Type: TypePong})

		case TypeCheck:
			msg.Text = strings.TrimSpace(msg.Text)
			if len(msg.Text) < 2 {
				c.sendError("text too short", msg.SeqID)
				continue
			}
			if len(msg.Text) > 20000 {
				c.sendError("text too long (max 20000 chars)", msg.SeqID)
				continue
			}
			if msg.ClientID != "" && !clientIDPattern.MatchString(msg.ClientID) {
				c.sendError("invalid clientId format", msg.SeqID)
				continue
			}
			c.scheduleCheck(&msg)
		}
	}
}

// ── Write Pump ────────────────────────────────────────────

func (c *Conn) writePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteJSON(msg); err != nil {
				c.logger.Warn("ws write error", "conn", c.id, "err", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// ── Debounce ──────────────────────────────────────────────

func (c *Conn) scheduleCheck(msg *IncomingMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pendingMsg = msg

	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
	}

	c.debounceTimer = time.AfterFunc(debounceMs, func() {
		c.mu.Lock()
		pending := c.pendingMsg
		c.mu.Unlock()

		if pending != nil {
			c.doCheck(pending)
		}
	})
}

// ── Check ─────────────────────────────────────────────────

func (c *Conn) doCheck(msg *IncomingMessage) {
	start := time.Now()

	lang := msg.Language
	if lang == "" {
		lang = "en-US"
	}

	// Cache hit
	cacheKey := cache.BuildKey(cachePrefix, lang, msg.Text)
	var cached languagetool.CheckResponse
	hit, err := c.redis.Get(c.ctx, cacheKey, &cached)
	if err != nil {
		c.logger.Warn("redis get error", "err", err)
	}
	wordSet := c.getWordSet(msg.ClientID)

	if hit {
		c.safeSend(OutgoingMessage{
			Type:  TypeResult,
			SeqID: msg.SeqID,
			Payload: CheckPayload{
				Matches:   languagetool.FilterMatches(msg.Text, cached.Matches, wordSet),
				Language:  cached.Language,
				Cached:    true,
				LatencyMs: time.Since(start).Milliseconds(),
			},
		})
		return
	}

	// LT check
	result, err := c.lt.Check(c.ctx, languagetool.CheckRequest{
		Text:     msg.Text,
		Language: lang,
	})
	if err != nil {
		if c.ctx.Err() == nil {
			c.sendError("spell check failed", msg.SeqID)
		}
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c.redis.Set(ctx, cacheKey, result, cacheTTL)
	}()

	c.safeSend(OutgoingMessage{
		Type:  TypeResult,
		SeqID: msg.SeqID,
		Payload: CheckPayload{
			Matches:   languagetool.FilterMatches(msg.Text, result.Matches, wordSet),
			Language:  result.Language,
			Cached:    false,
			LatencyMs: time.Since(start).Milliseconds(),
		},
	})
}

// ── Helpers ───────────────────────────────────────────────

func (c *Conn) getWordSet(clientID string) map[string]struct{} {
	if c.dictSvc == nil || clientID == "" {
		return nil
	}
	return c.dictSvc.GetWordSet(c.ctx, clientID)
}

// safeSend sends a message without panicking if the channel is closed.
// Uses recover so that a debounce goroutine firing after cleanup() cannot crash the process.
func (c *Conn) safeSend(msg OutgoingMessage) {
	defer func() { recover() }() //nolint:errcheck
	select {
	case c.send <- msg:
	case <-c.ctx.Done():
	}
}

func (c *Conn) sendError(msg string, seqID int) {
	c.safeSend(OutgoingMessage{Type: TypeError, Error: msg, SeqID: seqID})
}

func (c *Conn) cleanup() {
	c.mu.Lock()
	if c.debounceTimer != nil {
		c.debounceTimer.Stop()
	}
	c.mu.Unlock()
	close(c.send)
	c.logger.Info("ws connection closed", "conn", c.id)
}
