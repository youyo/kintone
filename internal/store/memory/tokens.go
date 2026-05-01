// Package memory は store の memory backend 実装を提供する。
//
// 単発 CLI / 単体テスト / dev 用途のみを想定し、auth=oidc では使用禁止。
// Phase 1 で全 sub-store（Tokens / Cache / SigningKey / IDProxy adapter）を実装する。
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/youyo/kintone/internal/store"
)

type tokenKey struct {
	domain      string
	principalID string
	authType    store.AuthType
}

// MemoryTokenStore はインメモリの [store.TokenStore] 実装。
type MemoryTokenStore struct {
	mu sync.RWMutex
	m  map[tokenKey]*store.Token
}

// NewTokenStore は空の MemoryTokenStore を返す。
func NewTokenStore() *MemoryTokenStore {
	return &MemoryTokenStore{m: map[tokenKey]*store.Token{}}
}

// Get は tokenKey に対応する Token のコピーを返す。不在は store.ErrNotFound。
func (s *MemoryTokenStore) Get(_ context.Context, domain, principalID string, t store.AuthType) (*store.Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.m[tokenKey{domain: domain, principalID: principalID, authType: t}]
	if !ok {
		return nil, store.ErrNotFound
	}
	out := *v
	return &out, nil
}

// Put は Token を保存する（ディープコピー）。UpdatedAt が zero なら現在時刻を入れる。
func (s *MemoryTokenStore) Put(_ context.Context, tok store.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tok.UpdatedAt.IsZero() {
		tok.UpdatedAt = time.Now().UTC()
	}
	cp := tok
	s.m[tokenKey{domain: tok.Domain, principalID: tok.PrincipalID, authType: tok.AuthType}] = &cp
	return nil
}

// Delete はキーが一致する Token を削除する。不在は no-op。
func (s *MemoryTokenStore) Delete(_ context.Context, domain, principalID string, t store.AuthType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, tokenKey{domain: domain, principalID: principalID, authType: t})
	return nil
}

// ListByDomain は domain + AuthType に一致する Token を principalID 昇順で返す。
func (s *MemoryTokenStore) ListByDomain(_ context.Context, domain string, t store.AuthType) ([]*store.Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.Token, 0)
	for k, v := range s.m {
		if k.domain == domain && k.authType == t {
			cp := *v
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PrincipalID < out[j].PrincipalID })
	return out, nil
}

// Close は内部マップを解放する。冪等。
func (s *MemoryTokenStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = map[tokenKey]*store.Token{}
	return nil
}
