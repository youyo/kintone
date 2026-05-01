package dynamodb

import (
	"context"
	"sync"

	idproxy "github.com/youyo/idproxy"
	"github.com/youyo/kintone/internal/store"
)

// Container は DynamoDB backend の store.Container 実装。
//
// client は単独所有 (sub-store の Close は no-op)。lazy 初期化のために
// 各 sub-store は sync.Once で生成する。
type Container struct {
	client dynamoDBAPI
	table  string

	tokensOnce  sync.Once
	tokens      store.TokenStore
	cacheOnce   sync.Once
	cache       store.CacheStore
	signingOnce sync.Once
	signing     store.SigningKeyStore
	idpOnce     sync.Once
	idp         idproxy.Store

	closeOnce sync.Once
}

// newContainer は DescribeTable / DescribeTimeToLive で前提を検証してから
// Container を返す。失敗時は ErrTableNotFound / ErrGSIMissing / ErrTTLDisabled / ErrConnectionFailed を wrap する。
func newContainer(client dynamoDBAPI, table string) (*Container, error) {
	ctx := context.Background()
	if err := describeMandatory(ctx, client, table); err != nil {
		return nil, err
	}
	return &Container{client: client, table: table}, nil
}

// Tokens は TokenStore を返す (lazy)。
func (c *Container) Tokens() (store.TokenStore, error) {
	c.tokensOnce.Do(func() { c.tokens = newTokenStore(c.client, c.table) })
	return c.tokens, nil
}

// CacheForDecorator は CacheStore を返す (decorator 用、lazy)。
func (c *Container) CacheForDecorator() (store.CacheStore, error) { return c.cacheCommon() }

// CacheForAdmin は CacheStore を返す (admin コマンド用、lazy)。
// DynamoDB backend では decorator と admin で同一インスタンスを共有する。
func (c *Container) CacheForAdmin() (store.CacheStore, error) { return c.cacheCommon() }

func (c *Container) cacheCommon() (store.CacheStore, error) {
	c.cacheOnce.Do(func() { c.cache = newCacheStore(c.client, c.table) })
	return c.cache, nil
}

// SigningKey は SigningKeyStore を返す (lazy)。
func (c *Container) SigningKey() (store.SigningKeyStore, error) {
	c.signingOnce.Do(func() { c.signing = newSigningKeyStore(c.client, c.table) })
	return c.signing, nil
}

// IDProxyStore は idproxy.Store を返す (lazy)。
func (c *Container) IDProxyStore() (idproxy.Store, error) {
	c.idpOnce.Do(func() { c.idp = newIDProxyStore(c.client, c.table) })
	return c.idp, nil
}

// LocationString は backend 表示用 URL 文字列を返す。
func (c *Container) LocationString() string { return "dynamodb://" + c.table }

// Close は全 sub-store を解放する。冪等。
//
// DynamoDB client 自体は AWS SDK v2 の HTTP クライアントを共有するため
// 明示的な Close は不要。sub-store は no-op だが Close 義務を表明する。
func (c *Container) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		if c.idp != nil {
			_ = c.idp.Close()
		}
		if c.tokens != nil {
			_ = c.tokens.Close()
		}
		if c.cache != nil {
			_ = c.cache.Close()
		}
		if c.signing != nil {
			_ = c.signing.Close()
		}
	})
	return nil
}

// 静的 interface 検査。
var _ store.Container = (*Container)(nil)
