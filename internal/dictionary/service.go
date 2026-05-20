package dictionary

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Service orchestrates persistent storage (DynamoDB) and cache (Redis).
type Service struct {
	store  Store
	cache  *DictCache
	logger *slog.Logger
}

func NewService(store Store, cache *DictCache, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, cache: cache, logger: logger}
}

func (s *Service) AddWord(ctx context.Context, clientID, word, language string) (Word, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return Word{}, &ValidationError{Msg: "word cannot be empty"}
	}
	if len([]rune(word)) > 100 {
		return Word{}, &ValidationError{Msg: "word too long (max 100 characters)"}
	}
	if strings.ContainsAny(word, " \t\n\r") {
		return Word{}, &ValidationError{Msg: "word must be a single token (no spaces)"}
	}

	now := time.Now().UTC()
	if err := s.store.AddWord(ctx, clientID, word, language); err != nil {
		return Word{}, fmt.Errorf("store add: %w", err)
	}

	if err := s.cache.AddWord(ctx, clientID, word); err != nil {
		s.logger.Warn("dict cache add failed", "clientId", clientID, "word", word, "err", err)
	}

	return Word{Word: word, Language: language, AddedAt: now}, nil
}

func (s *Service) RemoveWord(ctx context.Context, clientID, word string) error {
	if err := s.store.RemoveWord(ctx, clientID, word); err != nil {
		return fmt.Errorf("store remove: %w", err)
	}

	if err := s.cache.RemoveWord(ctx, clientID, word); err != nil {
		s.logger.Warn("dict cache remove failed", "clientId", clientID, "word", word, "err", err)
	}

	return nil
}

func (s *Service) ListWords(ctx context.Context, clientID string) ([]Word, error) {
	words, err := s.store.ListWords(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("store list: %w", err)
	}
	return words, nil
}

func (s *Service) ClearAll(ctx context.Context, clientID string) error {
	if err := s.store.ClearAll(ctx, clientID); err != nil {
		return fmt.Errorf("store clear: %w", err)
	}
	if err := s.cache.Invalidate(ctx, clientID); err != nil {
		s.logger.Warn("dict cache invalidate failed", "clientId", clientID, "err", err)
	}
	return nil
}

// GetWordSet returns a lowercased word set for match filtering.
// This is the hot path — cache-first with DynamoDB fallback.
// Always returns a non-nil map; fails open on error.
func (s *Service) GetWordSet(ctx context.Context, clientID string) map[string]struct{} {
	if clientID == "" {
		return nil
	}

	cached, hit, err := s.cache.GetWords(ctx, clientID)
	if err != nil {
		s.logger.Warn("dict cache get failed, falling back to db", "clientId", clientID, "err", err)
	}

	if hit {
		return toSet(cached)
	}

	// DynamoDB fallback
	words, err := s.store.ListWords(ctx, clientID)
	if err != nil {
		s.logger.Warn("dict store list failed, proceeding unfiltered", "clientId", clientID, "err", err)
		return nil
	}

	// Populate cache in background — don't block the check request
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if cerr := s.cache.SetWords(bgCtx, clientID, words); cerr != nil {
			s.logger.Warn("dict cache set failed", "clientId", clientID, "err", cerr)
		}
	}()

	wordStrings := make([]string, len(words))
	for i, w := range words {
		wordStrings[i] = strings.ToLower(w.Word)
	}
	return toSet(wordStrings)
}

func toSet(words []string) map[string]struct{} {
	if len(words) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(words))
	for _, w := range words {
		set[w] = struct{}{}
	}
	return set
}
