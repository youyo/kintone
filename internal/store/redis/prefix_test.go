package redis_test

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	idproxy "github.com/youyo/idproxy"

	"github.com/youyo/kintone/internal/store"
	redisstore "github.com/youyo/kintone/internal/store/redis"
)

// TestPrefixSeparation_KintoneAndIDProxy は kintone と idproxy が同一 Redis に書いても
// "kintone:" / "idproxy:" の 2 prefix で分離され、キー衝突がないことを確認する。
func TestPrefixSeparation_KintoneAndIDProxy(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	c := redisstore.NewContainer(client, "redis://"+mr.Addr())
	defer func() { _ = c.Close(context.Background()) }()

	ctx := context.Background()

	// kintone 側に書く
	cs, err := c.CacheForDecorator()
	if err != nil {
		t.Fatalf("CacheForDecorator: %v", err)
	}
	if err := cs.Put(ctx, "v1:app:1", []byte(`{"a":1}`), time.Minute); err != nil {
		t.Fatalf("cache put: %v", err)
	}

	tokens, err := c.Tokens()
	if err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	if err := tokens.Put(ctx, store.Token{
		Domain:      "example.cybozu.com",
		PrincipalID: "oauth:user1",
		AuthType:    store.AuthTypeOAuth,
		AccessToken: "at-1",
	}); err != nil {
		t.Fatalf("tokens put: %v", err)
	}

	if _, err := c.SigningKey(); err != nil {
		t.Fatalf("SigningKey: %v", err)
	}

	// idproxy 側に書く（adapter 経由）
	idp, err := c.IDProxyStore()
	if err != nil {
		t.Fatalf("IDProxyStore: %v", err)
	}
	if err := idp.SetSession(ctx, "sess-1", &idproxy.Session{
		ID:        "sess-1",
		IDToken:   "idtoken-xyz",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}, time.Hour); err != nil {
		t.Fatalf("SetSession: %v", err)
	}

	// 全キーを取得して prefix 分布を確認
	allKeys, err := client.Keys(ctx, "*").Result()
	if err != nil {
		t.Fatalf("KEYS *: %v", err)
	}
	sort.Strings(allKeys)
	if len(allKeys) == 0 {
		t.Fatal("expected at least one key after writes")
	}

	prefixes := map[string]int{}
	for _, k := range allKeys {
		switch {
		case strings.HasPrefix(k, "kintone:"):
			prefixes["kintone"]++
		case strings.HasPrefix(k, "idproxy:"):
			prefixes["idproxy"]++
		default:
			t.Errorf("unexpected key prefix: %q", k)
		}
	}
	if prefixes["kintone"] == 0 {
		t.Errorf("no kintone:* keys found, allKeys=%v", allKeys)
	}
	if prefixes["idproxy"] == 0 {
		t.Errorf("no idproxy:* keys found, allKeys=%v", allKeys)
	}
}

// TestIDProxyAdapter_CloseDoesNotCloseClient は idproxy adapter の Close を呼んでも
// 共有 client が閉じられないことを検証する。
func TestIDProxyAdapter_CloseDoesNotCloseClient(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	c := redisstore.NewContainer(client, "redis://"+mr.Addr())
	idp, err := c.IDProxyStore()
	if err != nil {
		t.Fatalf("IDProxyStore: %v", err)
	}

	// adapter の Close
	if err := idp.Close(); err != nil {
		t.Fatalf("idproxy adapter Close: %v", err)
	}

	// client がまだ生きているはず
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("client must remain usable after idproxy adapter Close: %v", err)
	}

	// Container.Close は client も閉じる
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Container Close: %v", err)
	}
	// Container.Close 後は client.Close 済み（go-redis は閉じた client にも Ping が走るが、
	// closed エラーになる）
	if err := client.Ping(context.Background()).Err(); err == nil {
		t.Fatal("client should be closed after Container.Close")
	}
}
