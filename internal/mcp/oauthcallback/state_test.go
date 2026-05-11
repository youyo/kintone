package oauthcallback

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryStateStore_PutTake(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := NewMemoryStateStore()
	defer s.Close() //nolint:errcheck

	entry := StateEntry{
		State:       "state-abc",
		PrincipalID: "https://issuer.example.com:user-1",
		Verifier:    "verifier-xyz",
		Method:      "S256",
	}
	if err := s.Put(ctx, entry); err != nil {
		t.Fatalf("Put err = %v", err)
	}

	got, err := s.Take(ctx, "state-abc")
	if err != nil {
		t.Fatalf("Take err = %v", err)
	}
	if got.State != entry.State || got.PrincipalID != entry.PrincipalID || got.Verifier != entry.Verifier {
		t.Errorf("Take returned unexpected entry: %+v", got)
	}

	// Take は one-shot: 2 回目は ErrStateNotFound
	if _, err := s.Take(ctx, "state-abc"); !errors.Is(err, ErrStateNotFound) {
		t.Errorf("second Take err = %v, want ErrStateNotFound", err)
	}
}

func TestMemoryStateStore_TTLExpire(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{t: now}
	s := NewMemoryStateStore(WithTTL(1*time.Minute), WithNow(clock.Now))
	defer s.Close() //nolint:errcheck

	if err := s.Put(ctx, StateEntry{State: "s1", PrincipalID: "p1", Verifier: "v"}); err != nil {
		t.Fatalf("Put err = %v", err)
	}

	// TTL 内: 取れる
	clock.advance(30 * time.Second)
	// 取り出すと削除されるので再 Put
	if _, err := s.Take(ctx, "s1"); err != nil {
		t.Fatalf("Take within TTL err = %v", err)
	}
	if err := s.Put(ctx, StateEntry{State: "s1", PrincipalID: "p1", Verifier: "v"}); err != nil {
		t.Fatalf("re-Put err = %v", err)
	}

	// TTL 超過: ErrStateNotFound
	clock.advance(2 * time.Minute)
	if _, err := s.Take(ctx, "s1"); !errors.Is(err, ErrStateNotFound) {
		t.Errorf("Take after TTL err = %v, want ErrStateNotFound", err)
	}
}

func TestMemoryStateStore_ConcurrentPutTake(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := NewMemoryStateStore()
	defer s.Close() //nolint:errcheck

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			state := stateKey(idx)
			if err := s.Put(ctx, StateEntry{State: state, PrincipalID: "p", Verifier: "v"}); err != nil {
				t.Errorf("Put %d err = %v", idx, err)
				return
			}
			got, err := s.Take(ctx, state)
			if err != nil {
				t.Errorf("Take %d err = %v", idx, err)
				return
			}
			if got.State != state {
				t.Errorf("Take %d returned %q, want %q", idx, got.State, state)
			}
		}(i)
	}
	wg.Wait()
}

func TestMemoryStateStore_StoresVerifier(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := NewMemoryStateStore()
	defer s.Close() //nolint:errcheck

	if err := s.Put(ctx, StateEntry{State: "s", Verifier: "secret-verifier", Method: "S256", PrincipalID: "p"}); err != nil {
		t.Fatalf("Put err = %v", err)
	}
	got, err := s.Take(ctx, "s")
	if err != nil {
		t.Fatalf("Take err = %v", err)
	}
	if got.Verifier != "secret-verifier" {
		t.Errorf("Verifier = %q, want secret-verifier", got.Verifier)
	}
	if got.Method != "S256" {
		t.Errorf("Method = %q, want S256", got.Method)
	}
}

func TestMemoryStateStore_EmptyStateRejected(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := NewMemoryStateStore()
	defer s.Close() //nolint:errcheck

	if err := s.Put(ctx, StateEntry{State: ""}); err == nil {
		t.Errorf("Put with empty state should error")
	}
	if _, err := s.Take(ctx, ""); !errors.Is(err, ErrStateNotFound) {
		t.Errorf("Take empty err = %v, want ErrStateNotFound", err)
	}
}

func TestMemoryStateStore_CloseIdempotent(t *testing.T) {
	t.Parallel()

	s := NewMemoryStateStore()
	if err := s.Close(); err != nil {
		t.Fatalf("first Close err = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close err = %v", err)
	}
}

func TestMemoryStateStore_GCOnPut(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	s := NewMemoryStateStore(WithTTL(1*time.Minute), WithNow(clock.Now))
	defer s.Close() //nolint:errcheck

	_ = s.Put(ctx, StateEntry{State: "old", PrincipalID: "p", Verifier: "v"})
	clock.advance(2 * time.Minute)
	_ = s.Put(ctx, StateEntry{State: "new", PrincipalID: "p", Verifier: "v"})

	// "old" は GC で削除されているはず
	if _, err := s.Take(ctx, "old"); !errors.Is(err, ErrStateNotFound) {
		t.Errorf("old state should be GCed; Take err = %v", err)
	}
	// "new" は生きている
	if _, err := s.Take(ctx, "new"); err != nil {
		t.Errorf("new state Take err = %v", err)
	}
}

// --- helpers ---

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func stateKey(i int) string {
	const hex = "0123456789abcdef"
	out := []byte("s-0000")
	out[2] = hex[(i>>12)&0xF]
	out[3] = hex[(i>>8)&0xF]
	out[4] = hex[(i>>4)&0xF]
	out[5] = hex[i&0xF]
	return string(out)
}
