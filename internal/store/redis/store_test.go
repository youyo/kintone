package redis_test

import (
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/youyo/kintone/internal/store"
	redisstore "github.com/youyo/kintone/internal/store/redis"
	"github.com/youyo/kintone/internal/store/storetest"
)

// startMiniredis は miniredis v2 サーバーを起動して、対応する go-redis client と
// cleanup 関数を返す。
//
// miniredis は実時間で TTL が進まないため、テスト中だけ 1ms 間隔で FastForward する
// goroutine を起動して conformance test の TTL 期限切れ判定を成立させる。goroutine は
// stop で停止する。
func startMiniredis(t *testing.T) (*miniredis.Miniredis, *goredis.Client, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				mr.FastForward(1 * time.Millisecond)
			}
		}
	}()
	stop := func() {
		close(stopCh)
		wg.Wait()
	}
	return mr, client, stop
}

func TestRedisTokenStore_Conformance(t *testing.T) {
	storetest.RunTokenStoreConformance(t, func() (store.TokenStore, func()) {
		mr, client, stop := startMiniredis(t)
		ts := redisstore.NewTokenStore(client)
		return ts, func() {
			stop()
			_ = ts.Close()
			_ = client.Close()
			mr.Close()
		}
	})
}

func TestRedisCacheStore_Conformance(t *testing.T) {
	storetest.RunCacheStoreConformance(t, func() (store.CacheStore, func()) {
		mr, client, stop := startMiniredis(t)
		cs := redisstore.NewCacheStore(client, "redis://"+mr.Addr())
		return cs, func() {
			stop()
			_ = cs.Close()
			_ = client.Close()
			mr.Close()
		}
	})
}

func TestRedisSigningKeyStore_Conformance(t *testing.T) {
	storetest.RunSigningKeyStoreConformance(t, func() (store.SigningKeyStore, func()) {
		mr, client, stop := startMiniredis(t)
		sk := redisstore.NewSigningKeyStore(client)
		return sk, func() {
			stop()
			_ = sk.Close()
			_ = client.Close()
			mr.Close()
		}
	})
}

func TestRedisStateStore_Conformance(t *testing.T) {
	storetest.RunStateStoreConformance(t, func() (store.StateStore, func()) {
		mr, client, stop := startMiniredis(t)
		ss := redisstore.NewStateStore(client)
		return ss, func() {
			stop()
			_ = ss.Close()
			_ = client.Close()
			mr.Close()
		}
	})
}

func TestRedisSigningKeyStore_PersistsAcrossClientRecreate(t *testing.T) {
	mr := miniredis.RunT(t)

	// 1) 初回 client + 鍵生成
	c1 := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	sk1 := redisstore.NewSigningKeyStore(c1)
	k1, err := sk1.LoadOrCreate(t.Context())
	if err != nil {
		t.Fatalf("LoadOrCreate(1): %v", err)
	}
	_ = sk1.Close()
	_ = c1.Close()

	// 2) 同 miniredis に再接続し、同じ鍵が返ることを確認
	c2 := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer func() { _ = c2.Close() }()
	sk2 := redisstore.NewSigningKeyStore(c2)
	defer func() { _ = sk2.Close() }()
	k2, err := sk2.LoadOrCreate(t.Context())
	if err != nil {
		t.Fatalf("LoadOrCreate(2): %v", err)
	}
	if !k1.PublicKey.Equal(&k2.PublicKey) {
		t.Fatal("signing key did not persist across client recreate")
	}
}
