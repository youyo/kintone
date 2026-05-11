package memory

import (
	"context"
	"sync"
	"time"

	"github.com/youyo/kintone/internal/store"
)

// MemoryStateStore は store.StateStore の in-memory 実装。
//
// process-local map + sync.Mutex で one-shot Take を保証する。
// multi-replica 配置では auth=oidc 環境で memory backend は ErrMemoryOIDCForbidden で
// Container open 時に拒否されるため、本実装は test / single-process 用途のみ。
//
// Now を差し替えて TTL テスト可能。
type MemoryStateStore struct {
	mu    sync.Mutex
	store map[string]*store.StateEntry
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

// NewStateStore は MemoryStateStore を構築する。
func NewStateStore(opts ...MemoryStateStoreOption) *MemoryStateStore {
	s := &MemoryStateStore{
		store: make(map[string]*store.StateEntry),
		ttl:   store.DefaultStateTTL,
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Put は entry を保存する。CreatedAt が zero の場合は now() を埋める。
func (s *MemoryStateStore) Put(_ context.Context, entry store.StateEntry) error {
	if entry.State == "" {
		return store.ErrStateNotFound
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = s.now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	cp := entry
	s.store[entry.State] = &cp
	return nil
}

// Take は state に対応する entry を atomic に取り出し削除する。
func (s *MemoryStateStore) Take(_ context.Context, state string) (*store.StateEntry, error) {
	if state == "" {
		return nil, store.ErrStateNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.store[state]
	if !ok {
		return nil, store.ErrStateNotFound
	}
	delete(s.store, state)
	if s.expired(entry) {
		return nil, store.ErrStateNotFound
	}
	cp := *entry
	return &cp, nil
}

// Close は内部 map をクリアする。冪等。
func (s *MemoryStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = make(map[string]*store.StateEntry)
	return nil
}

// expired は entry が期限切れかを判定する。
func (s *MemoryStateStore) expired(entry *store.StateEntry) bool {
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

// ensure MemoryStateStore implements store.StateStore at compile time
var _ store.StateStore = (*MemoryStateStore)(nil)
