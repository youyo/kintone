package cache

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	// modernc.org/sqlite は pure Go SQLite ドライバ（cgo 不要）。
	// クロスコンパイル容易性を理由に M07 で採用。
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// SQLiteStore は Store の SQLite 実装。
//
// 内部 *sql.DB は connection pool を持ち、複数 goroutine から安全に呼べる
// （PRAGMA journal_mode=WAL + busy_timeout=5000 で competing writer を緩和）。
type SQLiteStore struct {
	db   *sql.DB
	path string
}

// Open は DB ファイルを開く（不在時は作成・親ディレクトリ作成・スキーマ適用）。
//
// 動作:
//  1. 親ディレクトリが存在しない場合のみ MkdirAll(0o700)
//  2. SQLite を開き schema.sql を適用
//  3. 新規作成時のみファイルを 0o600 にする（既存ファイルのパーミッションは尊重）
//  4. PRAGMA journal_mode=WAL; busy_timeout=5000; synchronous=NORMAL を設定
func Open(path string) (*SQLiteStore, error) {
	// 親ディレクトリ作成
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("cache: create dir %s: %w", dir, err)
		}
	}

	// 新規かどうかを記録（後でパーミッション設定するため）
	_, statErr := os.Stat(path)
	isNew := errors.Is(statErr, fs.ErrNotExist)

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("cache: open %s: %w", path, err)
	}
	// Ping でファイル作成と接続確認
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache: ping %s: %w", path, err)
	}

	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache: apply schema: %w", err)
	}

	if isNew {
		// 新規作成時のみ 0o600 を強制（既存ファイルのパーミッションは尊重）
		if err := os.Chmod(path, 0o600); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("cache: chmod %s: %w", path, err)
		}
	}

	return &SQLiteStore{db: db, path: path}, nil
}

// OpenIfExists はファイルが存在する場合のみ Open する。
//
// 不在時は (nil, false, nil) を返す。
// `kintone cache stats` / `cache clear` の「read/clear-only でファイル auto-create しない」要件を実現する。
func OpenIfExists(path string) (*SQLiteStore, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("cache: stat %s: %w", path, err)
	}
	s, err := Open(path)
	if err != nil {
		return nil, true, err
	}
	return s, true, nil
}

// Path は DB ファイルのパスを返す。
func (s *SQLiteStore) Path() string { return s.path }

// Get はキーに対応する value を返す（不在 / 期限切れは ErrCacheMiss）。
func (s *SQLiteStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("cache: store is closed")
	}
	now := time.Now().UnixNano()
	row := s.db.QueryRowContext(ctx, `SELECT value FROM cache WHERE key = ? AND expires_at > ?`, key, now)
	var v []byte
	if err := row.Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCacheMiss
		}
		return nil, fmt.Errorf("cache: get %s: %w", key, err)
	}
	return v, nil
}

// Put は value を ttl 期限で保存する。同 key の既存値は上書き。
func (s *SQLiteStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if s == nil || s.db == nil {
		return errors.New("cache: store is closed")
	}
	now := time.Now().UnixNano()
	expires := time.Now().Add(ttl).UnixNano()
	_, err := s.db.ExecContext(ctx, `INSERT INTO cache(key,value,expires_at,created_at) VALUES(?,?,?,?)
        ON CONFLICT(key) DO UPDATE SET value=excluded.value, expires_at=excluded.expires_at, created_at=excluded.created_at`,
		key, value, expires, now)
	if err != nil {
		return fmt.Errorf("cache: put %s: %w", key, err)
	}
	return nil
}

// Delete は単一 key を削除する。
func (s *SQLiteStore) Delete(ctx context.Context, key string) error {
	if s == nil || s.db == nil {
		return errors.New("cache: store is closed")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM cache WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("cache: delete %s: %w", key, err)
	}
	return nil
}

// DeleteByPrefix は prefix 一致するキーを削除し、削除件数を返す。
func (s *SQLiteStore) DeleteByPrefix(ctx context.Context, prefix string) (int, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("cache: store is closed")
	}
	// LIKE のメタ文字を escape してから glob する
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	res, err := s.db.ExecContext(ctx, `DELETE FROM cache WHERE key LIKE ? ESCAPE '\'`, escaped+"%")
	if err != nil {
		return 0, fmt.Errorf("cache: delete prefix %s: %w", prefix, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("cache: rows affected: %w", err)
	}
	return int(n), nil
}

// Stats は現在のキャッシュ統計を返す。
func (s *SQLiteStore) Stats(ctx context.Context) (Stats, error) {
	if s == nil || s.db == nil {
		return Stats{}, errors.New("cache: store is closed")
	}
	now := time.Now().UnixNano()
	st := Stats{DBPath: s.path, DBExists: true}

	if fi, err := os.Stat(s.path); err == nil {
		st.DBSizeBytes = fi.Size()
	}

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cache`).Scan(&st.Total); err != nil {
		return st, fmt.Errorf("cache: stats total: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cache WHERE expires_at <= ?`, now).Scan(&st.Expired); err != nil {
		return st, fmt.Errorf("cache: stats expired: %w", err)
	}

	var oldest sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(created_at) FROM cache`).Scan(&oldest); err != nil {
		return st, fmt.Errorf("cache: stats oldest: %w", err)
	}
	if oldest.Valid {
		st.OldestStored = time.Unix(0, oldest.Int64).UTC()
	}
	return st, nil
}

// Close は DB ハンドルを閉じる。
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}
