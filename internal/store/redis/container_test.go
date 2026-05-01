package redis_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/goleak"

	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/store"
	redisstore "github.com/youyo/kintone/internal/store/redis"
)

func TestContainer_LocationStringAndCloseIdempotent(t *testing.T) {
	// goleak は LIFO で defer が走るので、最初に登録 → 最後に検査される
	defer goleak.VerifyNone(t)
	mr := miniredis.RunT(t)
	defer mr.Close()
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	c := redisstore.NewContainer(client, "redis://"+mr.Addr()+"/0")

	if loc := c.LocationString(); !strings.HasPrefix(loc, "redis://") {
		t.Fatalf("LocationString = %q, want redis:// prefix", loc)
	}

	// Tokens にアクセスして lazy init をトリガー
	if _, err := c.Tokens(); err != nil {
		t.Fatalf("Tokens: %v", err)
	}

	ctx := context.Background()
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close (second) should be idempotent: %v", err)
	}
}

func TestContainer_LocationSanitizesUserinfo(t *testing.T) {
	defer goleak.VerifyNone(t)
	mr := miniredis.RunT(t)
	defer mr.Close()
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	c := redisstore.NewContainer(client, "redis://user:secret@"+mr.Addr()+"/0")
	defer func() { _ = c.Close(context.Background()) }()

	loc := c.LocationString()
	if strings.Contains(loc, "secret") {
		t.Fatalf("LocationString must not contain plaintext password: %q", loc)
	}
	if !strings.Contains(loc, "user:***") {
		t.Fatalf("LocationString should mask password to ***, got: %q", loc)
	}
}

func TestOpen_RedissTLSDowngradeForbidden_LogsWarning(t *testing.T) {
	// rediss:// + KINTONE_STORE_REDIS_TLS=0 は TLS 維持 + WARN ログを出す
	// 実際の TLS 接続はせず、URL parse / WARN ログ発行のみ確認するため Ping は失敗させる前に
	// パース段階で TLS 設定がされていることを確認するのが目的。ただし Open は Ping 必須。
	// よってここではログ取り込みを差し替えて、ログメッセージのみ検証する。
	var buf bytes.Buffer
	restore := output.SetForTest(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer restore()

	// ここでは Open を直接呼ばず、scheme + RedisTLS=false の組み合わせだけ確認する
	// Open の TLS 設定は Ping 失敗で終わるが、その前に WARN ログは出る
	cfg := &store.Config{
		Backend:  store.BackendRedis,
		RedisURL: "rediss://127.0.0.1:1/0",
		RedisTLS: false,
	}
	_, _ = redisstore.Open(cfg)
	if !strings.Contains(buf.String(), "ignoring KINTONE_STORE_REDIS_TLS=0") {
		t.Fatalf("expected WARN log about ignoring TLS=0 for rediss://, got: %q", buf.String())
	}
}

func TestOpen_RedisPlaintextNonLoopbackForbidden(t *testing.T) {
	cfg := &store.Config{
		Backend:                store.BackendRedis,
		RedisURL:               "redis://203.0.113.5:6379/0",
		RedisTLS:               false,
		RedisInsecurePlaintext: false,
	}
	_, err := redisstore.Open(cfg)
	if !errors.Is(err, store.ErrPlaintextForbidden) {
		t.Fatalf("Open: want ErrPlaintextForbidden, got %v", err)
	}
}

func TestOpen_RedisPlaintextLoopbackOK(t *testing.T) {
	defer goleak.VerifyNone(t)
	mr := miniredis.RunT(t)
	defer mr.Close()
	cfg := &store.Config{
		Backend:  store.BackendRedis,
		RedisURL: "redis://" + mr.Addr() + "/0",
		RedisTLS: false,
	}
	c, err := redisstore.Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if _, err := c.Tokens(); err != nil {
		t.Fatalf("Tokens: %v", err)
	}
}

func TestOpen_RedisPlaintextNonLoopbackOptIn(t *testing.T) {
	// INSECURE_PLAINTEXT=1 を渡すと non-loopback でも reject はされない（ただし実 IP に
	// 接続できないので Ping で ErrConnectionFailed）
	cfg := &store.Config{
		Backend:                store.BackendRedis,
		RedisURL:               "redis://203.0.113.5:6379/0",
		RedisTLS:               false,
		RedisInsecurePlaintext: true,
	}
	_, err := redisstore.Open(cfg)
	if errors.Is(err, store.ErrPlaintextForbidden) {
		t.Fatalf("opt-in should bypass ErrPlaintextForbidden, got %v", err)
	}
	// 実 IP に接続できないので ErrConnectionFailed が返る想定だが、サンドボックス次第で
	// timeout 形態が異なるためエラー型までは断定しない。Open が成功 (nil) しないことのみ確認。
	if err == nil {
		t.Fatal("Open against unreachable address should fail (we only verify it does not return ErrPlaintextForbidden)")
	}
}

func TestOpen_RedisPing_Unreachable_ReturnsConnectionFailed(t *testing.T) {
	cfg := &store.Config{
		Backend:  store.BackendRedis,
		RedisURL: "redis://127.0.0.1:1/0", // ポート 1 は基本到達不可
	}
	_, err := redisstore.Open(cfg)
	if !errors.Is(err, store.ErrConnectionFailed) {
		t.Fatalf("Open: want ErrConnectionFailed, got %v", err)
	}
}
