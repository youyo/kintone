package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/youyo/kintone/internal/store"
)

type cacheEntry struct {
	value     []byte
	expiresAt time.Time // IsZero なら無期限
}

func (e cacheEntry) isExpired(now time.Time) bool {
	if e.expiresAt.IsZero() {
		return false
	}
	return !now.Before(e.expiresAt)
}

// MemoryCacheStore はインメモリの [store.CacheStore] 実装。
type MemoryCacheStore struct {
	mu sync.RWMutex
	m  map[string]cacheEntry
}

// NewCacheStore は空の MemoryCacheStore を返す。
func NewCacheStore() *MemoryCacheStore {
	return &MemoryCacheStore{m: map[string]cacheEntry{}}
}

// Get はキーに対応する値を返す。不在 / 期限切れは store.ErrCacheMiss。
// 期限切れエントリは lazy delete する。
func (c *MemoryCacheStore) Get(_ context.Context, key string) ([]byte, error) {
	now := time.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return nil, store.ErrCacheMiss
	}
	if e.isExpired(now) {
		delete(c.m, key)
		return nil, store.ErrCacheMiss
	}
	out := make([]byte, len(e.value))
	copy(out, e.value)
	return out, nil
}

// Put は value を ttl 期限で保存する。ttl <= 0 のときは無期限とする。
func (c *MemoryCacheStore) Put(_ context.Context, key string, value []byte, ttl time.Duration) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().UTC().Add(ttl)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = cacheEntry{value: cp, expiresAt: exp}
	return nil
}

// Delete は単一 key を削除する。不在は no-op。
func (c *MemoryCacheStore) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, key)
	return nil
}

// DeleteByPrefix は prefix に一致するキーを削除し、削除件数を返す。
func (c *MemoryCacheStore) DeleteByPrefix(_ context.Context, prefix string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for k := range c.m {
		if strings.HasPrefix(k, prefix) {
			delete(c.m, k)
			n++
		}
	}
	return n, nil
}

// Stats は現在のキャッシュ統計を返す。
func (c *MemoryCacheStore) Stats(_ context.Context) (store.Stats, error) {
	now := time.Now().UTC()
	c.mu.RLock()
	defer c.mu.RUnlock()
	var total, expired int64
	for _, e := range c.m {
		total++
		if e.isExpired(now) {
			expired++
		}
	}
	expPtr := expired
	return store.Stats{
		Backend:      "memory",
		Location:     "memory://",
		Reachable:    true,
		EntryCount:   total,
		ExpiredCount: &expPtr,
	}, nil
}

// Close は内部マップを解放する。冪等。
func (c *MemoryCacheStore) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = map[string]cacheEntry{}
	return nil
}

// cleanupExpired は期限切れエントリを物理削除する。
// Container の cleanup goroutine から定期的に呼ばれる。
func (c *MemoryCacheStore) cleanupExpired() {
	now := time.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.m {
		if e.isExpired(now) {
			delete(c.m, k)
		}
	}
}
