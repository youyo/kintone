package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/store"
	// memory backend を register するため blank import。
	_ "github.com/youyo/kintone/internal/store/memory"
)

func TestOpenFromConfig_Memory(t *testing.T) {
	c, err := store.OpenFromConfig(&store.Config{Backend: store.BackendMemory})
	if err != nil {
		t.Fatalf("OpenFromConfig: %v", err)
	}
	if c == nil {
		t.Fatal("Container should be non-nil")
	}
	if loc := c.LocationString(); loc != "memory://" {
		t.Errorf("LocationString got=%q want=%q", loc, "memory://")
	}
	ctx := context.Background()
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// 冪等
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close (second time) should be idempotent: %v", err)
	}
}

func TestOpenFromConfig_UnsupportedBackends(t *testing.T) {
	cases := []string{store.BackendSQLite, store.BackendRedis, store.BackendDynamoDB, "unknown-xyz", ""}
	for _, b := range cases {
		b := b
		t.Run(b, func(t *testing.T) {
			_, err := store.OpenFromConfig(&store.Config{Backend: b})
			if !errors.Is(err, store.ErrUnsupportedBackend) {
				t.Fatalf("Backend=%q: want ErrUnsupportedBackend, got %v", b, err)
			}
		})
	}
}

func TestOpenFromConfig_NilCfg(t *testing.T) {
	_, err := store.OpenFromConfig(nil)
	if err == nil {
		t.Fatal("nil cfg should error")
	}
}
