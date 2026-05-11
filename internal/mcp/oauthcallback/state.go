// Package oauthcallback は MCP サーバホスト型 kintone OAuth callback を提供する。
//
// M13 で導入、M14 で StateStore を internal/store に移設し multi-replica 対応化。
// kintone OAuth は redirect_uri に HTTPS を強制するため、ローカル CLI の loopback
// http フローは成立しない。代わりに MCP サーバ自身が OAuth client として振る舞い、
// /oauth/kintone/start で authorize URL に誘導し /oauth/kintone/callback で
// authorization_code を token に交換して TokenStore に保存する。
//
// 設計判断（M14）:
//   - StateStore interface / StateEntry / ErrStateNotFound は `internal/store` に正準定義し、
//     本パッケージは型エイリアスで再エクスポートする（API 後方互換）。
//   - 物理実装は memory / sqlite / redis / dynamodb の 4 backend に移植され、
//     `Container.StateStore()` 経由で取得する。
//   - state は 10 分 TTL（SSO + MFA を考慮）。Take は one-shot（OAuth 2.0 仕様準拠）。
//   - CSRF は三重保護: idproxy Principal + state cookie + state map の PrincipalID 比較。
package oauthcallback

import (
	"context"
	"time"

	"github.com/youyo/kintone/internal/store"
	memorystore "github.com/youyo/kintone/internal/store/memory"
)

// DefaultStateTTL は state の有効期限既定値（store パッケージから再エクスポート）。
const DefaultStateTTL = store.DefaultStateTTL

// ErrStateNotFound は state map に該当エントリがない（または期限切れ）ときに返される。
// store.ErrStateNotFound のエイリアス。
var ErrStateNotFound = store.ErrStateNotFound

// StateEntry は state ↔ session の対応情報を保持する（store.StateEntry の型エイリアス）。
type StateEntry = store.StateEntry

// StateStore は state ↔ session map の抽象（store.StateStore の型エイリアス）。
type StateStore = store.StateStore

// MemoryStateStore は in-memory な StateStore 実装。
//
// プロセス再起動・multi-replica で state が共有されないことに注意。
// M14 以降、multi-replica の MCP では sqlite / redis / dynamodb backend を使うこと。
// 本型は test / single-process 用途として後方互換のために残されている。
//
// 内部実装は memorystore.MemoryStateStore に委譲する。
type MemoryStateStore struct {
	inner *memorystore.MemoryStateStore
}

// MemoryStateStoreOption は MemoryStateStore のコンストラクタオプション。
type MemoryStateStoreOption func(*memoryStateStoreConfig)

type memoryStateStoreConfig struct {
	ttl time.Duration
	now func() time.Time
}

// WithTTL は TTL を上書きする。
func WithTTL(d time.Duration) MemoryStateStoreOption {
	return func(c *memoryStateStoreConfig) { c.ttl = d }
}

// WithNow は現在時刻関数を差し替える（テスト用）。
func WithNow(now func() time.Time) MemoryStateStoreOption {
	return func(c *memoryStateStoreConfig) { c.now = now }
}

// NewMemoryStateStore は MemoryStateStore を構築する。
//
// option を内部の memorystore.NewStateStore に橋渡しする。
func NewMemoryStateStore(opts ...MemoryStateStoreOption) *MemoryStateStore {
	cfg := &memoryStateStoreConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	var innerOpts []memorystore.MemoryStateStoreOption
	if cfg.ttl != 0 {
		innerOpts = append(innerOpts, memorystore.WithTTL(cfg.ttl))
	}
	if cfg.now != nil {
		innerOpts = append(innerOpts, memorystore.WithNow(cfg.now))
	}
	return &MemoryStateStore{inner: memorystore.NewStateStore(innerOpts...)}
}

// Put は state を保存する。空 state は明示エラーを返す（後方互換のため store.ErrStateNotFound とは別エラー）。
func (s *MemoryStateStore) Put(ctx context.Context, entry StateEntry) error {
	if entry.State == "" {
		return errEmptyState
	}
	return s.inner.Put(ctx, entry)
}

// Take は state を取り出し削除する（one-shot）。
func (s *MemoryStateStore) Take(ctx context.Context, state string) (*StateEntry, error) {
	return s.inner.Take(ctx, state)
}

// Close は内部 map をクリアする。冪等。
func (s *MemoryStateStore) Close() error { return s.inner.Close() }

// errEmptyState は Put に空 state が渡された場合のエラー（M13 互換）。
var errEmptyState = emptyStateError("oauthcallback: empty state")

type emptyStateError string

func (e emptyStateError) Error() string { return string(e) }

// 静的チェック
var _ StateStore = (*MemoryStateStore)(nil)
