package dictionary

import (
	"context"
	"fmt"
	"strings"
	"time"

	appredis "languagetool-backend/internal/cache"
)

const dictTTL = 24 * time.Hour

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
func (c *DictCache) GetWords(ctx context.Context, clientID string) ([]string, bool, error) {
	key := dictKey(clientID)

	exists, err := c.r.SExists(ctx, key)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}

	words, err := c.r.SMembers(ctx, key)
	if err != nil {
		return nil, false, err
	}

	return words, true, nil
}

// SetWords replaces the entire Redis Set for a user (called on DynamoDB load).
func (c *DictCache) SetWords(ctx context.Context, clientID string, words []Word) error {
	key := dictKey(clientID)

	_ = c.r.Del(ctx, key)

	if len(words) > 0 {
		members := make([]any, len(words))
		for i, w := range words {
			members[i] = strings.ToLower(w.Word)
		}
		if err := c.r.SAdd(ctx, key, members...); err != nil {
			return err
		}
	}

	return c.r.Expire(ctx, key, dictTTL)
}

// AddWord adds a lowercased word to the Redis Set.
func (c *DictCache) AddWord(ctx context.Context, clientID, word string) error {
	key := dictKey(clientID)
	if err := c.r.SAdd(ctx, key, strings.ToLower(word)); err != nil {
		return err
	}
	return c.r.Expire(ctx, key, dictTTL)
}

// RemoveWord removes a lowercased word from the Redis Set.
func (c *DictCache) RemoveWord(ctx context.Context, clientID, word string) error {
	return c.r.SRem(ctx, dictKey(clientID), strings.ToLower(word))
}

// Invalidate removes the entire user cache entry.
func (c *DictCache) Invalidate(ctx context.Context, clientID string) error {
	return c.r.Del(ctx, dictKey(clientID))
}
