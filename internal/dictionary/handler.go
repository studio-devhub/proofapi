package dictionary

import (
	"encoding/json"
	"errors"
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

// AddWord adds a word to the client's custom dictionary.
//
//	@Summary		Add word to dictionary
//	@Description	Adds a word to the client's personal dictionary. Future grammar checks will ignore this word.
//	@Tags			dictionary
//	@Accept			json
//	@Produce		json
//	@Param			X-Client-ID	header		string						true	"Client identifier"
//	@Param			request		body		docs.DictionaryAddRequest	true	"Word to add"
//	@Success		201			{object}	docs.DictionaryWord
//	@Failure		400			{object}	docs.ErrorResponse	"Invalid input"
//	@Failure		401			{object}	docs.ErrorResponse	"Missing or invalid API key"
//	@Failure		500			{object}	docs.ErrorResponse	"Storage error"
//	@Security		ApiKeyAuth
//	@Router			/dictionary/words [post]
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

// RemoveWord removes a word from the client's custom dictionary.
//
//	@Summary		Remove word from dictionary
//	@Description	Removes a word from the client's personal dictionary. Idempotent — no error if word does not exist.
//	@Tags			dictionary
//	@Produce		json
//	@Param			X-Client-ID	header		string	true	"Client identifier"
//	@Param			word		path		string	true	"Word to remove"
//	@Success		200			{object}	docs.DictionaryRemoveResponse
//	@Failure		400			{object}	docs.ErrorResponse	"Missing word"
//	@Failure		401			{object}	docs.ErrorResponse	"Missing or invalid API key"
//	@Failure		500			{object}	docs.ErrorResponse	"Storage error"
//	@Security		ApiKeyAuth
//	@Router			/dictionary/words/{word} [delete]
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

// ListWords returns all words in the client's custom dictionary.
//
//	@Summary		List dictionary words
//	@Description	Returns all words the client has added to their personal dictionary.
//	@Tags			dictionary
//	@Produce		json
//	@Param			X-Client-ID	header		string	true	"Client identifier"
//	@Success		200			{object}	docs.DictionaryListResponse
//	@Failure		401			{object}	docs.ErrorResponse	"Missing or invalid API key"
//	@Failure		500			{object}	docs.ErrorResponse	"Storage error"
//	@Security		ApiKeyAuth
//	@Router			/dictionary/words [get]
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

// ClearAll removes all words from the client's custom dictionary.
//
//	@Summary		Clear dictionary
//	@Description	Deletes all words from the client's personal dictionary.
//	@Tags			dictionary
//	@Produce		json
//	@Param			X-Client-ID	header		string	true	"Client identifier"
//	@Success		200			{object}	docs.DictionaryClearResponse
//	@Failure		401			{object}	docs.ErrorResponse	"Missing or invalid API key"
//	@Failure		500			{object}	docs.ErrorResponse	"Storage error"
//	@Security		ApiKeyAuth
//	@Router			/dictionary [delete]
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
	var ve *ValidationError
	return errors.As(err, &ve)
}
