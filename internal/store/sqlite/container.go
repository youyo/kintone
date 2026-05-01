package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	idproxy "github.com/youyo/idproxy"

	"github.com/youyo/kintone/internal/store"
)

// Container は SQLite backend の [store.Container] 実装。
//
// kintone.db に Tokens / Cache / SigningKey の 3 サブストアを格納し、
// idproxy.db を別 *sql.DB として lazy 初期化する。
//
// 全サブストアは sync.Once で lazy 初期化される。Close は冪等で、
// IDProxyStore → kintone *sql.DB の順で逆依存順クローズする。
type Container struct {
	dir         string
	kintonePath string
	idproxyPath string

	closeOnce sync.Once

	dbOnce sync.Once
	db     *sql.DB
	dbErr  error

	tokensOnce sync.Once
	tokens     *SQLiteTokenStore
	tokensErr  error

	cacheOnce sync.Once
	cache     *SQLiteCacheStore
	cacheErr  error

	signingOnce sync.Once
	signing     *SQLiteSigningKeyStore
	signingErr  error

	idproxyOnce  sync.Once
	idproxyStore idproxy.Store
	idproxyErr   error
}

// NewContainer は dir に kintone.db / idproxy.db を配置する Container を構築する。
// dir 自体の作成は OpenDB の親ディレクトリ作成ロジックに委ねる。
//
// この時点では DB を開かず、各アクセサ呼び出し時に lazy で open する。
func NewContainer(dir string) (*Container, error) {
	if dir == "" {
		return nil, errors.New("store/sqlite: empty dir")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: abs %s: %w", dir, err)
	}
	return &Container{
		dir:         abs,
		kintonePath: filepath.Join(abs, "kintone.db"),
		idproxyPath: filepath.Join(abs, "idproxy.db"),
	}, nil
}

// initDB は kintone.db を lazy に開いてキャッシュする。
func (c *Container) initDB() (*sql.DB, error) {
	c.dbOnce.Do(func() {
		db, err := OpenDB(context.Background(), c.kintonePath)
		if err != nil {
			c.dbErr = err
			return
		}
		c.db = db
	})
	return c.db, c.dbErr
}

// Tokens は SQLiteTokenStore を lazy 初期化して返す。
func (c *Container) Tokens() (store.TokenStore, error) {
	c.tokensOnce.Do(func() {
		db, err := c.initDB()
		if err != nil {
			c.tokensErr = err
			return
		}
		c.tokens = NewTokenStore(db)
	})
	if c.tokensErr != nil {
		return nil, c.tokensErr
	}
	return c.tokens, nil
}

// CacheForDecorator は SQLiteCacheStore を lazy 初期化して返す。
func (c *Container) CacheForDecorator() (store.CacheStore, error) {
	return c.initCache()
}

// CacheForAdmin は CacheForDecorator と同一インスタンスを返す（SQLite では分離不要）。
func (c *Container) CacheForAdmin() (store.CacheStore, error) {
	return c.initCache()
}

func (c *Container) initCache() (*SQLiteCacheStore, error) {
	c.cacheOnce.Do(func() {
		db, err := c.initDB()
		if err != nil {
			c.cacheErr = err
			return
		}
		c.cache = NewCacheStore(db, c.kintonePath)
	})
	if c.cacheErr != nil {
		return nil, c.cacheErr
	}
	return c.cache, nil
}

// SigningKey は SQLiteSigningKeyStore を lazy 初期化して返す。
func (c *Container) SigningKey() (store.SigningKeyStore, error) {
	c.signingOnce.Do(func() {
		db, err := c.initDB()
		if err != nil {
			c.signingErr = err
			return
		}
		c.signing = NewSigningKeyStore(db)
	})
	if c.signingErr != nil {
		return nil, c.signingErr
	}
	return c.signing, nil
}

// IDProxyStore は idproxy 用 SQLite store を lazy 初期化して返す。
//
// 初回呼び出し時のみ idproxy.db ファイルを作成する。`kintone auth login` のような
// idproxy.db を必要としない経路ではファイルを生成しない。
func (c *Container) IDProxyStore() (idproxy.Store, error) {
	c.idproxyOnce.Do(func() {
		s, err := newIDProxyStore(c.idproxyPath)
		if err != nil {
			c.idproxyErr = err
			return
		}
		c.idproxyStore = s
	})
	if c.idproxyErr != nil {
		return nil, c.idproxyErr
	}
	return c.idproxyStore, nil
}

// LocationString は backend の保存場所を表す URL 風文字列を返す。
//
// ディレクトリ単位の表記とすることで、kintone.db / idproxy.db 双方を含むことを示す。
func (c *Container) LocationString() string {
	return "sqlite:///" + c.dir
}

// Close は idproxy → kintone *sql.DB の順で逆依存順クローズする。冪等。
func (c *Container) Close(_ context.Context) error {
	var errs []error
	c.closeOnce.Do(func() {
		// 1) idproxy store (Phase 3 で使う、ただし lazy 未 init なら skip)
		if c.idproxyStore != nil {
			if err := c.idproxyStore.Close(); err != nil {
				errs = append(errs, fmt.Errorf("store/sqlite: close idproxy: %w", err))
			}
		}
		// 2) kintone *sql.DB（sub-store の Close は no-op だが順序整合性のため呼ぶ）
		if c.tokens != nil {
			_ = c.tokens.Close()
		}
		if c.cache != nil {
			_ = c.cache.Close()
		}
		if c.signing != nil {
			_ = c.signing.Close()
		}
		if c.db != nil {
			if err := c.db.Close(); err != nil {
				errs = append(errs, fmt.Errorf("store/sqlite: close db: %w", err))
			}
		}
	})
	return errors.Join(errs...)
}
