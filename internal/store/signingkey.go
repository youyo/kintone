package store

import (
	"context"
	"crypto/ecdsa"
)

// SigningKeyStore は idproxy が利用する ES256（P-256）署名鍵の永続化抽象。
//
// LoadOrCreate は、永続化されていなければ ecdsa.GenerateKey で新規生成して保存し、
// 既存があれば同じ鍵を返す。鍵ローテーションは Phase 7+ で別 API を追加予定。
type SigningKeyStore interface {
	// LoadOrCreate は永続鍵をロードする。未保存なら新規生成して保存する。
	LoadOrCreate(ctx context.Context) (*ecdsa.PrivateKey, error)
	// Close は内部リソースを解放する。冪等。
	Close() error
}
