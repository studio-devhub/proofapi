package cache

import (
	"context"
	"time"
)

// CheckCache is the interface used by grammar-check handlers.
// Both *Redis and *TieredCache satisfy it.
type CheckCache interface {
	Get(ctx context.Context, key string, dest any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	TTL(ctx context.Context, key string) (time.Duration, error)
	DeletePattern(ctx context.Context, pattern string) (int64, error)
}

// TieredCache is an L1 (in-memory LRU) in front of L2 (Redis).
// Reads are served from L1 when possible; L2 is always written first.
type TieredCache struct {
	l1 *LRUCache
	l2 *Redis
}

func NewTieredCache(l1 *LRUCache, l2 *Redis) *TieredCache {
	return &TieredCache{l1: l1, l2: l2}
}

func (t *TieredCache) Get(ctx context.Context, key string, dest any) (bool, error) {
	if hit, err := t.l1.Get(ctx, key, dest); hit {
		return true, err
	}

	hit, err := t.l2.Get(ctx, key, dest)
	if err != nil || !hit {
		return hit, err
	}

	// Backfill L1 — ignore error, L1 is best-effort
	_ = t.l1.Set(ctx, key, dest, t.l1.ttl)
	return true, nil
}

func (t *TieredCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	// L2 is authoritative — write there first
	if err := t.l2.Set(ctx, key, value, ttl); err != nil {
		return err
	}
	// Only populate L1 after a confirmed L2 write
	_ = t.l1.Set(ctx, key, value, ttl)
	return nil
}

func (t *TieredCache) TTL(ctx context.Context, key string) (time.Duration, error) {
	if remaining, err := t.l1.TTL(ctx, key); err == nil && remaining > 0 {
		return remaining, nil
	}
	return t.l2.TTL(ctx, key)
}

func (t *TieredCache) DeletePattern(ctx context.Context, pattern string) (int64, error) {
	_, _ = t.l1.DeletePattern(ctx, pattern)
	return t.l2.DeletePattern(ctx, pattern)
}
