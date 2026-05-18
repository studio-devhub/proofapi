package ws

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/languagetool"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (CLI, server-to-server)
		}
		allowed := os.Getenv("ALLOWED_ORIGINS")
		if allowed == "" {
			return true // no restriction configured — open (dev mode)
		}
		for _, o := range strings.Split(allowed, ",") {
			if strings.TrimSpace(o) == origin {
				return true
			}
		}
		return false
	},
	HandshakeTimeout: 10 * time.Second,
}

type Handler struct {
	hub    *Hub
	lt     *languagetool.Client
	redis  *cache.Redis
	logger *slog.Logger
}

func NewHandler(
	hub *Hub,
	lt *languagetool.Client,
	redis *cache.Redis,
	logger *slog.Logger,
) *Handler {
	return &Handler{hub: hub, lt: lt, redis: redis, logger: logger}
}

// ServeWS upgrades HTTP → WebSocket and runs the connection
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	raw, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("ws upgrade failed", "err", err)
		return
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())

	conn := NewConn(id, raw, h.lt, h.redis, h.logger)

	h.hub.Register(conn)
	defer h.hub.Unregister(id)

	conn.Run()
}

// Stats returns hub stats for health endpoint
func (h *Handler) Stats() map[string]int64 {
	return h.hub.Stats()
}
