package cache

import (
	"container/list"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

type lruEntry struct {
	key       string
	value     []byte
	expiresAt time.Time
}

// LRUCache is a thread-safe in-memory LRU cache with per-entry TTL.
// Entries expire lazily on the next Get that touches them.
type LRUCache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	list     *list.List
	index    map[string]*list.Element
}

func NewLRUCache(capacity int, ttl time.Duration) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		ttl:      ttl,
		list:     list.New(),
		index:    make(map[string]*list.Element, capacity),
	}
}

func (c *LRUCache) Get(_ context.Context, key string, dest any) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.index[key]
	if !ok {
		return false, nil
	}

	entry := el.Value.(*lruEntry)
	if time.Now().After(entry.expiresAt) {
		c.evict(el)
		return false, nil
	}

	c.list.MoveToFront(el)
	return true, json.Unmarshal(entry.value, dest)
}

func (c *LRUCache) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	// L1 TTL is always capped at its own configured TTL so it never
	// outlives L2 (Redis), which is the authoritative store.
	effective := ttl
	if effective <= 0 || effective > c.ttl {
		effective = c.ttl
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.index[key]; ok {
		entry := el.Value.(*lruEntry)
		entry.value = b
		entry.expiresAt = time.Now().Add(effective)
		c.list.MoveToFront(el)
		return nil
	}

	if c.list.Len() >= c.capacity {
		c.evict(c.list.Back())
	}

	entry := &lruEntry{key: key, value: b, expiresAt: time.Now().Add(effective)}
	el := c.list.PushFront(entry)
	c.index[key] = el
	return nil
}

func (c *LRUCache) TTL(_ context.Context, key string) (time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.index[key]
	if !ok {
		return -1, nil
	}
	entry := el.Value.(*lruEntry)
	remaining := time.Until(entry.expiresAt)
	if remaining <= 0 {
		c.evict(el)
		return -1, nil
	}
	return remaining, nil
}

func (c *LRUCache) DeletePattern(_ context.Context, pattern string) (int64, error) {
	prefix := strings.TrimSuffix(pattern, "*")

	c.mu.Lock()
	defer c.mu.Unlock()

	var deleted int64
	var next *list.Element
	for el := c.list.Back(); el != nil; el = next {
		next = el.Prev()
		entry := el.Value.(*lruEntry)
		if strings.HasPrefix(entry.key, prefix) {
			c.evict(el)
			deleted++
		}
	}
	return deleted, nil
}

// Len returns the number of live (possibly expired) entries. For observability and tests.
func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list.Len()
}

// evict removes el from both the list and the index. Caller must hold mu.
func (c *LRUCache) evict(el *list.Element) {
	entry := el.Value.(*lruEntry)
	delete(c.index, entry.key)
	c.list.Remove(el)
}
