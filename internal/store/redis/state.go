package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/youyo/kintone/internal/store"
)

// stateKeyPrefix は OAuth state の Redis キー prefix。
// "kintone:oauthstate:<state>" の hash として格納する。
const stateKeyPrefix = "kintone:oauthstate:"

// takeStateScript は HGETALL + DEL を atomic に行う Lua script。
//
// Redis は単一 Lua script の実行を **atomic** に保証する（クラスタでも単一キー操作なので
// CROSSSLOT エラーは発生しない）。これにより複数 client が同時に Take を呼んでも
// 勝者は最大 1 つになる。
const takeStateScript = `
local v = redis.call('HGETALL', KEYS[1])
if #v == 0 then return v end
redis.call('DEL', KEYS[1])
return v
`

// RedisStateStore は store.StateStore の Redis 実装。
//
// key: "kintone:oauthstate:<state>" の hash に principal_id / verifier / method /
// created_at(ns) を格納する。TTL は EXPIRE で自動失効（DefaultStateTTL）。
// one-shot Take は Lua script で HGETALL + DEL を atomic に実行する。
//
// client は Container が所有する。RedisStateStore.Close は no-op。
type RedisStateStore struct {
	client    goredis.UniversalClient
	ttl       time.Duration
	takeShaMu *takeShaCache
}

// NewStateStore は RedisStateStore を構築する。
func NewStateStore(client goredis.UniversalClient) *RedisStateStore {
	return &RedisStateStore{
		client:    client,
		ttl:       store.DefaultStateTTL,
		takeShaMu: &takeShaCache{},
	}
}

func stateKey(state string) string { return stateKeyPrefix + state }

// Put は entry を Redis hash として保存する。TTL は EXPIRE で自動失効。
func (s *RedisStateStore) Put(ctx context.Context, entry store.StateEntry) error {
	if entry.State == "" {
		return store.ErrStateNotFound
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	method := entry.Method
	if method == "" {
		method = "S256"
	}
	key := stateKey(entry.State)
	fields := map[string]interface{}{
		"principal_id": entry.PrincipalID,
		"verifier":     entry.Verifier,
		"method":       method,
		"created_at":   strconv.FormatInt(entry.CreatedAt.UnixNano(), 10),
	}
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store/redis: state put: %w", err)
	}
	return nil
}

// Take は HGETALL + DEL を Lua script で atomic に実行する。
//
// 並行 Take は Redis が script の atomic 実行を保証するため、winner は最大 1 つ。
// 期限切れ entry は EXPIRE で Redis 側が削除済みなので、空 hash → ErrStateNotFound。
func (s *RedisStateStore) Take(ctx context.Context, state string) (*store.StateEntry, error) {
	if state == "" {
		return nil, store.ErrStateNotFound
	}
	key := stateKey(state)
	res, err := s.client.Eval(ctx, takeStateScript, []string{key}).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, store.ErrStateNotFound
		}
		return nil, fmt.Errorf("store/redis: state take: %w", err)
	}
	arr, ok := res.([]interface{})
	if !ok || len(arr) == 0 {
		return nil, store.ErrStateNotFound
	}
	// HGETALL の戻り値は [k1,v1,k2,v2,...] の交互配列
	m := make(map[string]string, len(arr)/2)
	for i := 0; i+1 < len(arr); i += 2 {
		k, _ := arr[i].(string)
		v, _ := arr[i+1].(string)
		m[k] = v
	}
	entry := &store.StateEntry{
		State:       state,
		PrincipalID: m["principal_id"],
		Verifier:    m["verifier"],
		Method:      m["method"],
	}
	if v := m["created_at"]; v != "" {
		if ns, err := strconv.ParseInt(v, 10, 64); err == nil {
			entry.CreatedAt = time.Unix(0, ns)
		}
	}
	return entry, nil
}

// Close は no-op。実際の client.Close は Container が行う。
func (s *RedisStateStore) Close() error { return nil }

// takeShaCache は将来 Eval → EvalSha に切り替える際の sha キャッシュ用 placeholder。
// 現状は Eval で毎回 send する（10 分 TTL 内に高々数回程度のため性能影響は無視できる）。
type takeShaCache struct{}

// ensure RedisStateStore implements store.StateStore at compile time
var _ store.StateStore = (*RedisStateStore)(nil)
