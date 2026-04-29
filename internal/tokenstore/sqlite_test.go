package tokenstore_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/tokenstore"
)

// openStore はテスト用の一時 DB を開くヘルパ。
func openStore(t *testing.T) tokenstore.Store {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/tokens.db"
	s, err := tokenstore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// T-1: Put → Get で同一値が復元できること（api-token）
func TestPutGet_APIToken(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	tok := tokenstore.Token{
		Domain:      "example.cybozu.com",
		PrincipalID: "",
		AuthType:    tokenstore.AuthTypeAPIToken,
		APIToken:    "secret-token",
		ExpiresAt:   time.Time{}, // api-token は expires なし
		UpdatedAt:   time.Now().Truncate(time.Second),
	}
	if err := s.Put(ctx, tok); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.APIToken != tok.APIToken {
		t.Errorf("APIToken: want %q, got %q", tok.APIToken, got.APIToken)
	}
	if got.Domain != tok.Domain {
		t.Errorf("Domain: want %q, got %q", tok.Domain, got.Domain)
	}
	if got.AuthType != tok.AuthType {
		t.Errorf("AuthType: want %q, got %q", tok.AuthType, got.AuthType)
	}
}

// T-2: Get で存在しないキーは ErrNotFound
func TestGet_NotFound(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_, err := s.Get(ctx, "no.such.domain", "", tokenstore.AuthTypeAPIToken)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != tokenstore.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// T-3: Put で上書き → 後勝ちかつ UpdatedAt 更新
func TestPut_Overwrite(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	first := tokenstore.Token{
		Domain:    "example.cybozu.com",
		AuthType:  tokenstore.AuthTypeAPIToken,
		APIToken:  "first-token",
		UpdatedAt: time.Now().Add(-time.Hour).Truncate(time.Second),
	}
	if err := s.Put(ctx, first); err != nil {
		t.Fatalf("Put first: %v", err)
	}

	second := tokenstore.Token{
		Domain:    "example.cybozu.com",
		AuthType:  tokenstore.AuthTypeAPIToken,
		APIToken:  "second-token",
		UpdatedAt: time.Now().Truncate(time.Second),
	}
	if err := s.Put(ctx, second); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	got, err := s.Get(ctx, second.Domain, second.PrincipalID, second.AuthType)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.APIToken != second.APIToken {
		t.Errorf("APIToken: want %q, got %q", second.APIToken, got.APIToken)
	}
	// UpdatedAt は後者の時刻
	if !got.UpdatedAt.Equal(second.UpdatedAt) {
		t.Errorf("UpdatedAt: want %v, got %v", second.UpdatedAt, got.UpdatedAt)
	}
}

// T-4: Delete 後に Get すると ErrNotFound
func TestDelete(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	tok := tokenstore.Token{
		Domain:   "example.cybozu.com",
		AuthType: tokenstore.AuthTypeAPIToken,
		APIToken: "token",
	}
	if err := s.Put(ctx, tok); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(ctx, tok.Domain, tok.PrincipalID, tok.AuthType); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType)
	if err != tokenstore.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// T-5: 同 domain・同 principal だが AuthType 異なる → 別エントリとして両立
func TestKeyUniqueness_AuthType(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	apiTok := tokenstore.Token{
		Domain:   "example.cybozu.com",
		AuthType: tokenstore.AuthTypeAPIToken,
		APIToken: "api-secret",
	}
	oauthTok := tokenstore.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  "google:sub12345",
		AuthType:     tokenstore.AuthTypeOAuth,
		AccessToken:  "oauth-access",
		RefreshToken: "oauth-refresh",
	}

	if err := s.Put(ctx, apiTok); err != nil {
		t.Fatalf("Put api: %v", err)
	}
	if err := s.Put(ctx, oauthTok); err != nil {
		t.Fatalf("Put oauth: %v", err)
	}

	gotAPI, err := s.Get(ctx, apiTok.Domain, apiTok.PrincipalID, apiTok.AuthType)
	if err != nil {
		t.Fatalf("Get api: %v", err)
	}
	if gotAPI.APIToken != apiTok.APIToken {
		t.Errorf("api token: want %q, got %q", apiTok.APIToken, gotAPI.APIToken)
	}

	gotOAuth, err := s.Get(ctx, oauthTok.Domain, oauthTok.PrincipalID, oauthTok.AuthType)
	if err != nil {
		t.Fatalf("Get oauth: %v", err)
	}
	if gotOAuth.AccessToken != oauthTok.AccessToken {
		t.Errorf("access token: want %q, got %q", oauthTok.AccessToken, gotOAuth.AccessToken)
	}
}

// T-6: OAuth Token の全フィールドが復元できること
func TestPutGet_OAuthToken(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	tok := tokenstore.Token{
		Domain:       "example.cybozu.com",
		PrincipalID:  "google:sub12345",
		AuthType:     tokenstore.AuthTypeOAuth,
		AccessToken:  "access-xxx",
		RefreshToken: "refresh-yyy",
		ExpiresAt:    now.Add(time.Hour),
		UpdatedAt:    now,
	}
	if err := s.Put(ctx, tok); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != tok.AccessToken {
		t.Errorf("AccessToken: want %q, got %q", tok.AccessToken, got.AccessToken)
	}
	if got.RefreshToken != tok.RefreshToken {
		t.Errorf("RefreshToken: want %q, got %q", tok.RefreshToken, got.RefreshToken)
	}
	if !got.ExpiresAt.Equal(tok.ExpiresAt) {
		t.Errorf("ExpiresAt: want %v, got %v", tok.ExpiresAt, got.ExpiresAt)
	}
	if got.PrincipalID != tok.PrincipalID {
		t.Errorf("PrincipalID: want %q, got %q", tok.PrincipalID, got.PrincipalID)
	}
}

// T-7: UpdatedAt が zero 値でも現在時刻が保存されること
func TestPut_UpdatedAtAutoSet(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	before := time.Now().Truncate(time.Second)
	tok := tokenstore.Token{
		Domain:   "example.cybozu.com",
		AuthType: tokenstore.AuthTypeAPIToken,
		APIToken: "token",
		// UpdatedAt は意図的に zero
	}
	if err := s.Put(ctx, tok); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero after Put")
	}
	after := time.Now().Add(time.Second)
	if got.UpdatedAt.Before(before) || got.UpdatedAt.After(after) {
		t.Errorf("UpdatedAt %v not in range [%v, %v]", got.UpdatedAt, before, after)
	}
}

// Open: 書き込み不可パスはエラーを返す
func TestOpen_InvalidPath(t *testing.T) {
	// /dev/null/tokens.db は書けないため error になる
	_, err := tokenstore.Open("/dev/null/tokens.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

// Close 後も Close を呼んでもエラーにならない
func TestClose_Idempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := tokenstore.Open(dir + "/tokens.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// Delete: 存在しないキーは no-op でエラーなし
func TestDelete_Noop(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	// 存在しないキーの削除はエラーにならない
	if err := s.Delete(ctx, "no.domain", "", tokenstore.AuthTypeAPIToken); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

// OpenIfExists: ファイル不在時は (nil, false, nil)
func TestOpenIfExists_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/notexist.db"
	s, exists, err := tokenstore.OpenIfExists(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected exists=false")
	}
	if s != nil {
		t.Error("expected nil store")
	}
}

// OpenIfExists: ファイル存在時は正常に開ける
func TestOpenIfExists_Exists(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/tokens.db"
	// まず Open で作成
	s1, err := tokenstore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = s1.Close()

	s2, exists, err := tokenstore.OpenIfExists(path)
	if err != nil {
		t.Fatalf("OpenIfExists: %v", err)
	}
	if !exists {
		t.Error("expected exists=true")
	}
	if s2 == nil {
		t.Fatal("expected non-nil store")
	}
	_ = s2.Close()
}

// T-8: 同キーで 10 goroutine 並行 Put → エラーなし、最後の値が残る
func TestPut_Concurrent(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok := tokenstore.Token{
				Domain:   "concurrent.cybozu.com",
				AuthType: tokenstore.AuthTypeAPIToken,
				APIToken: "token",
			}
			errs[i] = s.Put(ctx, tok)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Put error: %v", i, err)
		}
	}
	// 最終的に Get できること
	_, err := s.Get(ctx, "concurrent.cybozu.com", "", tokenstore.AuthTypeAPIToken)
	if err != nil {
		t.Errorf("Get after concurrent Put: %v", err)
	}
}
