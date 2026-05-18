package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"languagetool-backend/internal/cache"
)

func setupRedis(t *testing.T) (*cache.Redis, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	r, err := cache.NewRedis(cache.Config{
		Host:     mr.Host(),
		Port:     mr.Port(),
		Password: "",
	})
	require.NoError(t, err)
	return r, mr
}

func TestRedis_SetAndGet(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	type payload struct {
		Name  string `json:"name"`
		Score int    `json:"score"`
	}

	original := payload{Name: "test", Score: 42}
	err := r.Set(ctx, "key:1", original, time.Minute)
	require.NoError(t, err)

	var result payload
	hit, err := r.Get(ctx, "key:1", &result)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, original, result)
}

func TestRedis_GetMiss(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	var result map[string]any
	hit, err := r.Get(ctx, "nonexistent:key", &result)
	require.NoError(t, err)
	assert.False(t, hit)
	assert.Nil(t, result)
}

func TestRedis_TTLExpiry(t *testing.T) {
	r, mr := setupRedis(t)
	ctx := context.Background()

	err := r.Set(ctx, "expiring:key", "value", 2*time.Second)
	require.NoError(t, err)

	mr.FastForward(3 * time.Second)

	var result string
	hit, err := r.Get(ctx, "expiring:key", &result)
	require.NoError(t, err)
	assert.False(t, hit)
}

func TestRedis_DeletePattern(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	keys := []string{"lt:check:abc", "lt:check:def", "lt:check:xyz", "other:key"}
	for _, k := range keys {
		require.NoError(t, r.Set(ctx, k, "val", time.Minute))
	}

	deleted, err := r.DeletePattern(ctx, "lt:check:*")
	require.NoError(t, err)
	assert.Equal(t, int64(3), deleted)

	var val string
	hit, _ := r.Get(ctx, "other:key", &val)
	assert.True(t, hit)
}

func TestRedis_Ping(t *testing.T) {
	r, _ := setupRedis(t)
	assert.True(t, r.Ping(context.Background()))
}

func TestRedis_PingFail(t *testing.T) {
	r, mr := setupRedis(t)
	mr.Close()
	assert.False(t, r.Ping(context.Background()))
}

func TestBuildKey_Deterministic(t *testing.T) {
	k1 := cache.BuildKey("lt:check", "en-US", "default", "Hello world")
	k2 := cache.BuildKey("lt:check", "en-US", "default", "Hello world")
	k3 := cache.BuildKey("lt:check", "en-US", "default", "Different text")

	assert.Equal(t, k1, k2)
	assert.NotEqual(t, k1, k3)
}

func TestRedis_OverwriteKey(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	require.NoError(t, r.Set(ctx, "key:ow", "first", time.Minute))
	require.NoError(t, r.Set(ctx, "key:ow", "second", time.Minute))

	var result string
	hit, err := r.Get(ctx, "key:ow", &result)
	require.NoError(t, err)
	assert.True(t, hit)
	assert.Equal(t, "second", result)
}

func TestRedis_DeletePattern_NoMatch(t *testing.T) {
	r, _ := setupRedis(t)
	ctx := context.Background()

	deleted, err := r.DeletePattern(ctx, "no:match:*")
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)
}
