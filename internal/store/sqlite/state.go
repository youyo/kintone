package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/youyo/kintone/internal/store"
)

// SQLiteStateStore は store.StateStore の SQLite 実装。
//
// テーブル: kintone_oauth_state（schema.sql で定義）。
// expires_at / created_at は Unix epoch nanoseconds。
//
// **one-shot Take は `DELETE ... RETURNING` による単一文 atomic 操作**で実装する
// （SQLite 3.35+, modernc.org/sqlite 1.50.0 で対応）。これにより並行 Take は
// ちょうど 1 つだけが winner になる。
//
// *sql.DB は Container が所有する。SQLiteStateStore.Close は no-op。
type SQLiteStateStore struct {
	db  *sql.DB
	ttl time.Duration
}

// NewStateStore は SQLiteStateStore を構築する。db は呼び出し側 (Container) が所有する。
func NewStateStore(db *sql.DB) *SQLiteStateStore {
	return &SQLiteStateStore{db: db, ttl: store.DefaultStateTTL}
}

// Put は新しい entry を保存する。同 state の上書きは許容。
func (s *SQLiteStateStore) Put(ctx context.Context, entry store.StateEntry) error {
	if entry.State == "" {
		return store.ErrStateNotFound
	}
	now := time.Now()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	createdAt := entry.CreatedAt.UnixNano()
	expiresAt := entry.CreatedAt.Add(s.ttl).UnixNano()
	method := entry.Method
	if method == "" {
		method = "S256"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kintone_oauth_state(state,principal_id,verifier,method,created_at,expires_at)
         VALUES(?,?,?,?,?,?)
         ON CONFLICT(state) DO UPDATE SET
           principal_id=excluded.principal_id,
           verifier=excluded.verifier,
           method=excluded.method,
           created_at=excluded.created_at,
           expires_at=excluded.expires_at`,
		entry.State, entry.PrincipalID, entry.Verifier, method, createdAt, expiresAt)
	if err != nil {
		return fmt.Errorf("store/sqlite: state put: %w", err)
	}
	return nil
}

// Take は state に対応する entry を atomic に取り出し削除する。
//
// SQLite の `DELETE ... RETURNING` は単一文として atomic に評価されるため、
// 並行 Take は最大 1 件が winner になる（残りは sql.ErrNoRows → ErrStateNotFound）。
func (s *SQLiteStateStore) Take(ctx context.Context, state string) (*store.StateEntry, error) {
	if state == "" {
		return nil, store.ErrStateNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`DELETE FROM kintone_oauth_state WHERE state = ?
         RETURNING principal_id, verifier, method, created_at, expires_at`, state)
	var (
		principalID string
		verifier    string
		method      string
		createdAt   int64
		expiresAt   int64
	)
	if err := row.Scan(&principalID, &verifier, &method, &createdAt, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrStateNotFound
		}
		return nil, fmt.Errorf("store/sqlite: state take: %w", err)
	}
	if expiresAt < time.Now().UnixNano() {
		// 期限切れ entry は既に DELETE 済みなので追加 cleanup 不要
		return nil, store.ErrStateNotFound
	}
	return &store.StateEntry{
		State:       state,
		PrincipalID: principalID,
		Verifier:    verifier,
		Method:      method,
		CreatedAt:   time.Unix(0, createdAt),
	}, nil
}

// Close は no-op。実際の *sql.DB.Close は Container が行う。
func (s *SQLiteStateStore) Close() error { return nil }

// ensure SQLiteStateStore implements store.StateStore at compile time
var _ store.StateStore = (*SQLiteStateStore)(nil)
