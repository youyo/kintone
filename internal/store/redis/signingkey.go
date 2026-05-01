package redis

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"

	goredis "github.com/redis/go-redis/v9"

	"github.com/youyo/kintone/internal/store"
)

// signingKeyKey は kintone SigningKey の Redis キー。
// 単一鍵運用のため固定。Phase 7+ でローテーション API を別途追加予定。
const signingKeyKey = "kintone:signingkey:current"

// RedisSigningKeyStore は [store.SigningKeyStore] の Redis 実装。
//
// 永続化形式: PKCS#8 PEM ("PRIVATE KEY" ブロック)。
// client は Container が所有する。RedisSigningKeyStore.Close は no-op。
type RedisSigningKeyStore struct {
	client goredis.UniversalClient
	mu     sync.Mutex
	cache  *ecdsa.PrivateKey
}

// NewSigningKeyStore は RedisSigningKeyStore を構築する。client は呼び出し側 (Container) が所有する。
func NewSigningKeyStore(client goredis.UniversalClient) *RedisSigningKeyStore {
	return &RedisSigningKeyStore{client: client}
}

// LoadOrCreate は永続鍵をロードする。未保存なら ES256 (P-256) 鍵を新規生成して保存する。
//
// 同一 Store 内では同じ *ecdsa.PrivateKey を返す（プロセス内 cache）。
// 並行呼び出しは sync.Mutex で直列化される。
func (s *RedisSigningKeyStore) LoadOrCreate(ctx context.Context) (*ecdsa.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil {
		return s.cache, nil
	}

	// 既存値を試す
	got, err := s.client.Get(ctx, signingKeyKey).Result()
	switch {
	case err == nil:
		key, perr := parsePEM(got)
		if perr != nil {
			return nil, fmt.Errorf("store/redis: signing key parse: %w", perr)
		}
		s.cache = key
		return key, nil
	case errors.Is(err, goredis.Nil):
		// fall through: 新規生成
	default:
		return nil, fmt.Errorf("store/redis: signing key get: %w", err)
	}

	// 新規生成
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("store/redis: signing key generate: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("store/redis: signing key marshal: %w", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	// SET NX で race を避ける（同時起動が両方生成しても先勝ち優先）
	ok, err := s.client.SetNX(ctx, signingKeyKey, pemStr, 0).Result()
	if err != nil {
		return nil, fmt.Errorf("store/redis: signing key setnx: %w", err)
	}
	if !ok {
		// race: 先行プロセスが書いた値を採用
		got, err := s.client.Get(ctx, signingKeyKey).Result()
		if err != nil {
			return nil, fmt.Errorf("store/redis: signing key reget: %w", err)
		}
		persisted, err := parsePEM(got)
		if err != nil {
			return nil, fmt.Errorf("store/redis: signing key parse persisted: %w", err)
		}
		s.cache = persisted
		return persisted, nil
	}
	s.cache = key
	return key, nil
}

// Close は no-op。実際の client.Close は Container が行う。
func (s *RedisSigningKeyStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = nil
	return nil
}

// parsePEM は PEM 文字列から *ecdsa.PrivateKey を取り出す。
// PKCS#8 (PRIVATE KEY) を一次、PKCS#1 (EC PRIVATE KEY) をフォールバックで試す。
func parsePEM(pemStr string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("invalid PEM block")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not ECDSA: %T", k)
		}
		return ec, nil
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, errors.New("PEM is neither PKCS#8 nor SEC1 EC private key")
}

// ensure RedisSigningKeyStore implements store.SigningKeyStore at compile time
var _ store.SigningKeyStore = (*RedisSigningKeyStore)(nil)
