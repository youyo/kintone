package store

import (
	"context"
	"time"
)

// TTL の既定値。CachingAPI decorator が apps / fields / list_apps レスポンスに適用する。
const (
	TTLApps     = 365 * 24 * time.Hour
	TTLFields   = 365 * 24 * time.Hour
	TTLListApps = 365 * 24 * time.Hour
)

// キープレフィックス（v1 スキーマ）。後方互換のため変更時は v2 に上げて旧キャッシュを暗黙無効化する。
const (
	KeyPrefixApps     = "v1:app:"
	KeyPrefixFields   = "v1:fields:"
	KeyPrefixListApps = "v1:list_apps:"
)

// CacheStore は KV キャッシュ抽象。
type CacheStore interface {
	// Get はキーに対応する value を返す。不在 / 期限切れは [ErrCacheMiss]。
	Get(ctx context.Context, key string) ([]byte, error)
	// Put は value を ttl 期限で保存する。同 key の既存値は上書き。
	// ttl <= 0 のときは無期限とする実装が許容される（memory backend の挙動）。
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete は単一 key を削除する。不在は no-op。
	Delete(ctx context.Context, key string) error
	// DeleteByPrefix は prefix 一致するキーを削除し、削除件数を返す。
	DeleteByPrefix(ctx context.Context, prefix string) (int, error)
	// Stats は現在のキャッシュ統計を返す。
	Stats(ctx context.Context) (Stats, error)
	// Close は内部リソースを解放する。冪等。
	Close() error
}

// CacheKey は scope と id から v1 スキーマのキャッシュキーを組み立てる。
//
// 既知 scope: "apps" / "fields" / "list_apps"。未知 scope は id のみを返す。
func CacheKey(scope, id string) string { return PrefixOfScope(scope) + id }

// PrefixOfScope は scope 名から KeyPrefix* 定数を返す。
func PrefixOfScope(scope string) string {
	switch scope {
	case "apps":
		return KeyPrefixApps
	case "fields":
		return KeyPrefixFields
	case "list_apps":
		return KeyPrefixListApps
	default:
		return ""
	}
}
