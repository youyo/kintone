package store

import (
	"context"
	"errors"
	"time"
)

// DefaultStateTTL は OAuth state の既定 TTL（SSO + MFA を考慮し 10 分）。
const DefaultStateTTL = 10 * time.Minute

// ErrStateNotFound は StateStore.Take で対象 state が存在しない（または期限切れ）ときに返る。
var ErrStateNotFound = errors.New("store: oauth state not found")

// StateEntry は OAuth Authorization Code フローで state ↔ session を結びつける情報。
//
// Verifier は PKCE code_verifier、PrincipalID は idproxy 経由の OIDC principal。
// CreatedAt は TTL 判定に用いる。これらは authorization 開始時に Put し、
// callback 受信時に Take で取り出す。
type StateEntry struct {
	State       string
	PrincipalID string
	Verifier    string
	Method      string
	CreatedAt   time.Time
}

// StateStore は OAuth state ↔ session map の永続化抽象。
//
// Take は **one-shot semantics**: 成功時に entry を削除し、同一 state の再 Take は
// ErrStateNotFound を返す。期限切れ entry も ErrStateNotFound として扱う。
//
// 並行性: 複数 goroutine / 複数プロセスから同一 state を同時 Take した場合、
// **ちょうど 1 つだけ** が entry を取得し、残りはすべて ErrStateNotFound を得る
// （atomic Take 契約）。各 backend 実装は SQLite DELETE RETURNING / Redis Lua /
// DynamoDB DeleteItem ReturnValues=ALL_OLD などでこれを保証する。
type StateStore interface {
	// Put は新しい entry を保存する。同 state の上書きは許容（衝突確率は天文学的に低い）。
	// CreatedAt が zero のときは現在時刻で埋める。
	Put(ctx context.Context, entry StateEntry) error
	// Take は state に対応する entry を取り出し、同時に削除する（one-shot semantics）。
	// 期限切れ entry は ErrStateNotFound を返す。
	Take(ctx context.Context, state string) (*StateEntry, error)
	// Close は内部リソースを解放する。冪等。
	Close() error
}
