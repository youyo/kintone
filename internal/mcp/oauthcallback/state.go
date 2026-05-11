// Package oauthcallback は MCP サーバホスト型 kintone OAuth callback を提供する。
//
// M13 で導入。kintone OAuth は redirect_uri に HTTPS を強制するため、ローカル CLI の
// loopback http フローは成立しない。代わりに MCP サーバ自身が OAuth client として振る舞い、
// /oauth/kintone/start で authorize URL に誘導し /oauth/kintone/callback で
// authorization_code を token に交換して TokenStore に保存する。
//
// 設計判断:
//   - state map は in-memory + sync.Mutex（M13）。M14 で Redis/DynamoDB に拡張可能な
//     interface（StateStore）を提供する。
//   - state は 10 分 TTL（SSO + MFA を考慮）。Take は one-shot（OAuth 2.0 仕様準拠）。
//   - CSRF は三重保護: idproxy Principal + state cookie + state map の PrincipalID 比較。
package oauthcallback

import (
	"context"
	"errors"
	"sync"
	"time"
)

// DefaultStateTTL は state の有効期限既定値。
const DefaultStateTTL = 10 * time.Minute

// ErrStateNotFound は state map に該当エントリがない（または期限切れ）ときに返される。
var ErrStateNotFound = errors.New("oauthcallback: state not found")

// StateEntry は state ↔ session の対応情報を保持する。
//
// State / PrincipalID / Verifier は CSRF 検証および token exchange で使う。
// CreatedAt は TTL チェック用。
type StateEntry struct {
	State       string // 乱数（base64url）
	PrincipalID string // OIDC principal_id（issuer:subject）
	Verifier    string // PKCE code_verifier（callback で token exchange に渡す）
	Method      string // 通常 "S256"
	CreatedAt   time.Time
}

// StateStore は state ↔ session map の抽象。
//
// M13 では MemoryStateStore のみを提供。M14 で Redis/DynamoDB 実装を追加する設計余地を残す。
type StateStore interface {
	// Put は新しい entry を保存する。同 state の上書きは許容（衝突確率は天文学的に低い）。
	Put(ctx context.Context, entry StateEntry) error
	// Take は state に対応する entry を取り出し、同時に削除する（one-shot semantics）。
	// 期限切れ entry はその場で削除し ErrStateNotFound を返す。
	Take(ctx context.Context, state string) (*StateEntry, error)
	// Close は内部リソースを解放する。冪等。
	Close() error
}

// MemoryStateStore は in-memory な StateStore 実装。
//
// プロセス再起動・multi-replica で state が共有されないことに注意（README で明記）。
// goroutine 安全。Now を差し替えて TTL テスト可能。
type MemoryStateStore struct {
	mu    sync.Mutex
	store map[string]*StateEntry
	ttl   time.Duration
	now   func() time.Time
}

// MemoryStateStoreOption は MemoryStateStore のコンストラクタオプション。
type MemoryStateStoreOption func(*MemoryStateStore)

// WithTTL は TTL を上書きする。
func WithTTL(d time.Duration) MemoryStateStoreOption {
	return func(s *MemoryStateStore) { s.ttl = d }
}

// WithNow は現在時刻関数を差し替える（テスト用）。
func WithNow(now func() time.Time) MemoryStateStoreOption {
	return func(s *MemoryStateStore) { s.now = now }
}

// NewMemoryStateStore は MemoryStateStore を構築する。
func NewMemoryStateStore(opts ...MemoryStateStoreOption) *MemoryStateStore {
	s := &MemoryStateStore{
		store: make(map[string]*StateEntry),
		ttl:   DefaultStateTTL,
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Put は entry を保存する。CreatedAt が zero の場合は Now を埋める。
func (s *MemoryStateStore) Put(_ context.Context, entry StateEntry) error {
	if entry.State == "" {
		return errors.New("oauthcallback: empty state")
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = s.now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 期限切れエントリの GC（put のタイミングで実施、別 goroutine 不要）
	s.gcLocked()
	cp := entry
	s.store[entry.State] = &cp
	return nil
}

// Take は state に対応する entry を取り出し削除する。
func (s *MemoryStateStore) Take(_ context.Context, state string) (*StateEntry, error) {
	if state == "" {
		return nil, ErrStateNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.store[state]
	if !ok {
		return nil, ErrStateNotFound
	}
	delete(s.store, state)
	if s.expired(entry) {
		return nil, ErrStateNotFound
	}
	cp := *entry
	return &cp, nil
}

// Close は内部 map をクリアする。冪等。
func (s *MemoryStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = make(map[string]*StateEntry)
	return nil
}

// expired は entry が期限切れかを判定する。
func (s *MemoryStateStore) expired(entry *StateEntry) bool {
	if s.ttl <= 0 {
		return false
	}
	return s.now().Sub(entry.CreatedAt) > s.ttl
}

// gcLocked は期限切れ entry を削除する。呼び出し側で mu 取得済みであること。
func (s *MemoryStateStore) gcLocked() {
	if s.ttl <= 0 {
		return
	}
	cutoff := s.now().Add(-s.ttl)
	for k, v := range s.store {
		if v.CreatedAt.Before(cutoff) {
			delete(s.store, k)
		}
	}
}
