// Package cache は kintone CLI/MCP の SQLite ベース KV キャッシュを提供する。
//
// 用途:
//   - apps / fields / list_apps レスポンスの 1 年 TTL キャッシュ
//   - 後続マイルストーン（M08 Resolver）の名前 → ID マップ
//
// キーは必ず先頭に "v1:" バージョンプレフィックスを付与する。
// 形式変更時は "v2:" に上げて旧キャッシュを暗黙無効化できる。
//
// パス決定:
//
//  1. KINTONE_CACHE_PATH 環境変数
//  2. $HOME/.cache/kintone/cache.db
//
// container 利用時は Dockerfile 側で env を明示する。
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrCacheMiss はキー不在 / 期限切れを表す sentinel エラー。
var ErrCacheMiss = errors.New("cache: miss")

// Store は KV キャッシュ抽象。
type Store interface {
	// Get はキーに対応する value を返す。不在 / 期限切れは ErrCacheMiss。
	Get(ctx context.Context, key string) ([]byte, error)
	// Put は value を ttl 期限で保存する。同 key の既存値は上書き。
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete は単一 key を削除する。不在は no-op。
	Delete(ctx context.Context, key string) error
	// DeleteByPrefix は prefix 一致するキーを削除し、削除件数を返す。
	DeleteByPrefix(ctx context.Context, prefix string) (int, error)
	// Stats は現在のキャッシュ統計を返す。
	Stats(ctx context.Context) (Stats, error)
	// Close は DB ハンドルを閉じる。
	Close() error
}

// Stats はキャッシュ DB の統計情報。
//
// JSON タグは `kintone cache stats` のレスポンスに直接利用される。
type Stats struct {
	DBPath       string    `json:"db_path"`
	DBExists     bool      `json:"db_exists"`
	DBSizeBytes  int64     `json:"db_size_bytes"`
	Total        int       `json:"total"`
	Expired      int       `json:"expired"`
	OldestStored time.Time `json:"oldest_stored,omitempty"`
}
