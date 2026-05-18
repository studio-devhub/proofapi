package ws

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// Hub tracks all active WebSocket connections
type Hub struct {
	mu    sync.RWMutex
	conns map[string]*Conn

	totalConnections atomic.Int64
	activeConns      atomic.Int64

	logger *slog.Logger
}

func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		conns:  make(map[string]*Conn),
		logger: logger,
	}
}

func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	h.conns[c.id] = c
	h.mu.Unlock()

	h.totalConnections.Add(1)
	h.activeConns.Add(1)

	h.logger.Info("ws client connected",
		"conn", c.id,
		"active", h.activeConns.Load(),
	)
}

func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	delete(h.conns, id)
	h.mu.Unlock()

	h.activeConns.Add(-1)

	h.logger.Info("ws client disconnected",
		"conn", id,
		"active", h.activeConns.Load(),
	)
}

func (h *Hub) Stats() map[string]int64 {
	return map[string]int64{
		"active":    h.activeConns.Load(),
		"total":     h.totalConnections.Load(),
	}
}
