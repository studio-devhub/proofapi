package dictionary

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type HTTPHandler struct {
	svc    *Service
	logger *slog.Logger
}

func NewHTTPHandler(svc *Service, logger *slog.Logger) *HTTPHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTPHandler{svc: svc, logger: logger}
}

func (h *HTTPHandler) AddWord(w http.ResponseWriter, r *http.Request) {
	clientID := r.Header.Get("X-Client-ID")

	var req AddWordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	word, err := h.svc.AddWord(r.Context(), clientID, req.Word, req.Language)
	if err != nil {
		if isValidationErr(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		h.logger.Error("dict add word failed", "clientId", clientID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to add word")
		return
	}

	writeJSON(w, http.StatusCreated, word)
}

func (h *HTTPHandler) RemoveWord(w http.ResponseWriter, r *http.Request) {
	clientID := r.Header.Get("X-Client-ID")
	word := chi.URLParam(r, "word")

	if strings.TrimSpace(word) == "" {
		writeError(w, http.StatusBadRequest, "word is required")
		return
	}

	if err := h.svc.RemoveWord(r.Context(), clientID, word); err != nil {
		h.logger.Error("dict remove word failed", "clientId", clientID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to remove word")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"removed": word})
}

func (h *HTTPHandler) ListWords(w http.ResponseWriter, r *http.Request) {
	clientID := r.Header.Get("X-Client-ID")

	words, err := h.svc.ListWords(r.Context(), clientID)
	if err != nil {
		h.logger.Error("dict list words failed", "clientId", clientID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list words")
		return
	}

	if words == nil {
		words = []Word{}
	}

	writeJSON(w, http.StatusOK, ListResponse{
		ClientID: clientID,
		Words:  words,
		Count:  len(words),
	})
}

func (h *HTTPHandler) ClearAll(w http.ResponseWriter, r *http.Request) {
	clientID := r.Header.Get("X-Client-ID")

	if err := h.svc.ClearAll(r.Context(), clientID); err != nil {
		h.logger.Error("dict clear failed", "clientId", clientID, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to clear dictionary")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"cleared": true, "clientId": clientID})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func isValidationErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "cannot be empty") ||
		strings.Contains(msg, "too long") ||
		strings.Contains(msg, "no spaces")
}
