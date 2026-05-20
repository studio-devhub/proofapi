package ws

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"languagetool-backend/internal/cache"
	"languagetool-backend/internal/dictionary"
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
	hub     *Hub
	apiKey  string
	lt      *languagetool.Client
	redis   *cache.Redis
	dictSvc *dictionary.Service
	logger  *slog.Logger
}

func NewHandler(
	hub *Hub,
	apiKey string,
	lt *languagetool.Client,
	redis *cache.Redis,
	dictSvc *dictionary.Service,
	logger *slog.Logger,
) *Handler {
	return &Handler{hub: hub, apiKey: apiKey, lt: lt, redis: redis, dictSvc: dictSvc, logger: logger}
}

// ServeWS upgrades HTTP → WebSocket and runs the connection.
// Auth happens inside the connection via first-message auth (TypeAuth),
// not at the HTTP middleware level — this keeps the API key out of URLs/logs.
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	raw, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn("ws upgrade failed", "err", err)
		return
	}

	id := newConnID()
	conn := NewConn(id, h.apiKey, raw, h.lt, h.redis, h.dictSvc, h.logger)

	h.hub.Register(conn)
	defer h.hub.Unregister(id)

	conn.Run()
}

// Stats returns hub stats for health endpoint
func (h *Handler) Stats() map[string]int64 {
	return h.hub.Stats()
}

func newConnID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely — fall back to timestamp-based ID
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}
