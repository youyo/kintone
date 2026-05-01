package store

import (
	"context"

	idproxy "github.com/youyo/idproxy"
)

// Container は 4 種類のサブストアを束ねる統合インターフェース。
//
// Tokens / CacheForDecorator / CacheForAdmin / SigningKey / IDProxyStore は
// それぞれ lazy 初期化される（実装は sync.Once 推奨）。
//
// CacheForDecorator は CachingAPI decorator が、CacheForAdmin は `cache stats/clear`
// 等の管理コマンドが利用する。memory / sqlite では同一インスタンスを返すが、
// dynamodb では異なる読み取り戦略を選ぶ余地を残すため API を分離している。
type Container interface {
	Tokens() (TokenStore, error)
	CacheForDecorator() (CacheStore, error)
	CacheForAdmin() (CacheStore, error)
	SigningKey() (SigningKeyStore, error)
	IDProxyStore() (idproxy.Store, error)
	// LocationString は backend 種別を示す URL 風文字列を返す（例: "memory://" / "file:///path/to/dir/"）。
	// CLI / MCP の `cache stats` 等の表示に利用する。
	LocationString() string
	// Close は全サブストアを解放する。冪等。
	Close(ctx context.Context) error
}

type ctxKey struct{}

// WithContainer は ctx に Container を埋め込み、子 ctx に Container を伝搬させる。
func WithContainer(ctx context.Context, c Container) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// ContainerFromContext は ctx から Container を取り出す。未設定 / nil ctx の場合は nil を返す。
func ContainerFromContext(ctx context.Context) Container {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(ctxKey{}).(Container); ok {
		return v
	}
	return nil
}
