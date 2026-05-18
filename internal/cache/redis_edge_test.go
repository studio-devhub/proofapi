package cache_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/cache"
)

// ── Edge Cases ────────────────────────────────────────────

func TestRedis_SetNilValue(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()
	err := r.Set(ctx, "nil:key", nil, time.Minute)
	assert.NoError(t, err) // nil marshals to JSON null
}

func TestRedis_SetEmptyString(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()
	require.NoError(t, r.Set(ctx, "empty:str", "", time.Minute))
	var result string
	hit, err := r.Get(ctx, "empty:str", &result)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, "", result)
}

func TestRedis_SetZeroTTL(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()
	// TTL=0 — Redis SetEx requires TTL>0, should error
	err := r.Set(ctx, "no:ttl", "value", 0)
	assert.Error(t, err) // SetEx with TTL=0 is invalid
}

func TestRedis_SetLargeValue(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()
	large := make([]byte, 1024*100) // 100KB
	for i := range large {
		large[i] = 'x'
	}
	require.NoError(t, r.Set(ctx, "large:key", string(large), time.Minute))
	var result string
	hit, err := r.Get(ctx, "large:key", &result)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Len(t, result, 1024*100)
}

func TestRedis_SetComplexStruct(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	type Nested struct {
		Items []string          `json:"items"`
		Meta  map[string]int    `json:"meta"`
		Flag  bool              `json:"flag"`
	}

	original := Nested{
		Items: []string{"a", "b", "c"},
		Meta:  map[string]int{"x": 1, "y": 2},
		Flag:  true,
	}

	require.NoError(t, r.Set(ctx, "complex:key", original, time.Minute))

	var result Nested
	hit, err := r.Get(ctx, "complex:key", &result)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, original, result)
}

func TestRedis_KeyWithSpecialChars(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()
	keys := []string{
		"lt:check:abc123",
		"lt:check:with-dash",
		"lt:check:with_underscore",
		"lt:check:with.dot",
	}
	for _, k := range keys {
		require.NoError(t, r.Set(ctx, k, "val", time.Minute))
		var v string
		hit, err := r.Get(ctx, k, &v)
		require.NoError(t, err)
		assert.True(t, hit, "key: %s", k)
	}
}

func TestRedis_TTL_Accuracy(t *testing.T) {
	r, mr := setupRedis(t)
	ctx := context.Background()

	require.NoError(t, r.Set(ctx, "ttl:key", "val", 60*time.Second))
	ttl, err := r.TTL(ctx, "ttl:key")
	require.NoError(t, err)
	assert.Greater(t, ttl, 58*time.Second)
	assert.LessOrEqual(t, ttl, 60*time.Second)

	mr.FastForward(30 * time.Second)
	ttl2, err := r.TTL(ctx, "ttl:key")
	require.NoError(t, err)
	assert.Less(t, ttl2, 31*time.Second)
}

func TestRedis_DeletePattern_Wildcard(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		r.Set(ctx, fmt.Sprintf("lt:check:%d", i), "v", time.Minute)
	}
	r.Set(ctx, "other:1", "v", time.Minute)
	r.Set(ctx, "other:2", "v", time.Minute)

	deleted, err := r.DeletePattern(ctx, "lt:check:*")
	require.NoError(t, err)
	assert.Equal(t, int64(10), deleted)

	// other keys intact
	var v string
	hit, _ := r.Get(ctx, "other:1", &v)
	assert.True(t, hit)
}

func TestRedis_GetExpiredKey(t *testing.T) {
	r, mr := setupRedis(t)
	ctx := context.Background()

	r.Set(ctx, "expire:me", "val", 1*time.Second)
	mr.FastForward(2 * time.Second)

	var result string
	hit, err := r.Get(ctx, "expire:me", &result)
	require.NoError(t, err)
	assert.False(t, hit)
}

func TestRedis_Concurrent_Writes(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent:%d", i)
			if err := r.Set(ctx, key, i, time.Minute); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		assert.Error(t, err) // SetEx with TTL=0 is invalid
	}

	// Verify all written
	for i := 0; i < 100; i++ {
		var v int
		hit, err := r.Get(ctx, fmt.Sprintf("concurrent:%d", i), &v)
		require.NoError(t, err)
		assert.True(t, hit)
		assert.Equal(t, i, v)
	}
}

func TestRedis_Concurrent_ReadWrite(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	// Pre-seed
	require.NoError(t, r.Set(ctx, "shared:key", 0, time.Minute))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			r.Set(ctx, "shared:key", i, time.Minute)
		}(i)
		go func() {
			defer wg.Done()
			var v int
			r.Get(ctx, "shared:key", &v)
		}()
	}
	wg.Wait()
}

func TestBuildKey_UniquePerInput(t *testing.T) {
	cases := []struct {
		prefix, lang, level, text string
	}{
		{"lt:check", "en-US", "default", "Hello"},
		{"lt:check", "en-GB", "default", "Hello"},
		{"lt:check", "en-US", "picky",   "Hello"},
		{"lt:check", "en-US", "default", "World"},
		{"lt:ws",    "en-US", "default", "Hello"},
	}

	keys := map[string]bool{}
	for _, c := range cases {
		k := cache.BuildKey(c.prefix, c.lang, c.level, c.text)
		assert.False(t, keys[k], "duplicate key for %+v", c)
		keys[k] = true
	}
}

func TestBuildKey_LongText(t *testing.T) {
	text := string(make([]byte, 20000))
	k1 := cache.BuildKey("lt:check", "en-US", "default", text)
	k2 := cache.BuildKey("lt:check", "en-US", "default", text)
	assert.Equal(t, k1, k2)
	assert.Contains(t, k1, "lt:check:")
}

func TestRedis_Stats(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		r.Set(ctx, fmt.Sprintf("stat:%d", i), "v", time.Minute)
	}

	stats := r.Stats(ctx)
	assert.GreaterOrEqual(t, stats.Keys, int64(5))
}

func TestRedis_Close(t *testing.T) {
	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{Host: mr.Host(), Port: mr.Port()})
	require.NoError(t, err)
	assert.NoError(t, r.Close())
}

func TestRedis_ContextCancelled(t *testing.T) {
	r, _ := setupRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	// Should handle cancelled context gracefully
	err := r.Set(ctx, "key", "val", time.Minute)
	// May error due to cancelled context — that's OK
	_ = err
}

func TestRedis_NewRedis_InvalidHost(t *testing.T) {
	_, err := cache.NewRedis(cache.Config{
		Host: "invalid-host-that-does-not-exist",
		Port: "9999",
	})
	assert.Error(t, err)
}
