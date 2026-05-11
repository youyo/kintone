package storetest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/youyo/kintone/internal/store"
)

// RunStateStoreConformance は StateStore の最小契約を検証する。
//
// makeStore は新しい空の StateStore と、テスト終了時に呼ばれる cleanup を返す。
// 各 backend の `store_test.go` から呼ぶこと。
func RunStateStoreConformance(t *testing.T, makeStore func() (store.StateStore, func())) {
	t.Helper()

	t.Run("Put_Take_Roundtrip", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		entry := store.StateEntry{
			State:       "state-roundtrip",
			PrincipalID: "oidc:user1",
			Verifier:    "verifier-xyz",
			Method:      "S256",
		}
		if err := s.Put(ctx, entry); err != nil {
			t.Fatalf("Put: %v", err)
		}
		got, err := s.Take(ctx, "state-roundtrip")
		if err != nil {
			t.Fatalf("Take: %v", err)
		}
		if got.PrincipalID != entry.PrincipalID || got.Verifier != entry.Verifier || got.Method != entry.Method {
			t.Fatalf("Take returned wrong entry: %+v", got)
		}
		// one-shot: 2 回目は ErrStateNotFound
		if _, err := s.Take(ctx, "state-roundtrip"); !errors.Is(err, store.ErrStateNotFound) {
			t.Fatalf("second Take want ErrStateNotFound got %v", err)
		}
	})

	t.Run("TakeMissing", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		if _, err := s.Take(ctx, "no-such-state"); !errors.Is(err, store.ErrStateNotFound) {
			t.Fatalf("missing Take want ErrStateNotFound got %v", err)
		}
	})

	t.Run("EmptyStateRejected", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		if _, err := s.Take(ctx, ""); !errors.Is(err, store.ErrStateNotFound) {
			t.Fatalf("empty state Take want ErrStateNotFound got %v", err)
		}
	})

	t.Run("ConcurrentTake_OneWinner", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		if err := s.Put(ctx, store.StateEntry{
			State:       "race",
			PrincipalID: "oidc:race",
			Verifier:    "v",
			Method:      "S256",
		}); err != nil {
			t.Fatalf("Put: %v", err)
		}
		const N = 20
		var wg sync.WaitGroup
		var wins int64
		var misses int64
		start := make(chan struct{})
		for i := 0; i < N; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				got, err := s.Take(ctx, "race")
				switch {
				case err == nil && got != nil:
					atomic.AddInt64(&wins, 1)
				case errors.Is(err, store.ErrStateNotFound):
					atomic.AddInt64(&misses, 1)
				default:
					t.Errorf("unexpected err: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if wins != 1 {
			t.Fatalf("ConcurrentTake winners got=%d want=1 (misses=%d)", wins, misses)
		}
		if misses != N-1 {
			t.Fatalf("ConcurrentTake misses got=%d want=%d", misses, N-1)
		}
	})

	t.Run("CloseIdempotent", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		if err := s.Close(); err != nil {
			t.Fatalf("Close(1): %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close(2) should be idempotent: %v", err)
		}
	})
}
