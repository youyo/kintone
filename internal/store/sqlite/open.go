// Package sqlite は store の SQLite backend 実装を提供する。
//
// 採用ドライバ: modernc.org/sqlite (pure Go、CGO 不要)。
// kintone.db / idproxy.db を別ファイルで開き、両者を Container が束ねる。
package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	// modernc.org/sqlite は pure Go SQLite ドライバ（cgo 不要）。
	// クロスコンパイル容易性を理由に M07 で採用、M12 でも継承。
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// dsn は modernc.org/sqlite 用 DSN を組み立てる。
//
// PRAGMA は接続ごとに適用される (`_pragma=` クエリパラメータ)。
//   - busy_timeout=5000: 並行 writer の SQLITE_BUSY を 5 秒待ち
//   - journal_mode=WAL: 並行 reader/writer のレイテンシ向上
//   - synchronous=NORMAL: WAL とのバランス（fsync 削減 / 耐久性は sync 程度）
func dsn(path string) string {
	return "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
}

// OpenDB は path の SQLite DB を開いてスキーマを適用した *sql.DB を返す。
//
// 動作:
//  1. 親ディレクトリが存在しない場合のみ MkdirAll(0o700)
//  2. ファイル存在を記録
//  3. sql.Open + PingContext で接続確立
//  4. 新規作成だった場合のみ os.Chmod(path, 0o600)（既存ファイルのパーミッションは尊重）
//  5. schema.sql を ExecContext で適用（IF NOT EXISTS のため idempotent）
//
// 返却された *sql.DB は呼び出し側が Close する責務を持つ。
func OpenDB(ctx context.Context, path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("store/sqlite: create dir %s: %w", dir, err)
		}
	}

	_, statErr := os.Stat(path)
	isNew := errors.Is(statErr, fs.ErrNotExist)

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: open %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store/sqlite: ping %s: %w", path, err)
	}

	if isNew {
		if err := os.Chmod(path, 0o600); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store/sqlite: chmod %s: %w", path, err)
		}
	}

	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store/sqlite: apply schema %s: %w", path, err)
	}

	return db, nil
}
