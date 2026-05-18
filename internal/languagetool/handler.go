package languagetool

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"languagetool-backend/internal/cache"
)

const cachePrefix = "lt:check"
const cacheTTL = 5 * time.Minute

type Handler struct {
	client *Client
	redis  *cache.Redis
	logger *slog.Logger
}

func NewHandler(client *Client, redis *cache.Redis, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{client: client, redis: redis, logger: logger}
}

func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	var req CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Text = strings.TrimSpace(req.Text)
	if len(req.Text) < 2 || len(req.Text) > 20000 {
		writeError(w, http.StatusBadRequest, "text must be 2-20000 chars")
		return
	}
	if req.Language == "" {
		req.Language = "en-US"
	}
	if req.Level == "" {
		req.Level = "default"
	}

	cacheKey := cache.BuildKey(cachePrefix, req.Language, req.Level, req.Text)

	var cached CheckResponse
	hit, err := h.redis.Get(r.Context(), cacheKey, &cached)
	if err != nil {
		h.logger.Warn("redis get error", "err", err)
	}
	if hit {
		ttl, _ := h.redis.TTL(r.Context(), cacheKey)
		cached.Cached = true
		cached.ExpiresIn = int(ttl.Seconds())
		writeJSON(w, http.StatusOK, cached)
		return
	}

	result, err := h.client.Check(r.Context(), req)
	if err != nil {
		h.logger.Error("lt check failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "languagetool unavailable")
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := h.redis.Set(ctx, cacheKey, result, cacheTTL); err != nil {
			h.logger.Warn("redis set error", "err", err)
		}
	}()

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) Languages(w http.ResponseWriter, r *http.Request) {
	cacheKey := "lt:languages"

	var cached []map[string]any
	hit, _ := h.redis.Get(r.Context(), cacheKey, &cached)
	if hit {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	langs, err := h.client.Languages(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to fetch languages")
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		h.redis.Set(ctx, cacheKey, langs, time.Hour)
	}()

	writeJSON(w, http.StatusOK, langs)
}

func (h *Handler) ClearCache(w http.ResponseWriter, r *http.Request) {
	deleted, err := h.redis.DeletePattern(r.Context(), cachePrefix+":*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cache clear failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "unreachable"
}
