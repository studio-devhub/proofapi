package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/languagetool"
)

const (
	debounceMs     = 150 * time.Millisecond // optimized: 400ms → 150ms
	writeTimeout   = 10 * time.Second
	pongTimeout    = 60 * time.Second
	pingInterval   = 30 * time.Second
	maxMessageSize = 32 * 1024 // 32KB
	cacheTTL       = 5 * time.Minute
	cachePrefix    = "lt:ws"
)

type Conn struct {
	id     string
	conn   *websocket.Conn
	lt     *languagetool.Client
	redis  *cache.Redis
	logger *slog.Logger

	send   chan OutgoingMessage
	ctx    context.Context
	cancel context.CancelFunc

	mu            sync.Mutex
	debounceTimer *time.Timer
	pendingMsg    *IncomingMessage
}

func NewConn(
	id string,
	conn *websocket.Conn,
	lt *languagetool.Client,
	redis *cache.Redis,
	logger *slog.Logger,
) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		id:     id,
		conn:   conn,
		lt:     lt,
		redis:  redis,
		logger: logger,
		send:   make(chan OutgoingMessage, 64),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (c *Conn) Run() {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() { defer wg.Done(); c.writePump() }()
	go func() { defer wg.Done(); c.readPump() }()

	c.send <- OutgoingMessage{
		Type: TypeAck,
		Payload: map[string]string{
			"connId": c.id,
			"status": "connected",
		},
	}

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
		if err := json.Unmarshal(raw, &msg); err != nil {
			c.sendError("invalid message format", 0)
			continue
		}

		switch msg.Type {
		case TypePing:
			c.send <- OutgoingMessage{Type: TypePong}

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
	level := msg.Level
	if level == "" {
		level = "picky"
	}
	enabledCategories := msg.EnabledCategories
	if enabledCategories == "" {
		enabledCategories = "GRAMMAR,SPELLING,STYLE,PUNCTUATION,TYPOGRAPHY,CASING,CONFUSED_WORDS,REDUNDANCY,COMPOUNDING,MISC"
	}

	// Cache hit
	cacheKey := cache.BuildKey(cachePrefix, lang, level, enabledCategories, msg.MotherTongue, msg.Text)
	var cached languagetool.CheckResponse
	hit, err := c.redis.Get(c.ctx, cacheKey, &cached)
	if err != nil {
		c.logger.Warn("redis get error", "err", err)
	}
	if hit {
		c.send <- OutgoingMessage{
			Type:  TypeResult,
			SeqID: msg.SeqID,
			Payload: CheckPayload{
				Matches:   cached.Matches,
				Language:  cached.Language,
				Cached:    true,
				LatencyMs: time.Since(start).Milliseconds(),
			},
		}
		return
	}

	// LT check
	result, err := c.lt.Check(c.ctx, languagetool.CheckRequest{
		Text:               msg.Text,
		Language:           lang,
		Level:              level,
		MotherTongue:       msg.MotherTongue,
		EnabledCategories:  enabledCategories,
		DisabledCategories: msg.DisabledCategories,
		EnabledRules:       msg.EnabledRules,
		DisabledRules:      msg.DisabledRules,
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

	c.send <- OutgoingMessage{
		Type:  TypeResult,
		SeqID: msg.SeqID,
		Payload: CheckPayload{
			Matches:   result.Matches,
			Language:  result.Language,
			Cached:    false,
			LatencyMs: time.Since(start).Milliseconds(),
		},
	}
}

// ── Helpers ───────────────────────────────────────────────

func (c *Conn) sendError(msg string, seqID int) {
	select {
	case c.send <- OutgoingMessage{Type: TypeError, Error: msg, SeqID: seqID}:
	default:
	}
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
