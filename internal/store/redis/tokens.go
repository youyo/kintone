package redis

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/youyo/kintone/internal/store"
)

// tokenKeyPrefix は kintone Token store の Redis キー prefix。
// "kintone:tokens:" 配下に "kintone:tokens:{domain}:{principalID}:{authType}" の形で格納する。
const tokenKeyPrefix = "kintone:tokens:"

// scanCount は SCAN の COUNT オプションのバッチサイズ。
const scanCount = 100

// RedisTokenStore は [store.TokenStore] の Redis 実装。
//
// 各 Token は HSET で 1 hash として保存する（domain / principal_id / auth_type /
// api_token / access_token / refresh_token / expires_at / updated_at）。
//
// client は Container が所有する。RedisTokenStore.Close は no-op。
type RedisTokenStore struct {
	client goredis.UniversalClient
}

// NewTokenStore は RedisTokenStore を構築する。client は呼び出し側 (Container) が所有する。
func NewTokenStore(client goredis.UniversalClient) *RedisTokenStore {
	return &RedisTokenStore{client: client}
}

func tokenKey(domain, principalID string, t store.AuthType) string {
	return tokenKeyPrefix + domain + ":" + principalID + ":" + string(t)
}

// Get はキーに対応する Token を返す。不在は store.ErrNotFound。
func (s *RedisTokenStore) Get(ctx context.Context, domain, principalID string, t store.AuthType) (*store.Token, error) {
	key := tokenKey(domain, principalID, t)
	m, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("store/redis: tokens get %s: %w", key, err)
	}
	if len(m) == 0 {
		return nil, store.ErrNotFound
	}
	return decodeToken(m), nil
}

// Put は Token を保存する。UpdatedAt が zero のときは現在時刻を自動設定する。
func (s *RedisTokenStore) Put(ctx context.Context, tok store.Token) error {
	updatedAt := tok.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	var expiresAtSec int64
	if !tok.ExpiresAt.IsZero() {
		expiresAtSec = tok.ExpiresAt.Unix()
	}
	fields := map[string]interface{}{
		"domain":        tok.Domain,
		"principal_id":  tok.PrincipalID,
		"auth_type":     string(tok.AuthType),
		"api_token":     tok.APIToken,
		"access_token":  tok.AccessToken,
		"refresh_token": tok.RefreshToken,
		"expires_at":    expiresAtSec,
		"updated_at":    updatedAt.Unix(),
	}
	key := tokenKey(tok.Domain, tok.PrincipalID, tok.AuthType)
	if err := s.client.HSet(ctx, key, fields).Err(); err != nil {
		return fmt.Errorf("store/redis: tokens put %s: %w", key, err)
	}
	return nil
}

// Delete は単一 Token を削除する。不在は no-op。
func (s *RedisTokenStore) Delete(ctx context.Context, domain, principalID string, t store.AuthType) error {
	key := tokenKey(domain, principalID, t)
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("store/redis: tokens delete %s: %w", key, err)
	}
	return nil
}

// ListByDomain は domain + AuthType に一致する Token を principalID 昇順で返す。
//
// 実装: SCAN MATCH "kintone:tokens:{domain}:*" でキー列挙 → HGETALL で各 hash 取得
// → AuthType filter → principalID で sort。
func (s *RedisTokenStore) ListByDomain(ctx context.Context, domain string, t store.AuthType) ([]*store.Token, error) {
	pattern := tokenKeyPrefix + domain + ":*"
	var (
		cursor uint64
		keys   []string
	)
	for {
		batch, next, err := s.client.Scan(ctx, cursor, pattern, scanCount).Result()
		if err != nil {
			return nil, fmt.Errorf("store/redis: tokens scan: %w", err)
		}
		keys = append(keys, batch...)
		if next == 0 {
			break
		}
		cursor = next
	}

	var result []*store.Token
	for _, k := range keys {
		m, err := s.client.HGetAll(ctx, k).Result()
		if err != nil {
			return nil, fmt.Errorf("store/redis: tokens hgetall %s: %w", k, err)
		}
		if len(m) == 0 {
			continue
		}
		if m["auth_type"] != string(t) {
			continue
		}
		result = append(result, decodeToken(m))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PrincipalID < result[j].PrincipalID
	})
	return result, nil
}

// Close は no-op。実際の client.Close は Container が行う。
func (s *RedisTokenStore) Close() error { return nil }

// decodeToken は HGETALL の結果 map から store.Token を組み立てる。
func decodeToken(m map[string]string) *store.Token {
	tok := &store.Token{
		Domain:       m["domain"],
		PrincipalID:  m["principal_id"],
		AuthType:     store.AuthType(m["auth_type"]),
		APIToken:     m["api_token"],
		AccessToken:  m["access_token"],
		RefreshToken: m["refresh_token"],
	}
	if v := m["expires_at"]; v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil && sec > 0 {
			tok.ExpiresAt = time.Unix(sec, 0).UTC()
		}
	}
	if v := m["updated_at"]; v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
			tok.UpdatedAt = time.Unix(sec, 0).UTC()
		}
	}
	return tok
}

// ensure RedisTokenStore implements store.TokenStore at compile time
var _ store.TokenStore = (*RedisTokenStore)(nil)
