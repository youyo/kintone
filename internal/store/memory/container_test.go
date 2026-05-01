package memory_test

import (
	"context"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/youyo/kintone/internal/store/memory"
)

func TestContainer_Lifecycle(t *testing.T) {
	defer goleak.VerifyNone(t)
	c := memory.New(0) // cleanup goroutine 起動なし
	if c == nil {
		t.Fatal("New returned nil")
	}
	if loc := c.LocationString(); loc != "memory://" {
		t.Fatalf("LocationString got=%q", loc)
	}

	if tk, err := c.Tokens(); err != nil || tk == nil {
		t.Fatalf("Tokens: %v %v", tk, err)
	}
	if cd, err := c.CacheForDecorator(); err != nil || cd == nil {
		t.Fatalf("CacheForDecorator: %v %v", cd, err)
	}
	if ca, err := c.CacheForAdmin(); err != nil || ca == nil {
		t.Fatalf("CacheForAdmin: %v %v", ca, err)
	}
	if sk, err := c.SigningKey(); err != nil || sk == nil {
		t.Fatalf("SigningKey: %v %v", sk, err)
	}
	if ip, err := c.IDProxyStore(); err != nil || ip == nil {
		t.Fatalf("IDProxyStore: %v %v", ip, err)
	}

	ctx := context.Background()
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close (second) should be idempotent: %v", err)
	}
}

func TestContainer_CacheUnifiedSameInstance(t *testing.T) {
	defer goleak.VerifyNone(t)
	c := memory.New(0)
	defer func() { _ = c.Close(context.Background()) }()

	a, _ := c.CacheForDecorator()
	b, _ := c.CacheForAdmin()
	if a != b {
		t.Fatal("CacheForDecorator and CacheForAdmin should return the same instance for memory backend")
	}
}

func TestContainer_CleanupGoroutineStops(t *testing.T) {
	defer goleak.VerifyNone(t)
	// 短い周期で goroutine を起動し、Close で確実に停止することを確認
	c := memory.New(5 * time.Millisecond)
	if _, err := c.CacheForDecorator(); err != nil {
		t.Fatalf("CacheForDecorator: %v", err)
	}
	// goroutine が tick するのを待つ
	time.Sleep(20 * time.Millisecond)
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
