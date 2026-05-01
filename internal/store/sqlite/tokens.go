package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/youyo/kintone/internal/store"
)

// SQLiteTokenStore は [store.TokenStore] の SQLite 実装。
//
// テーブル名: kintone_oauth_tokens（M12 で旧 tokens テーブルから rename）。
// expires_at / updated_at は Unix epoch seconds (INTEGER) 互換。
//
// *sql.DB は Container が所有する。SQLiteTokenStore.Close は no-op。
type SQLiteTokenStore struct {
	db *sql.DB
}

// NewTokenStore は SQLiteTokenStore を構築する。db は呼び出し側 (Container) が所有する。
func NewTokenStore(db *sql.DB) *SQLiteTokenStore {
	return &SQLiteTokenStore{db: db}
}

// Get はキーに対応する Token を返す。不在は store.ErrNotFound。
func (s *SQLiteTokenStore) Get(ctx context.Context, domain, principalID string, t store.AuthType) (*store.Token, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT domain, principal_id, auth_type, api_token, access_token, refresh_token, expires_at, updated_at
		 FROM kintone_oauth_tokens
		 WHERE domain=? AND principal_id=? AND auth_type=?`,
		domain, principalID, string(t),
	)
	var tok store.Token
	var expiresAtSec, updatedAtSec int64
	var authTypeStr string
	err := row.Scan(
		&tok.Domain, &tok.PrincipalID, &authTypeStr,
		&tok.APIToken, &tok.AccessToken, &tok.RefreshToken,
		&expiresAtSec, &updatedAtSec,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("store/sqlite: tokens get (%s,%s,%s): %w", domain, principalID, t, err)
	}
	tok.AuthType = store.AuthType(authTypeStr)
	if expiresAtSec > 0 {
		tok.ExpiresAt = time.Unix(expiresAtSec, 0).UTC()
	}
	tok.UpdatedAt = time.Unix(updatedAtSec, 0).UTC()
	return &tok, nil
}

// Put は Token を保存する。UpdatedAt が zero のときは現在時刻を自動設定する。
func (s *SQLiteTokenStore) Put(ctx context.Context, tok store.Token) error {
	updatedAt := tok.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	var expiresAtSec int64
	if !tok.ExpiresAt.IsZero() {
		expiresAtSec = tok.ExpiresAt.Unix()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kintone_oauth_tokens(domain, principal_id, auth_type, api_token, access_token, refresh_token, expires_at, updated_at)
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
		return fmt.Errorf("store/sqlite: tokens put (%s,%s,%s): %w", tok.Domain, tok.PrincipalID, tok.AuthType, err)
	}
	return nil
}

// Delete は単一 Token を削除する。不在は no-op。
func (s *SQLiteTokenStore) Delete(ctx context.Context, domain, principalID string, t store.AuthType) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM kintone_oauth_tokens WHERE domain=? AND principal_id=? AND auth_type=?`,
		domain, principalID, string(t),
	)
	if err != nil {
		return fmt.Errorf("store/sqlite: tokens delete (%s,%s,%s): %w", domain, principalID, t, err)
	}
	return nil
}

// ListByDomain は domain + AuthType に一致する Token を principalID 昇順で返す。
func (s *SQLiteTokenStore) ListByDomain(ctx context.Context, domain string, t store.AuthType) ([]*store.Token, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT domain, principal_id, auth_type, api_token, access_token, refresh_token, expires_at, updated_at
		 FROM kintone_oauth_tokens
		 WHERE domain=? AND auth_type=?
		 ORDER BY principal_id`,
		domain, string(t),
	)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: tokens list (%s,%s): %w", domain, t, err)
	}
	defer func() { _ = rows.Close() }()

	var result []*store.Token
	for rows.Next() {
		var tok store.Token
		var expiresAtSec, updatedAtSec int64
		var authTypeStr string
		if scanErr := rows.Scan(
			&tok.Domain, &tok.PrincipalID, &authTypeStr,
			&tok.APIToken, &tok.AccessToken, &tok.RefreshToken,
			&expiresAtSec, &updatedAtSec,
		); scanErr != nil {
			return nil, fmt.Errorf("store/sqlite: tokens scan: %w", scanErr)
		}
		tok.AuthType = store.AuthType(authTypeStr)
		if expiresAtSec > 0 {
			tok.ExpiresAt = time.Unix(expiresAtSec, 0).UTC()
		}
		tok.UpdatedAt = time.Unix(updatedAtSec, 0).UTC()
		result = append(result, &tok)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store/sqlite: tokens list rows: %w", err)
	}
	return result, nil
}

// Close は no-op。実際の *sql.DB.Close は Container が行う。
func (s *SQLiteTokenStore) Close() error { return nil }
