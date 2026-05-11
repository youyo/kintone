package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"

	goredis "github.com/redis/go-redis/v9"

	idproxy "github.com/youyo/idproxy"

	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/store"
)

// Container は Redis backend の [store.Container] 実装。
//
// 単一の redis.UniversalClient を保持し、kintone（"kintone:" prefix）と
// idproxy（"idproxy:" prefix）の両 sub-store でこれを共有する。
// client の Close 責任は Container にあり、sub-store / idproxy adapter の Close は no-op。
type Container struct {
	client    goredis.UniversalClient
	rawURL    string
	closeOnce sync.Once
	closeErr  error

	tokensOnce sync.Once
	tokens     *RedisTokenStore

	cacheOnce sync.Once
	cache     *RedisCacheStore

	signingOnce sync.Once
	signing     *RedisSigningKeyStore

	stateOnce sync.Once
	state     *RedisStateStore

	idproxyOnce  sync.Once
	idproxyStore idproxy.Store
}

// NewContainer は Redis backend の Container を構築する。
// client の所有権は Container が持つ（Close で閉じる）。
func NewContainer(client goredis.UniversalClient, rawURL string) *Container {
	return &Container{
		client: client,
		rawURL: rawURL,
	}
}

// Tokens は RedisTokenStore を lazy 初期化して返す。
func (c *Container) Tokens() (store.TokenStore, error) {
	c.tokensOnce.Do(func() { c.tokens = NewTokenStore(c.client) })
	return c.tokens, nil
}

// CacheForDecorator は RedisCacheStore を lazy 初期化して返す。
func (c *Container) CacheForDecorator() (store.CacheStore, error) {
	return c.initCache(), nil
}

// CacheForAdmin は CacheForDecorator と同一インスタンスを返す（Redis では分離不要）。
func (c *Container) CacheForAdmin() (store.CacheStore, error) {
	return c.initCache(), nil
}

func (c *Container) initCache() *RedisCacheStore {
	c.cacheOnce.Do(func() {
		c.cache = NewCacheStore(c.client, c.LocationString())
	})
	return c.cache
}

// SigningKey は RedisSigningKeyStore を lazy 初期化して返す。
func (c *Container) SigningKey() (store.SigningKeyStore, error) {
	c.signingOnce.Do(func() { c.signing = NewSigningKeyStore(c.client) })
	return c.signing, nil
}

// StateStore は RedisStateStore を lazy 初期化して返す。
func (c *Container) StateStore() (store.StateStore, error) {
	c.stateOnce.Do(func() { c.state = NewStateStore(c.client) })
	return c.state, nil
}

// IDProxyStore は idproxy 用 Redis store を lazy 初期化して返す。
//
// 内部で `idproxy/store/redis.NewWithClient(client, "idproxy:")` を呼び、
// Close を no-op にする adapter で wrap する。
func (c *Container) IDProxyStore() (idproxy.Store, error) {
	c.idproxyOnce.Do(func() { c.idproxyStore = newIDProxyStore(c.client) })
	return c.idproxyStore, nil
}

// LocationString は backend の保存場所を表す URL 風文字列を返す（sanitized）。
func (c *Container) LocationString() string {
	return output.SanitizeURL(c.rawURL)
}

// Close は idproxy adapter（no-op）→ kintone sub-stores（no-op）→ client.Close の順で
// 逆依存順クローズする。冪等。
func (c *Container) Close(_ context.Context) error {
	c.closeOnce.Do(func() {
		var errs []error
		// idproxy adapter は no-op だが順序整合のため呼ぶ
		if c.idproxyStore != nil {
			if err := c.idproxyStore.Close(); err != nil {
				errs = append(errs, fmt.Errorf("store/redis: close idproxy adapter: %w", err))
			}
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
		if c.state != nil {
			_ = c.state.Close()
		}
		// 最後に共有 client を閉じる
		if c.client != nil {
			if err := c.client.Close(); err != nil {
				errs = append(errs, fmt.Errorf("store/redis: close client: %w", err))
			}
		}
		c.closeErr = errors.Join(errs...)
	})
	return c.closeErr
}

// ensure Container implements store.Container at compile time
var _ store.Container = (*Container)(nil)
