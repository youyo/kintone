package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/youyo/kintone/internal/store"
)

// SQLiteCacheStore は [store.CacheStore] の SQLite 実装。
//
// テーブル名: kintone_kv_cache（M12 で旧 cache テーブルから rename）。
// expires_at / created_at は Unix epoch nanoseconds (INTEGER)。
//
// *sql.DB は Container が所有する。SQLiteCacheStore.Close は no-op。
type SQLiteCacheStore struct {
	db   *sql.DB
	path string
}

// NewCacheStore は SQLiteCacheStore を構築する。db は呼び出し側 (Container) が所有する。
// path は Stats.BackendSpecific.db_size_bytes 取得のための DB ファイルパス。
func NewCacheStore(db *sql.DB, path string) *SQLiteCacheStore {
	return &SQLiteCacheStore{db: db, path: path}
}

// Get はキーに対応する value を返す。不在 / 期限切れは store.ErrCacheMiss。
// 期限切れエントリは検出時に lazy delete する。
func (c *SQLiteCacheStore) Get(ctx context.Context, key string) ([]byte, error) {
	now := time.Now().UnixNano()
	var v []byte
	var expiresAt int64
	row := c.db.QueryRowContext(ctx, `SELECT value, expires_at FROM kintone_kv_cache WHERE key = ?`, key)
	if err := row.Scan(&v, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrCacheMiss
		}
		return nil, fmt.Errorf("store/sqlite: cache get %s: %w", key, err)
	}
	if expiresAt < now {
		// lazy delete + cache miss
		_, _ = c.db.ExecContext(ctx, `DELETE FROM kintone_kv_cache WHERE key = ? AND expires_at < ?`, key, now)
		return nil, store.ErrCacheMiss
	}
	return v, nil
}

// Put は value を ttl 期限で保存する。同 key の既存値は上書き。
// ttl <= 0 のときは無期限相当（int64 max 近傍）として保存する。
func (c *SQLiteCacheStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	now := time.Now().UnixNano()
	var expires int64
	if ttl > 0 {
		expires = time.Now().Add(ttl).UnixNano()
	} else {
		// 約 292 年後 (math.MaxInt64) に相当する値で「実質無期限」を表現。
		expires = int64(^uint64(0) >> 1)
	}
	_, err := c.db.ExecContext(ctx,
		`INSERT INTO kintone_kv_cache(key,value,expires_at,created_at) VALUES(?,?,?,?)
         ON CONFLICT(key) DO UPDATE SET value=excluded.value, expires_at=excluded.expires_at, created_at=excluded.created_at`,
		key, value, expires, now)
	if err != nil {
		return fmt.Errorf("store/sqlite: cache put %s: %w", key, err)
	}
	return nil
}

// Delete は単一 key を削除する。不在は no-op。
func (c *SQLiteCacheStore) Delete(ctx context.Context, key string) error {
	_, err := c.db.ExecContext(ctx, `DELETE FROM kintone_kv_cache WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("store/sqlite: cache delete %s: %w", key, err)
	}
	return nil
}

// DeleteByPrefix は prefix 一致するキーを削除し、削除件数を返す。
func (c *SQLiteCacheStore) DeleteByPrefix(ctx context.Context, prefix string) (int, error) {
	// LIKE のメタ文字を escape してから glob する
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	res, err := c.db.ExecContext(ctx, `DELETE FROM kintone_kv_cache WHERE key LIKE ? ESCAPE '\'`, escaped+"%")
	if err != nil {
		return 0, fmt.Errorf("store/sqlite: cache delete prefix %s: %w", prefix, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store/sqlite: cache rows affected: %w", err)
	}
	return int(n), nil
}

// Stats は現在のキャッシュ統計を返す。
//
// EntryCount は全件、ExpiredCount は expires_at < now() の件数。
// BackendSpecific には db_size_bytes を含める。
func (c *SQLiteCacheStore) Stats(ctx context.Context) (store.Stats, error) {
	now := time.Now().UnixNano()
	var entryCount int64
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kintone_kv_cache`).Scan(&entryCount); err != nil {
		return store.Stats{}, fmt.Errorf("store/sqlite: cache stats count: %w", err)
	}
	var expired int64
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kintone_kv_cache WHERE expires_at < ?`, now).Scan(&expired); err != nil {
		return store.Stats{}, fmt.Errorf("store/sqlite: cache stats expired: %w", err)
	}
	expPtr := expired

	bs := map[string]any{}
	if c.path != "" {
		if fi, err := os.Stat(c.path); err == nil {
			bs["db_size_bytes"] = fi.Size()
		}
	}
	return store.Stats{
		Backend:         "sqlite",
		Location:        "sqlite:///" + c.path,
		Reachable:       true,
		EntryCount:      entryCount,
		ExpiredCount:    &expPtr,
		BackendSpecific: bs,
	}, nil
}

// Close は no-op。実際の *sql.DB.Close は Container が行う。
func (c *SQLiteCacheStore) Close() error { return nil }
