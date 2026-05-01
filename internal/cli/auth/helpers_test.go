package auth_test

import (
	"context"
	"testing"

	"github.com/youyo/kintone/internal/store"
	_ "github.com/youyo/kintone/internal/store/memory"
)

// newMemoryContainer は memory backend ベースの Container を返す。
// テスト終了時に Close される。
func newMemoryContainer(t *testing.T) store.Container {
	t.Helper()
	c, err := store.OpenFromConfig(&store.Config{Backend: store.BackendMemory})
	if err != nil {
		t.Fatalf("OpenFromConfig(memory): %v", err)
	}
	t.Cleanup(func() {
		_ = c.Close(context.Background())
	})
	return c
}
