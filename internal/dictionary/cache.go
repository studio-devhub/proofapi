package dictionary

import (
	"context"
	"fmt"
	"strings"
	"time"

	appredis "languagetool-backend/internal/cache"
)

const (
	dictTTL  = 24 * time.Hour
	// sentinel marks "loaded from DB, but user has zero words".
	// Prevents stampede: an empty dict never becomes a perpetual cache miss.
	dictSentinel = "__loaded__"
)

// DictCache wraps Redis Set operations for per-user word caching.
type DictCache struct {
	r *appredis.Redis
}

func NewDictCache(r *appredis.Redis) *DictCache {
	return &DictCache{r: r}
}

func dictKey(clientID string) string {
	return fmt.Sprintf("dict:%s", clientID)
}

// GetWords returns lowercased words for a user from Redis.
// Returns nil, false if not cached.
// Uses SMembers directly (no TOCTOU from a separate EXISTS check).
func (c *DictCache) GetWords(ctx context.Context, clientID string) ([]string, bool, error) {
	members, err := c.r.SMembers(ctx, dictKey(clientID))
	if err != nil {
		return nil, false, err
	}
	if len(members) == 0 {
		// Key doesn't exist — cache miss
		return nil, false, nil
	}
	// Filter out the sentinel; return real words (may be empty slice = valid empty dict)
	words := make([]string, 0, len(members))
	for _, m := range members {
		if m != dictSentinel {
			words = append(words, m)
		}
	}
	return words, true, nil
}

// SetWords atomically replaces the entire Redis Set for a user (called on DynamoDB load).
// Always stores the sentinel so empty dictionaries are cached and don't cause a DynamoDB stampede.
func (c *DictCache) SetWords(ctx context.Context, clientID string, words []Word) error {
	members := make([]any, 0, len(words)+1)
	members = append(members, dictSentinel) // always present so empty dict is cached
	for _, w := range words {
		members = append(members, strings.ToLower(w.Word))
	}
	return c.r.SetMembers(ctx, dictKey(clientID), members, dictTTL)
}

// AddWord invalidates the cache entry so the next read reloads from DynamoDB.
// Using SAdd after an eviction would create a one-word set that's missing prior words.
// Invalidation is safer: one extra DynamoDB read per word-add, but always consistent.
func (c *DictCache) AddWord(ctx context.Context, clientID, _ string) error {
	return c.r.Del(ctx, dictKey(clientID))
}

// RemoveWord removes a lowercased word from the Redis Set.
func (c *DictCache) RemoveWord(ctx context.Context, clientID, word string) error {
	return c.r.SRem(ctx, dictKey(clientID), strings.ToLower(word))
}

// Invalidate removes the entire user cache entry.
func (c *DictCache) Invalidate(ctx context.Context, clientID string) error {
	return c.r.Del(ctx, dictKey(clientID))
}
