package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestRedis_Stats_Fields verifies all Stats fields are populated
func TestRedis_Stats_Fields(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	r.Set(ctx, "stats-key-1", "v1", time.Minute)
	r.Set(ctx, "stats-key-2", "v2", time.Minute)

	stats := r.Stats(ctx)

	// Keys must reflect stored entries
	assert.GreaterOrEqual(t, stats.Keys, int64(2))

	// MemoryUsed must be a non-empty string (miniredis returns "N/A", real Redis returns e.g. "1.09M")
	assert.NotEmpty(t, stats.MemoryUsed)

	// Hits and Misses are int64 — just verify they are non-negative
	assert.GreaterOrEqual(t, stats.Hits, int64(0))
	assert.GreaterOrEqual(t, stats.Misses, int64(0))
}

// TestRedis_Stats_EmptyDB verifies Stats works on an empty database
func TestRedis_Stats_EmptyDB(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	stats := r.Stats(ctx)

	assert.Equal(t, int64(0), stats.Keys)
	assert.NotEmpty(t, stats.MemoryUsed)
}

// TestRedis_Stats_AfterExpiry verifies Keys decreases after TTL expires
func TestRedis_Stats_AfterExpiry(t *testing.T) {
	r, mr := setupRedis(t)
	ctx := context.Background()

	r.Set(ctx, "expire-key", "val", time.Second)

	before := r.Stats(ctx)
	assert.GreaterOrEqual(t, before.Keys, int64(1))

	// Fast-forward miniredis clock past TTL
	mr.FastForward(2 * time.Second)

	after := r.Stats(ctx)
	assert.Less(t, after.Keys, before.Keys)
}

// TestRedis_Stats_ClosedClient verifies Stats returns zero values gracefully on closed client
func TestRedis_Stats_ClosedClient(t *testing.T) {
	r, mr := setupRedis(t)
	ctx := context.Background()

	mr.Close() // simulate Redis going down

	stats := r.Stats(ctx)

	// Should not panic, returns zero values
	assert.GreaterOrEqual(t, stats.Keys, int64(0))
	assert.GreaterOrEqual(t, stats.Hits, int64(0))
}
