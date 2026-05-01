package memory

import (
	"context"
	"errors"
	"sync"
	"time"

	idproxy "github.com/youyo/idproxy"

	"github.com/youyo/kintone/internal/store"
)

// Container は memory backend の [store.Container] 実装。
//
// 各サブストアは sync.Once で lazy 初期化される。Close は冪等で、
// 初回呼び出しのみで cleanup goroutine を停止し全サブストアを Close する。
type Container struct {
	closeCtx    context.Context
	closeCancel context.CancelFunc
	closeOnce   sync.Once

	cleanupInterval time.Duration
	cleanupOnce     sync.Once
	cleanupDone     chan struct{}
	cleanupStartMu  sync.Mutex
	cleanupStartFlg bool

	tokensOnce sync.Once
	tokens     *MemoryTokenStore

	cacheOnce sync.Once
	cache     *MemoryCacheStore

	signingOnce sync.Once
	signing     *MemorySigningKeyStore

	idproxyOnce  sync.Once
	idproxyStore idproxy.Store
}

// New は memory backend の Container を構築する。
//
// cleanupInterval > 0 のとき、CacheStore の期限切れエントリを定期的にスイープする
// goroutine を起動する。<= 0 のときは goroutine を起動しない。
// goroutine は Close() で確実に停止する。
func New(cleanupInterval time.Duration) *Container {
	ctx, cancel := context.WithCancel(context.Background())
	return &Container{
		closeCtx:        ctx,
		closeCancel:     cancel,
		cleanupInterval: cleanupInterval,
		cleanupDone:     make(chan struct{}),
	}
}

// Tokens は MemoryTokenStore を lazy 初期化して返す。
func (c *Container) Tokens() (store.TokenStore, error) {
	c.tokensOnce.Do(func() { c.tokens = NewTokenStore() })
	return c.tokens, nil
}

// CacheForDecorator は MemoryCacheStore を lazy 初期化して返す。
func (c *Container) CacheForDecorator() (store.CacheStore, error) {
	return c.initCache(), nil
}

// CacheForAdmin は CacheForDecorator と同一インスタンスを返す（memory backend では区別しない）。
func (c *Container) CacheForAdmin() (store.CacheStore, error) {
	return c.initCache(), nil
}

func (c *Container) initCache() *MemoryCacheStore {
	c.cacheOnce.Do(func() {
		c.cache = NewCacheStore()
		c.startCleanup()
	})
	return c.cache
}

// SigningKey は MemorySigningKeyStore を lazy 初期化して返す。
func (c *Container) SigningKey() (store.SigningKeyStore, error) {
	c.signingOnce.Do(func() { c.signing = NewSigningKeyStore() })
	return c.signing, nil
}

// IDProxyStore は idproxy 用 MemoryStore を lazy 初期化して返す。
func (c *Container) IDProxyStore() (idproxy.Store, error) {
	c.idproxyOnce.Do(func() { c.idproxyStore = newIDProxyMemoryStore() })
	return c.idproxyStore, nil
}

// LocationString は memory backend を表す URL 風文字列を返す。
func (c *Container) LocationString() string { return "memory://" }

// Close は cleanup goroutine を停止し、初期化済みの全サブストアを Close する。
// 冪等。エラーは errors.Join で集約して返す。
//
// ctx は外部キャンセル待機に使う。直列 Close なので主には passthrough だが、
// goroutine 停止待機のタイムアウト制御を有効にする。
func (c *Container) Close(ctx context.Context) error {
	var errs []error
	c.closeOnce.Do(func() {
		// 1) cleanup goroutine 停止（起動済みの場合のみ wait）
		c.closeCancel()
		if c.cleanupStarted() && c.cleanupInterval > 0 {
			select {
			case <-c.cleanupDone:
			case <-ctx.Done():
				errs = append(errs, ctx.Err())
			}
		}
		// 2) サブストアを直列 Close（順序: idproxy → tokens → cache → signing）
		if c.idproxyStore != nil {
			if err := c.idproxyStore.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if c.tokens != nil {
			if err := c.tokens.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if c.cache != nil {
			if err := c.cache.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if c.signing != nil {
			if err := c.signing.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	})
	return errors.Join(errs...)
}

// startCleanup は cleanupInterval > 0 のときのみ goroutine を起動する。
// goroutine は closeCtx.Done() で停止し、cleanupDone を close する。
func (c *Container) startCleanup() {
	c.cleanupOnce.Do(func() {
		if c.cleanupInterval <= 0 {
			return
		}
		c.cleanupStartMu.Lock()
		c.cleanupStartFlg = true
		c.cleanupStartMu.Unlock()
		go func() {
			defer close(c.cleanupDone)
			t := time.NewTicker(c.cleanupInterval)
			defer t.Stop()
			for {
				select {
				case <-c.closeCtx.Done():
					return
				case <-t.C:
					if c.cache != nil {
						c.cache.cleanupExpired()
					}
				}
			}
		}()
	})
}

// cleanupStarted は cleanup goroutine が起動済みかどうかを返す。
func (c *Container) cleanupStarted() bool {
	c.cleanupStartMu.Lock()
	defer c.cleanupStartMu.Unlock()
	return c.cleanupStartFlg
}
