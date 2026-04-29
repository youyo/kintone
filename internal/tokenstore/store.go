// Package tokenstore は kintone CLI/MCP の認証情報永続ストアを提供する。
//
// M07 では plaintext + ファイル権限 0o600 の最低限保護。
// M09 OAuth 実装時に keyring 統合 + AES-GCM 暗号化を上乗せする設計。
//
// キー: Domain + PrincipalID + AuthType の組み合わせ。
// PrincipalID は OAuth 時に "provider:sub" 形式、API Token 時は空文字列。
//
// DB ファイル: cache.db とは別ファイル（tokens.db）。
// ライフサイクル分離（cache=TTL あり / tokens=明示削除のみ）と WAL 競合回避のため。
package tokenstore

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound はキーが存在しないときの sentinel エラー。
var ErrNotFound = errors.New("tokenstore: not found")

// AuthType は認証方式を表す型。
type AuthType string

const (
	// AuthTypeAPIToken は API トークン認証。
	AuthTypeAPIToken AuthType = "api-token"
	// AuthTypeOAuth は OAuth 2.0 認証（M09 で本格使用）。
	AuthTypeOAuth AuthType = "oauth"
)

// Token は認証情報の保存単位。
//
// APIToken / AccessToken / RefreshToken はどれか 1 つ以上が設定される。
// ログ出力時は "***" でマスクし、平文で外部に出さない。
type Token struct {
	Domain       string
	PrincipalID  string // provider:sub（OAuth）or "" + AuthTypeAPIToken
	AuthType     AuthType
	APIToken     string // AuthType=api-token のとき
	AccessToken  string // AuthType=oauth のとき
	RefreshToken string // AuthType=oauth のとき
	ExpiresAt    time.Time
	UpdatedAt    time.Time
}

// Store はトークン永続ストア抽象。
type Store interface {
	// Get はキーに対応する Token を返す。不在は ErrNotFound。
	Get(ctx context.Context, domain, principalID string, t AuthType) (*Token, error)
	// Put は Token を保存する。同キーの既存値は上書き。
	// UpdatedAt が zero のときは現在時刻を自動設定する。
	Put(ctx context.Context, tok Token) error
	// Delete は単一 Token を削除する。不在は no-op。
	Delete(ctx context.Context, domain, principalID string, t AuthType) error
	// Close は DB ハンドルを閉じる。
	Close() error
}
