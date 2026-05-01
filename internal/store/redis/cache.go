package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/store"
)

// cacheKeyPrefix は kintone Cache store の Redis キー prefix。
const cacheKeyPrefix = "kintone:cache:"

// unlinkBatchSize は DeleteByPrefix の UNLINK バッチサイズ。
const unlinkBatchSize = 500

// RedisCacheStore は [store.CacheStore] の Redis 実装。
//
// kintone:cache:{key} に SET ... EX ttl で保存する。Redis native TTL に委譲し、
// Get では redis.Nil を ErrCacheMiss にマッピングする。
//
// client は Container が所有する。RedisCacheStore.Close は no-op。
type RedisCacheStore struct {
	client   goredis.UniversalClient
	location string // sanitized URL（Stats.Location 用）
}

// NewCacheStore は RedisCacheStore を構築する。client は呼び出し側 (Container) が所有する。
// location は Stats.Location 表示用（sanitized URL を渡す）。
func NewCacheStore(client goredis.UniversalClient, location string) *RedisCacheStore {
	return &RedisCacheStore{client: client, location: location}
}

// Get はキーに対応する value を返す。不在 / 期限切れは store.ErrCacheMiss。
func (c *RedisCacheStore) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := c.client.Get(ctx, cacheKeyPrefix+key).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, store.ErrCacheMiss
		}
		return nil, fmt.Errorf("store/redis: cache get %s: %w", key, err)
	}
	return v, nil
}

// Put は value を ttl 期限で保存する。同 key の既存値は上書き。
// ttl <= 0 のときは TTL なし（永続）として保存する。
func (c *RedisCacheStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	var expire time.Duration
	if ttl > 0 {
		expire = ttl
	}
	if err := c.client.Set(ctx, cacheKeyPrefix+key, value, expire).Err(); err != nil {
		return fmt.Errorf("store/redis: cache put %s: %w", key, err)
	}
	return nil
}

// Delete は単一 key を削除する。不在は no-op。
func (c *RedisCacheStore) Delete(ctx context.Context, key string) error {
	if err := c.client.Del(ctx, cacheKeyPrefix+key).Err(); err != nil {
		return fmt.Errorf("store/redis: cache delete %s: %w", key, err)
	}
	return nil
}

// DeleteByPrefix は prefix 一致するキーを削除し、削除件数を返す。
//
// SCAN MATCH "kintone:cache:{prefix}*" でキー列挙 → unlinkBatchSize 件ごとに UNLINK。
func (c *RedisCacheStore) DeleteByPrefix(ctx context.Context, prefix string) (int, error) {
	pattern := cacheKeyPrefix + prefix + "*"
	var (
		cursor  uint64
		deleted int
		batch   []string
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := c.client.Unlink(ctx, batch...).Result()
		if err != nil {
			// UNLINK 未対応の Redis 互換実装（miniredis 等）に対するフォールバック
			n, err = c.client.Del(ctx, batch...).Result()
			if err != nil {
				return fmt.Errorf("store/redis: cache delete prefix %s: %w", prefix, err)
			}
		}
		deleted += int(n)
		batch = batch[:0]
		return nil
	}
	for {
		ks, next, err := c.client.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			return deleted, fmt.Errorf("store/redis: cache scan %s: %w", pattern, err)
		}
		batch = append(batch, ks...)
		for len(batch) >= unlinkBatchSize {
			head := batch[:unlinkBatchSize]
			rest := append([]string{}, batch[unlinkBatchSize:]...)
			n, err := c.client.Unlink(ctx, head...).Result()
			if err != nil {
				n, err = c.client.Del(ctx, head...).Result()
				if err != nil {
					return deleted, fmt.Errorf("store/redis: cache delete prefix %s: %w", prefix, err)
				}
			}
			deleted += int(n)
			batch = rest
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	if err := flush(); err != nil {
		return deleted, err
	}
	return deleted, nil
}

// Stats は現在のキャッシュ統計を返す。
//
// EntryCount は SCAN MATCH "kintone:cache:*" でカウント、ExpiredCount は Redis 上では
// 算出不能なので nil。BackendSpecific には INFO memory の used_memory を含める（best-effort）。
func (c *RedisCacheStore) Stats(ctx context.Context) (store.Stats, error) {
	reachable := true
	if err := c.client.Ping(ctx).Err(); err != nil {
		reachable = false
		output.Logger().Warn("redis ping failed during stats", "err", err.Error())
	}

	var entryCount int64
	var cursor uint64
	for {
		ks, next, err := c.client.Scan(ctx, cursor, cacheKeyPrefix+"*", scanCount).Result()
		if err != nil {
			return store.Stats{}, fmt.Errorf("store/redis: cache stats scan: %w", err)
		}
		entryCount += int64(len(ks))
		if next == 0 {
			break
		}
		cursor = next
	}

	bs := map[string]any{}
	if info, err := c.client.Info(ctx, "memory").Result(); err == nil {
		if used, ok := parseUsedMemory(info); ok {
			bs["memory_used_bytes"] = used
		}
	}

	return store.Stats{
		Backend:         "redis",
		Location:        c.location,
		Reachable:       reachable,
		EntryCount:      entryCount,
		ExpiredCount:    nil, // Redis では算出不可
		BackendSpecific: bs,
	}, nil
}

// Close は no-op。実際の client.Close は Container が行う。
func (c *RedisCacheStore) Close() error { return nil }

// parseUsedMemory は INFO memory の出力から `used_memory:N` を抽出する。
func parseUsedMemory(info string) (int64, bool) {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		const key = "used_memory:"
		if strings.HasPrefix(line, key) {
			n, err := strconv.ParseInt(strings.TrimPrefix(line, key), 10, 64)
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}

// ensure RedisCacheStore implements store.CacheStore at compile time
var _ store.CacheStore = (*RedisCacheStore)(nil)
