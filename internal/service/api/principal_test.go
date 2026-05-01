package api

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/store"
)

// fakeFallbackAPI はテスト用の最小 API スタブ。インスタンス比較のみ行う。
type fakeFallbackAPI struct{ API }

// fakeStore は store.TokenStore の最小スタブ。Get の挙動だけ制御する。
type fakeStore struct {
	tokens map[string]*store.Token
}

func newFakeStore() *fakeStore { return &fakeStore{tokens: map[string]*store.Token{}} }

func storeKey(domain, principalID string, t store.AuthType) string {
	return domain + "|" + principalID + "|" + string(t)
}

func (s *fakeStore) Get(_ context.Context, domain, principalID string, t store.AuthType) (*store.Token, error) {
	tok, ok := s.tokens[storeKey(domain, principalID, t)]
	if !ok {
		return nil, store.ErrNotFound
	}
	return tok, nil
}

func (s *fakeStore) Put(_ context.Context, tok store.Token) error {
	s.tokens[storeKey(tok.Domain, tok.PrincipalID, tok.AuthType)] = &tok
	return nil
}

func (s *fakeStore) Delete(_ context.Context, domain, principalID string, t store.AuthType) error {
	delete(s.tokens, storeKey(domain, principalID, t))
	return nil
}

func (s *fakeStore) ListByDomain(_ context.Context, domain string, t store.AuthType) ([]*store.Token, error) {
	out := make([]*store.Token, 0)
	for _, tok := range s.tokens {
		if tok.Domain == domain && tok.AuthType == t {
			cp := *tok
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PrincipalID < out[j].PrincipalID })
	return out, nil
}

func (s *fakeStore) Close() error { return nil }

func baseResolved() *config.Resolved {
	return &config.Resolved{Domain: "example.cybozu.com", Auth: config.AuthModeOAuth}
}

func TestPrincipalAPIFactory_NewErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  PrincipalAPIFactoryConfig
		want string
	}{
		{"nil base", PrincipalAPIFactoryConfig{Mode: AuthZModeAPIToken, Fallback: &fakeFallbackAPI{}}, "base"},
		{"nil fallback", PrincipalAPIFactoryConfig{Base: baseResolved(), Mode: AuthZModeAPIToken}, "fallback"},
		{"unknown mode", PrincipalAPIFactoryConfig{Base: baseResolved(), Mode: "wat", Fallback: &fakeFallbackAPI{}}, "unknown mode"},
		{"oauth without store", PrincipalAPIFactoryConfig{Base: baseResolved(), Mode: AuthZModeOAuth, Fallback: &fakeFallbackAPI{}}, "store required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewPrincipalAPIFactory(tt.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.want)
			}
		})
	}
}

func TestPrincipalAPIFactory_ForContext_APIToken_AlwaysFallback(t *testing.T) {
	t.Parallel()
	fb := &fakeFallbackAPI{}
	f, err := NewPrincipalAPIFactory(PrincipalAPIFactoryConfig{
		Base: baseResolved(), Mode: AuthZModeAPIToken, Fallback: fb,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Principal なしでも fallback
	got, err := f.ForContext(context.Background())
	if err != nil {
		t.Fatalf("ForContext: %v", err)
	}
	if got != fb {
		t.Fatalf("expected fallback API, got %v", got)
	}
	// Principal ありでも fallback
	ctx := idproxy.WithPrincipal(context.Background(), &idproxy.Principal{ID: "i:s"})
	got2, err := f.ForContext(ctx)
	if err != nil {
		t.Fatalf("ForContext with principal: %v", err)
	}
	if got2 != fb {
		t.Fatalf("expected fallback API even with principal, got %v", got2)
	}
}

func TestPrincipalAPIFactory_ForContext_OAuth_NoPrincipal(t *testing.T) {
	t.Parallel()
	fb := &fakeFallbackAPI{}
	f, err := NewPrincipalAPIFactory(PrincipalAPIFactoryConfig{
		Base: baseResolved(), Mode: AuthZModeOAuth, Store: newFakeStore(), Fallback: fb,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = f.ForContext(context.Background())
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("expected ErrAuthRequired, got %v", err)
	}
}

func TestPrincipalAPIFactory_ForContext_OAuth_NoToken(t *testing.T) {
	t.Parallel()
	fb := &fakeFallbackAPI{}
	fs := newFakeStore()
	f, err := NewPrincipalAPIFactory(PrincipalAPIFactoryConfig{
		Base: baseResolved(), Mode: AuthZModeOAuth, Store: fs, Fallback: fb,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := idproxy.WithPrincipal(context.Background(), &idproxy.Principal{ID: "https://i:sub"})
	_, err = f.ForContext(ctx)
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("expected ErrAuthRequired wrapping not found, got %v", err)
	}
}

func TestPrincipalAPIFactory_ForContext_OAuth_BuildsClient(t *testing.T) {
	t.Parallel()
	fb := &fakeFallbackAPI{}
	fs := newFakeStore()
	pid := "https://accounts.google.com:user-1"
	if err := fs.Put(context.Background(), store.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  pid,
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  "at-1",
		RefreshToken: "rt-1",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	f, err := NewPrincipalAPIFactory(PrincipalAPIFactoryConfig{
		Base: baseResolved(), Mode: AuthZModeOAuth, Store: fs, Fallback: fb,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := idproxy.WithPrincipal(context.Background(), &idproxy.Principal{ID: pid, Issuer: "https://accounts.google.com", Subject: "user-1"})
	got, err := f.ForContext(ctx)
	if err != nil {
		t.Fatalf("ForContext: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil API")
	}
	if got == fb {
		t.Fatal("expected new per-principal API, not fallback")
	}
}
