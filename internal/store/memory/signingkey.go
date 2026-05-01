package memory

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"sync"
)

// MemorySigningKeyStore はインメモリの [store.SigningKeyStore] 実装。
//
// 初回 LoadOrCreate 時に ES256（P-256）鍵を新規生成して保持し、
// 以降は同じ鍵を返す。プロセス終了で鍵は失われる（dev / test 専用）。
type MemorySigningKeyStore struct {
	mu  sync.Mutex
	key *ecdsa.PrivateKey
}

// NewSigningKeyStore は空の MemorySigningKeyStore を返す。
func NewSigningKeyStore() *MemorySigningKeyStore {
	return &MemorySigningKeyStore{}
}

// LoadOrCreate は永続鍵をロードする。未保存なら新規生成する。
func (s *MemorySigningKeyStore) LoadOrCreate(_ context.Context) (*ecdsa.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key != nil {
		return s.key, nil
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("memory signing key: generate: %w", err)
	}
	s.key = k
	return s.key, nil
}

// Close は鍵参照をクリアする。冪等。
func (s *MemorySigningKeyStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = nil
	return nil
}
