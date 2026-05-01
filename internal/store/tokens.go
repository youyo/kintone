package store

import "context"

// TokenStore は Token の永続ストア抽象。
//
// Get/Delete でキーが存在しないとき、実装は ErrNotFound を返す（または wrap）。
type TokenStore interface {
	// Get はキーに対応する Token を返す。不在は [ErrNotFound]。
	Get(ctx context.Context, domain, principalID string, t AuthType) (*Token, error)
	// Put は Token を保存する。同キーの既存値は上書き。
	// UpdatedAt が zero のときは現在時刻を自動設定する。
	Put(ctx context.Context, tok Token) error
	// Delete は単一 Token を削除する。不在は no-op。
	Delete(ctx context.Context, domain, principalID string, t AuthType) error
	// ListByDomain は domain + AuthType に一致する Token を principalID 昇順で返す。
	ListByDomain(ctx context.Context, domain string, t AuthType) ([]*Token, error)
	// Close は内部リソースを解放する。冪等。
	Close() error
}
