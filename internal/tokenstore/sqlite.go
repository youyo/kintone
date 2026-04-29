package tokenstore

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	// modernc.org/sqlite は pure Go SQLite ドライバ（cgo 不要）。
	// cache パッケージと同じドライバを共有する。
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// SQLiteStore は Store の SQLite 実装。
//
// 内部 *sql.DB は connection pool を持ち、複数 goroutine から安全に呼べる
// （PRAGMA journal_mode=WAL + busy_timeout=5000）。
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
			return nil, fmt.Errorf("tokenstore: create dir %s: %w", dir, err)
		}
	}

	// 新規かどうかを記録（後でパーミッション設定するため）
	_, statErr := os.Stat(path)
	isNew := errors.Is(statErr, fs.ErrNotExist)

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("tokenstore: open %s: %w", path, err)
	}
	// Ping でファイル作成と接続確認
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("tokenstore: ping %s: %w", path, err)
	}

	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("tokenstore: apply schema: %w", err)
	}

	if isNew {
		// 新規作成時のみ 0o600 を強制（既存ファイルのパーミッションは尊重）
		if err := os.Chmod(path, 0o600); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("tokenstore: chmod %s: %w", path, err)
		}
	}

	return &SQLiteStore{db: db, path: path}, nil
}

// OpenIfExists はファイルが存在する場合のみ Open する。
//
// 不在時は (nil, false, nil) を返す。
func OpenIfExists(path string) (*SQLiteStore, bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("tokenstore: stat %s: %w", path, err)
	}
	s, err := Open(path)
	if err != nil {
		return nil, true, err
	}
	return s, true, nil
}

// Get はキーに対応する Token を返す（不在は ErrNotFound）。
func (s *SQLiteStore) Get(ctx context.Context, domain, principalID string, t AuthType) (*Token, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT domain, principal_id, auth_type, api_token, access_token, refresh_token, expires_at, updated_at
		 FROM tokens
		 WHERE domain=? AND principal_id=? AND auth_type=?`,
		domain, principalID, string(t),
	)
	var tok Token
	var expiresAtSec, updatedAtSec int64
	var authTypeStr string
	err := row.Scan(
		&tok.Domain, &tok.PrincipalID, &authTypeStr,
		&tok.APIToken, &tok.AccessToken, &tok.RefreshToken,
		&expiresAtSec, &updatedAtSec,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("tokenstore: get (%s,%s,%s): %w", domain, principalID, t, err)
	}
	tok.AuthType = AuthType(authTypeStr)
	if expiresAtSec > 0 {
		tok.ExpiresAt = time.Unix(expiresAtSec, 0).UTC()
	}
	tok.UpdatedAt = time.Unix(updatedAtSec, 0).UTC()
	return &tok, nil
}

// Put は Token を保存する。UpdatedAt が zero のときは現在時刻を自動設定する。
func (s *SQLiteStore) Put(ctx context.Context, tok Token) error {
	updatedAt := tok.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	var expiresAtSec int64
	if !tok.ExpiresAt.IsZero() {
		expiresAtSec = tok.ExpiresAt.Unix()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tokens(domain, principal_id, auth_type, api_token, access_token, refresh_token, expires_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(domain, principal_id, auth_type)
		 DO UPDATE SET
		   api_token=excluded.api_token,
		   access_token=excluded.access_token,
		   refresh_token=excluded.refresh_token,
		   expires_at=excluded.expires_at,
		   updated_at=excluded.updated_at`,
		tok.Domain, tok.PrincipalID, string(tok.AuthType),
		tok.APIToken, tok.AccessToken, tok.RefreshToken,
		expiresAtSec, updatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("tokenstore: put (%s,%s,%s): %w", tok.Domain, tok.PrincipalID, tok.AuthType, err)
	}
	return nil
}

// Delete は単一 Token を削除する。不在は no-op。
func (s *SQLiteStore) Delete(ctx context.Context, domain, principalID string, t AuthType) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM tokens WHERE domain=? AND principal_id=? AND auth_type=?`,
		domain, principalID, string(t),
	)
	if err != nil {
		return fmt.Errorf("tokenstore: delete (%s,%s,%s): %w", domain, principalID, t, err)
	}
	return nil
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
