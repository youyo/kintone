package oauth_test

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth"
	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/store"
)

// コンパイル時に oauth.Authenticator が auth.Authenticator を実装することを確認。
var _ auth.Authenticator = (*oauth.Authenticator)(nil)

// mockStore はテスト用の in-memory store.TokenStore。
type mockStore struct {
	mu     sync.Mutex
	tokens map[string]*store.Token
	getErr error
	putErr error
}

func newMockStore() *mockStore {
	return &mockStore{tokens: make(map[string]*store.Token)}
}

func (m *mockStore) key(domain, principalID string, t store.AuthType) string {
	return domain + "|" + principalID + "|" + string(t)
}

func (m *mockStore) Get(_ context.Context, domain, principalID string, t store.AuthType) (*store.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	tok, ok := m.tokens[m.key(domain, principalID, t)]
	if !ok {
		return nil, store.ErrNotFound
	}
	// コピーを返す
	cp := *tok
	return &cp, nil
}

func (m *mockStore) Put(_ context.Context, tok store.Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return m.putErr
	}
	cp := tok
	m.tokens[m.key(tok.Domain, tok.PrincipalID, tok.AuthType)] = &cp
	return nil
}

func (m *mockStore) Delete(_ context.Context, domain, principalID string, t store.AuthType) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, m.key(domain, principalID, t))
	return nil
}

func (m *mockStore) ListByDomain(_ context.Context, domain string, t store.AuthType) ([]*store.Token, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.Token, 0)
	for _, tok := range m.tokens {
		if tok.Domain == domain && tok.AuthType == t {
			cp := *tok
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PrincipalID < out[j].PrincipalID })
	return out, nil
}

func (m *mockStore) Close() error { return nil }

// mockRefresher はテスト用 refresh mock。
type mockRefresher struct {
	mu        sync.Mutex
	callCount int
	result    *oauth.Result
	err       error
}

func (mr *mockRefresher) Refresh(_ context.Context, _ string) (*oauth.Result, error) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.callCount++
	return mr.result, mr.err
}

// PR-1: access_token 有効 → req に Bearer ヘッダ付与
func TestAuthenticator_Apply_ValidToken(t *testing.T) {
	t.Parallel()
	ts := newMockStore()
	future := time.Now().Add(1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain:      "example.cybozu.com",
		PrincipalID: "oauth:alice",
		AuthType:    store.AuthTypeOAuth,
		AccessToken: "valid-access-token",
		ExpiresAt:   future,
	})

	a := oauth.NewAuthenticator(ts, "example.cybozu.com", "oauth:alice", nil, nil)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.cybozu.com/k/v1/records.json", nil)
	if err := a.Apply(t.Context(), req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer valid-access-token" {
		t.Errorf("Authorization: got %q, want %q", got, "Bearer valid-access-token")
	}
}

// PR-2: 期限切れ → refresh が呼ばれて TokenStore 更新 → Bearer ヘッダ
func TestAuthenticator_Apply_ExpiredToken_Refresh(t *testing.T) {
	t.Parallel()
	ts := newMockStore()
	past := time.Now().Add(-1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  "oauth:alice",
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  "expired-access-token",
		RefreshToken: "my-refresh-token",
		ExpiresAt:    past,
	})

	mr := &mockRefresher{
		result: &oauth.Result{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
		},
	}
	a := oauth.NewAuthenticator(ts, "example.cybozu.com", "oauth:alice", mr, nil)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.cybozu.com/k/v1/records.json", nil)
	if err := a.Apply(t.Context(), req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer new-access-token" {
		t.Errorf("Authorization: got %q", got)
	}
	// TokenStore が更新されていること
	stored, _ := ts.Get(t.Context(), "example.cybozu.com", "oauth:alice", store.AuthTypeOAuth)
	if stored.AccessToken != "new-access-token" {
		t.Errorf("stored access_token: got %q", stored.AccessToken)
	}
}

// PR-3: 並行 Apply → refresh は 1 回のみ（mutex 動作）
func TestAuthenticator_Apply_ConcurrentRefresh(t *testing.T) {
	t.Parallel()
	ts := newMockStore()
	past := time.Now().Add(-1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  "oauth:alice",
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  "expired-access-token",
		RefreshToken: "my-refresh-token",
		ExpiresAt:    past,
	})

	var refreshCallCount atomic.Int32
	mr := &mockRefresher{}
	mr.result = &oauth.Result{
		AccessToken:  "new-access-token",
		RefreshToken: "new-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
	// Refresh 呼び出し時にカウント（mockRefresher は sync.Mutex を持つ）
	origRefresh := mr.Refresh
	_ = origRefresh // このフィールドは存在しないので、countRefresher ラッパーを使う

	countRefresher := &countingRefresher{
		count: &refreshCallCount,
		result: &oauth.Result{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
		},
	}

	a := oauth.NewAuthenticator(ts, "example.cybozu.com", "oauth:alice", countRefresher, nil)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.cybozu.com/k/v1/records.json", nil)
			_ = a.Apply(t.Context(), req)
		}()
	}
	wg.Wait()

	// refresh は 1 回のみ（mutex によって保護される）
	if count := refreshCallCount.Load(); count != 1 {
		t.Errorf("refresh called %d times, want 1", count)
	}
}

// countingRefresher は Refresh 呼び出しをカウントするテスト用型。
type countingRefresher struct {
	count  *atomic.Int32
	result *oauth.Result
	err    error
}

func (c *countingRefresher) Refresh(_ context.Context, _ string) (*oauth.Result, error) {
	c.count.Add(1)
	return c.result, c.err
}

// PR-4: refresh エラー → ErrRefreshTokenRevoked → req 未変更
func TestAuthenticator_Apply_RefreshError(t *testing.T) {
	t.Parallel()
	ts := newMockStore()
	past := time.Now().Add(-1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  "oauth:alice",
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  "expired",
		RefreshToken: "revoked",
		ExpiresAt:    past,
	})

	mr := &mockRefresher{err: oauth.ErrRefreshTokenRevoked}
	a := oauth.NewAuthenticator(ts, "example.cybozu.com", "oauth:alice", mr, nil)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.cybozu.com/k/v1/records.json", nil)
	err := a.Apply(t.Context(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, oauth.ErrRefreshTokenRevoked) {
		t.Errorf("expected ErrRefreshTokenRevoked, got: %v", err)
	}
	// Authorization ヘッダが設定されていないこと
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization should not be set on error, got: %q", got)
	}
}

// PR-5: TokenStore.Get 失敗 → エラー伝播
func TestAuthenticator_Apply_StoreGetError(t *testing.T) {
	t.Parallel()
	ts := newMockStore()
	ts.getErr = errors.New("db error")

	a := oauth.NewAuthenticator(ts, "example.cybozu.com", "oauth:alice", nil, nil)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.cybozu.com", nil)
	err := a.Apply(t.Context(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// PR-6: nil request → エラー
func TestAuthenticator_Apply_NilRequest(t *testing.T) {
	t.Parallel()
	a := oauth.NewAuthenticator(newMockStore(), "example.cybozu.com", "oauth:alice", nil, nil)
	err := a.Apply(t.Context(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

// PR-7: skew 内（now + skew >= expires_at）→ refresh トリガ
func TestAuthenticator_Apply_SkewTriggersRefresh(t *testing.T) {
	t.Parallel()
	ts := newMockStore()
	// 30 秒後に期限切れ（デフォルト skew は 60s → refresh トリガ）
	soonExpiry := time.Now().Add(30 * time.Second)
	_ = ts.Put(t.Context(), store.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  "oauth:alice",
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  "soon-expired",
		RefreshToken: "refresh-token",
		ExpiresAt:    soonExpiry,
	})

	mr := &mockRefresher{
		result: &oauth.Result{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh-token",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
		},
	}
	a := oauth.NewAuthenticator(ts, "example.cybozu.com", "oauth:alice", mr, nil)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.cybozu.com", nil)
	if err := a.Apply(t.Context(), req); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer refreshed-token" {
		t.Errorf("expected refreshed-token, got %q", got)
	}
}

// PR-8: refresh 後の new refresh_token を TokenStore に保存すること
func TestAuthenticator_Apply_StoresNewRefreshToken(t *testing.T) {
	t.Parallel()
	ts := newMockStore()
	past := time.Now().Add(-1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  "oauth:alice",
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  "expired",
		RefreshToken: "old-refresh",
		ExpiresAt:    past,
	})

	mr := &mockRefresher{
		result: &oauth.Result{
			AccessToken:  "new-access",
			RefreshToken: "brand-new-refresh",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
		},
	}
	a := oauth.NewAuthenticator(ts, "example.cybozu.com", "oauth:alice", mr, nil)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.cybozu.com", nil)
	_ = a.Apply(t.Context(), req)

	stored, _ := ts.Get(t.Context(), "example.cybozu.com", "oauth:alice", store.AuthTypeOAuth)
	if stored.RefreshToken != "brand-new-refresh" {
		t.Errorf("stored refresh_token: got %q, want %q", stored.RefreshToken, "brand-new-refresh")
	}
}

// PR-9: refresh レスポンスに refresh_token なし → 旧 refresh_token を維持
func TestAuthenticator_Apply_KeepsOldRefreshToken(t *testing.T) {
	t.Parallel()
	ts := newMockStore()
	past := time.Now().Add(-1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  "oauth:alice",
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  "expired",
		RefreshToken: "original-refresh",
		ExpiresAt:    past,
	})

	mr := &mockRefresher{
		result: &oauth.Result{
			AccessToken:  "new-access",
			RefreshToken: "original-refresh", // Refresher が旧値を維持して返す（RF-2 の動作）
			ExpiresAt:    time.Now().Add(1 * time.Hour),
		},
	}
	a := oauth.NewAuthenticator(ts, "example.cybozu.com", "oauth:alice", mr, nil)

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.cybozu.com", nil)
	_ = a.Apply(t.Context(), req)

	stored, _ := ts.Get(t.Context(), "example.cybozu.com", "oauth:alice", store.AuthTypeOAuth)
	if stored.RefreshToken != "original-refresh" {
		t.Errorf("stored refresh_token: got %q, want original-refresh", stored.RefreshToken)
	}
}
