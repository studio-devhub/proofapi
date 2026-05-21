package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRU_GetSet(t *testing.T) {
	c := NewLRUCache(10, time.Minute)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k1", map[string]string{"hello": "world"}, time.Minute))

	var dest map[string]string
	hit, err := c.Get(ctx, "k1", &dest)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, "world", dest["hello"])
}

func TestLRU_Miss(t *testing.T) {
	c := NewLRUCache(10, time.Minute)
	var dest map[string]string
	hit, err := c.Get(context.Background(), "missing", &dest)
	require.NoError(t, err)
	assert.False(t, hit)
}

func TestLRU_TTLExpiry(t *testing.T) {
	c := NewLRUCache(10, time.Minute)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k1", "value", 50*time.Millisecond))
	time.Sleep(80 * time.Millisecond)

	var dest string
	hit, err := c.Get(ctx, "k1", &dest)
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Equal(t, 0, c.Len())
}

func TestLRU_CapacityEviction(t *testing.T) {
	c := NewLRUCache(5, time.Minute)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, c.Set(ctx, fmt.Sprintf("k%d", i), i, time.Minute))
	}
	assert.Equal(t, 5, c.Len())

	// k0 is LRU — inserting k5 should evict k0
	require.NoError(t, c.Set(ctx, "k5", 5, time.Minute))
	assert.Equal(t, 5, c.Len())

	var dest int
	hit, _ := c.Get(ctx, "k0", &dest)
	assert.False(t, hit, "k0 should have been evicted")

	hit, _ = c.Get(ctx, "k5", &dest)
	assert.True(t, hit)
}

func TestLRU_LRUPromotion(t *testing.T) {
	c := NewLRUCache(3, time.Minute)
	ctx := context.Background()

	c.Set(ctx, "k0", 0, time.Minute)
	c.Set(ctx, "k1", 1, time.Minute)
	c.Set(ctx, "k2", 2, time.Minute)

	// Access k0 — it becomes MRU
	var dest int
	c.Get(ctx, "k0", &dest)

	// k1 is now LRU — k3 should evict k1
	c.Set(ctx, "k3", 3, time.Minute)

	hit, _ := c.Get(ctx, "k1", &dest)
	assert.False(t, hit, "k1 should have been evicted")

	hit, _ = c.Get(ctx, "k0", &dest)
	assert.True(t, hit, "k0 should still be present")
}

func TestLRU_UpdateExisting(t *testing.T) {
	c := NewLRUCache(10, time.Minute)
	ctx := context.Background()

	c.Set(ctx, "k1", "old", time.Minute)
	c.Set(ctx, "k1", "new", time.Minute)
	assert.Equal(t, 1, c.Len())

	var dest string
	hit, _ := c.Get(ctx, "k1", &dest)
	assert.True(t, hit)
	assert.Equal(t, "new", dest)
}

func TestLRU_TTLCappedAtConfigured(t *testing.T) {
	c := NewLRUCache(10, 5*time.Minute)
	ctx := context.Background()

	// Pass 30min TTL — should be capped at 5min
	c.Set(ctx, "k1", "val", 30*time.Minute)

	remaining, err := c.TTL(ctx, "k1")
	require.NoError(t, err)
	assert.LessOrEqual(t, remaining, 5*time.Minute)
	assert.Greater(t, remaining, 4*time.Minute)
}

func TestLRU_DeletePattern(t *testing.T) {
	c := NewLRUCache(10, time.Minute)
	ctx := context.Background()

	c.Set(ctx, "lt:check:abc", 1, time.Minute)
	c.Set(ctx, "lt:check:def", 2, time.Minute)
	c.Set(ctx, "lt:ws:xyz", 3, time.Minute)

	deleted, err := c.DeletePattern(ctx, "lt:check:*")
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)
	assert.Equal(t, 1, c.Len())

	var dest int
	hit, _ := c.Get(ctx, "lt:ws:xyz", &dest)
	assert.True(t, hit)
}

func TestLRU_ConcurrentReadWrite(t *testing.T) {
	c := NewLRUCache(100, time.Minute)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			c.Set(ctx, fmt.Sprintf("k%d", i), i, time.Minute)
		}(i)
		go func(i int) {
			defer wg.Done()
			var dest int
			c.Get(ctx, fmt.Sprintf("k%d", i), &dest)
		}(i)
	}
	wg.Wait()
}
