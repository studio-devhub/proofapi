package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Host     string
	Port     string
	Password string
}

type Redis struct {
	client *redis.Client
}

func NewRedis(cfg Config) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           0,
		PoolSize:     20,
		MinIdleConns: 5,
		MaxRetries:   3,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &Redis{client: client}, nil
}

func (r *Redis) Close() error {
	return r.client.Close()
}

func BuildKey(parts ...string) string {
	if len(parts) < 2 {
		panic("BuildKey requires at least prefix and one key part")
	}
	prefix := parts[0]
	hash := sha256.Sum256([]byte(strings.Join(parts[1:], "|")))
	return fmt.Sprintf("%s:%x", prefix, hash)
}

func (r *Redis) Get(ctx context.Context, key string, dest any) (bool, error) {
	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("redis get: %w", err)
	}
	return true, json.Unmarshal(data, dest)
}

func (r *Redis) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return r.client.SetEx(ctx, key, data, ttl).Err()
}

func (r *Redis) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}

func (r *Redis) DeletePattern(ctx context.Context, pattern string) (int64, error) {
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil || len(keys) == 0 {
		return 0, err
	}
	return r.client.Del(ctx, keys...).Result()
}

func (r *Redis) Ping(ctx context.Context) bool {
	return r.client.Ping(ctx).Err() == nil
}

// ── Set operations (for dictionary cache) ─────────────────

func (r *Redis) SAdd(ctx context.Context, key string, members ...any) error {
	return r.client.SAdd(ctx, key, members...).Err()
}

func (r *Redis) SRem(ctx context.Context, key string, members ...any) error {
	return r.client.SRem(ctx, key, members...).Err()
}

func (r *Redis) SMembers(ctx context.Context, key string) ([]string, error) {
	return r.client.SMembers(ctx, key).Result()
}

func (r *Redis) SExists(ctx context.Context, key string) (bool, error) {
	n, err := r.client.Exists(ctx, key).Result()
	return n > 0, err
}

func (r *Redis) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return r.client.Expire(ctx, key, ttl).Err()
}

func (r *Redis) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

type Stats struct {
	Hits       int64  `json:"hits"`
	Misses     int64  `json:"misses"`
	Keys       int64  `json:"keys"`
	MemoryUsed string `json:"memoryUsed"`
}

func (r *Redis) Stats(ctx context.Context) Stats {
	dbSize, _ := r.client.DBSize(ctx).Result()

	var hits, misses int64
	var memUsed string

	info, err := r.client.Info(ctx, "stats", "memory").Result()
	if err == nil {
		for _, line := range strings.Split(info, "\r\n") {
			switch {
			case strings.HasPrefix(line, "keyspace_hits:"):
				fmt.Sscanf(strings.TrimPrefix(line, "keyspace_hits:"), "%d", &hits)
			case strings.HasPrefix(line, "keyspace_misses:"):
				fmt.Sscanf(strings.TrimPrefix(line, "keyspace_misses:"), "%d", &misses)
			case strings.HasPrefix(line, "used_memory_human:"):
				memUsed = strings.TrimSpace(strings.TrimPrefix(line, "used_memory_human:"))
			}
		}
	}
	if memUsed == "" {
		memUsed = "N/A"
	}

	return Stats{
		Hits:       hits,
		Misses:     misses,
		Keys:       dbSize,
		MemoryUsed: memUsed,
	}
}
