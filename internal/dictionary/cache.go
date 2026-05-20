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

func dictKey(clientID string) string    { return fmt.Sprintf("dict:%s", clientID) }
func dictVerKey(clientID string) string { return fmt.Sprintf("dict:ver:%s", clientID) }

// GetWords returns lowercased words and the current version from Redis.
// Returns nil, 0, false if not cached.
func (c *DictCache) GetWords(ctx context.Context, clientID string) ([]string, int64, bool, error) {
	members, err := c.r.SMembers(ctx, dictKey(clientID))
	if err != nil {
		return nil, 0, false, err
	}
	if len(members) == 0 {
		return nil, 0, false, nil // cache miss
	}
	ver, err := c.r.GetInt64(ctx, dictVerKey(clientID))
	if err != nil {
		return nil, 0, false, err
	}
	words := make([]string, 0, len(members))
	for _, m := range members {
		if m != dictSentinel {
			words = append(words, m)
		}
	}
	return words, ver, true, nil
}

// SetWordsIfVersion writes to Redis only if the version counter hasn't changed
// since the caller read it — prevents stale background cache writes.
func (c *DictCache) SetWordsIfVersion(ctx context.Context, clientID string, words []Word, expectedVer int64) error {
	members := make([]any, 0, len(words)+1)
	members = append(members, dictSentinel)
	for _, w := range words {
		members = append(members, strings.ToLower(w.Word))
	}
	return c.r.SetMembersIfVersion(ctx, dictKey(clientID), dictVerKey(clientID), members, expectedVer, dictTTL)
}

// GetVersion returns the current version counter for a client (0 if not set).
func (c *DictCache) GetVersion(ctx context.Context, clientID string) (int64, error) {
	return c.r.GetInt64(ctx, dictVerKey(clientID))
}

// BumpAndInvalidate increments the version counter and deletes the cache key atomically.
// Called on every write (add/remove/clear) so background loaders can detect concurrent writes.
func (c *DictCache) BumpAndInvalidate(ctx context.Context, clientID string) error {
	return c.r.IncrAndDel(ctx, dictVerKey(clientID), dictKey(clientID))
}

// Invalidate removes the cache entry without bumping the version (used by ClearAll).
func (c *DictCache) Invalidate(ctx context.Context, clientID string) error {
	return c.r.Del(ctx, dictKey(clientID))
}
