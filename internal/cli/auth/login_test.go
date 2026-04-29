package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth/oauth"
	cliauth "github.com/youyo/kintone/internal/cli/auth"
	"github.com/youyo/kintone/internal/tokenstore"
)

// AL-1: OAuth.Login hook が呼ばれ、結果を TokenStore.Put すること。
func TestLogin_Success(t *testing.T) {
	// DB ファイルパスを共有して login 後に再オープンで確認する
	dbPath := t.TempDir() + "/tokens.db"
	cliauth.SetOpenTokenStoreFn(func() (tokenstore.Store, error) {
		return tokenstore.Open(dbPath)
	})
	t.Cleanup(cliauth.ResetOpenTokenStoreFn)

	loginCalled := false
	cliauth.SetLoginFn(func(_ context.Context, cfg oauth.Config) (*oauth.Result, error) {
		loginCalled = true
		return &oauth.Result{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
			ExpiresAt:    time.Now().Add(1 * time.Hour),
			ExpiresIn:    3600,
			Scope:        "k:app_record:read",
		}, nil
	})
	t.Cleanup(cliauth.ResetLoginFn)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")
	t.Setenv("KINTONE_AUTH", "oauth")
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "my-client-id")
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "my-client-secret")
	t.Setenv("KINTONE_OAUTH_REDIRECT_URL", "http://127.0.0.1:8080/callback")

	var out, errOut bytes.Buffer
	err := cliauth.ExecuteLoginWith([]string{"--oauth", "--principal-id", "oauth:alice"}, &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v\nout: %s", err, out.String())
	}

	if !loginCalled {
		t.Error("Login was not called")
	}

	// login 後に DB を再オープンして確認
	store, err := tokenstore.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen tokenstore: %v", err)
	}
	defer func() { _ = store.Close() }()
	tok, err := store.Get(t.Context(), "example.cybozu.com", "oauth:alice", tokenstore.AuthTypeOAuth)
	if err != nil {
		t.Fatalf("TokenStore.Get: %v", err)
	}
	if tok.AccessToken != "test-access-token" {
		t.Errorf("access_token: got %q", tok.AccessToken)
	}

	// JSON 出力
	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			PrincipalID string `json:"principal_id"`
			ExpiresAt   string `json:"expires_at"`
			Scope       string `json:"scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("parse JSON: %v, out=%q", err, out.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if resp.Data.PrincipalID != "oauth:alice" {
		t.Errorf("principal_id: got %q", resp.Data.PrincipalID)
	}
}

// AL-2: 必須環境変数欠落 → USAGE エラー JSON
func TestLogin_MissingRequiredEnv(t *testing.T) {
	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")
	t.Setenv("KINTONE_AUTH", "oauth")
	// KINTONE_OAUTH_CLIENT_ID が未設定
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "")
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "")
	t.Setenv("KINTONE_OAUTH_REDIRECT_URL", "")

	store := newTestStore(t)
	cliauth.SetOpenTokenStoreFn(func() (tokenstore.Store, error) { return store, nil })
	t.Cleanup(cliauth.ResetOpenTokenStoreFn)

	var out, errOut bytes.Buffer
	err := cliauth.ExecuteLoginWith([]string{"--oauth", "--principal-id", "oauth:alice"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if resp.Error.Code != "USAGE" {
		t.Errorf("Code=%q want USAGE", resp.Error.Code)
	}
}

// AL-3: OAuth.Login エラー（state mismatch）→ OAUTH_STATE_MISMATCH
func TestLogin_StateMismatch(t *testing.T) {
	store := newTestStore(t)
	cliauth.SetOpenTokenStoreFn(func() (tokenstore.Store, error) { return store, nil })
	t.Cleanup(cliauth.ResetOpenTokenStoreFn)

	cliauth.SetLoginFn(func(_ context.Context, _ oauth.Config) (*oauth.Result, error) {
		return nil, oauth.ErrStateMismatch
	})
	t.Cleanup(cliauth.ResetLoginFn)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")
	t.Setenv("KINTONE_AUTH", "oauth")
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "my-client-id")
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "my-secret")
	t.Setenv("KINTONE_OAUTH_REDIRECT_URL", "http://127.0.0.1:8080/callback")

	var out, errOut bytes.Buffer
	err := cliauth.ExecuteLoginWith([]string{"--oauth", "--principal-id", "oauth:alice"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var resp struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if resp.Error.Code != "OAUTH_STATE_MISMATCH" {
		t.Errorf("Code=%q want OAUTH_STATE_MISMATCH", resp.Error.Code)
	}
}

// AL-4: 成功時の JSON envelope に principal_id / expires_at / scope が含まれること。
func TestLogin_OutputContainsPrincipalID(t *testing.T) {
	store := newTestStore(t)
	cliauth.SetOpenTokenStoreFn(func() (tokenstore.Store, error) { return store, nil })
	t.Cleanup(cliauth.ResetOpenTokenStoreFn)

	cliauth.SetLoginFn(func(_ context.Context, _ oauth.Config) (*oauth.Result, error) {
		return &oauth.Result{
			AccessToken:  "tok",
			RefreshToken: "ref",
			ExpiresAt:    time.Unix(2000, 0),
			ExpiresIn:    3600,
			Scope:        "k:app_record:read k:app_record:write",
		}, nil
	})
	t.Cleanup(cliauth.ResetLoginFn)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")
	t.Setenv("KINTONE_AUTH", "oauth")
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "id")
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("KINTONE_OAUTH_REDIRECT_URL", "http://127.0.0.1:8080/callback")

	var out, errOut bytes.Buffer
	err := cliauth.ExecuteLoginWith([]string{"--oauth", "--principal-id", "oauth:bob"}, &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v\nout: %s", err, out.String())
	}

	var resp struct {
		OK   bool           `json:"ok"`
		Data map[string]any `json:"data"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v", parseErr)
	}
	if _, ok := resp.Data["principal_id"]; !ok {
		t.Error("principal_id not in data")
	}
	if _, ok := resp.Data["expires_at"]; !ok {
		t.Error("expires_at not in data")
	}
	if _, ok := resp.Data["scope"]; !ok {
		t.Error("scope not in data")
	}
}

// AL-5: --oauth フラグ未指定 → USAGE
func TestLogin_MissingOAuthFlag(t *testing.T) {
	store := newTestStore(t)
	cliauth.SetOpenTokenStoreFn(func() (tokenstore.Store, error) { return store, nil })
	t.Cleanup(cliauth.ResetOpenTokenStoreFn)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "id")
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("KINTONE_OAUTH_REDIRECT_URL", "http://127.0.0.1:8080/callback")

	var out, errOut bytes.Buffer
	err := cliauth.ExecuteLoginWith([]string{"--principal-id", "oauth:alice"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if resp.Error.Code != "USAGE" {
		t.Errorf("Code=%q want USAGE", resp.Error.Code)
	}
}

// AL-6: --no-browser フラグ指定 → NoBrowser=true が Login に渡されること。
func TestLogin_NoBrowserFlag(t *testing.T) {
	store := newTestStore(t)
	cliauth.SetOpenTokenStoreFn(func() (tokenstore.Store, error) { return store, nil })
	t.Cleanup(cliauth.ResetOpenTokenStoreFn)

	var gotNoBrowser bool
	cliauth.SetLoginFn(func(_ context.Context, cfg oauth.Config) (*oauth.Result, error) {
		gotNoBrowser = cfg.NoBrowser
		return &oauth.Result{
			AccessToken: "tok", RefreshToken: "ref",
			ExpiresAt: time.Now().Add(time.Hour), ExpiresIn: 3600,
		}, nil
	})
	t.Cleanup(cliauth.ResetLoginFn)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")
	t.Setenv("KINTONE_AUTH", "oauth")
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "id")
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("KINTONE_OAUTH_REDIRECT_URL", "http://127.0.0.1:8080/callback")

	var out, errOut bytes.Buffer
	_ = cliauth.ExecuteLoginWith([]string{"--oauth", "--principal-id", "oauth:alice", "--no-browser"}, &out, &errOut)

	if !gotNoBrowser {
		t.Error("expected NoBrowser=true, got false")
	}
}

// AL-7: --principal-id 未指定 → USAGE
func TestLogin_MissingPrincipalID(t *testing.T) {
	store := newTestStore(t)
	cliauth.SetOpenTokenStoreFn(func() (tokenstore.Store, error) { return store, nil })
	t.Cleanup(cliauth.ResetOpenTokenStoreFn)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")
	t.Setenv("KINTONE_AUTH", "oauth")
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "id")
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("KINTONE_OAUTH_REDIRECT_URL", "http://127.0.0.1:8080/callback")

	var out, errOut bytes.Buffer
	err := cliauth.ExecuteLoginWith([]string{"--oauth"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if resp.Error.Code != "USAGE" {
		t.Errorf("Code=%q want USAGE", resp.Error.Code)
	}
}

// AL-8: --principal-id 空文字 → USAGE
func TestLogin_EmptyPrincipalID(t *testing.T) {
	store := newTestStore(t)
	cliauth.SetOpenTokenStoreFn(func() (tokenstore.Store, error) { return store, nil })
	t.Cleanup(cliauth.ResetOpenTokenStoreFn)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")
	t.Setenv("KINTONE_AUTH", "oauth")
	t.Setenv("KINTONE_OAUTH_CLIENT_ID", "id")
	t.Setenv("KINTONE_OAUTH_CLIENT_SECRET", "secret")
	t.Setenv("KINTONE_OAUTH_REDIRECT_URL", "http://127.0.0.1:8080/callback")

	var out, errOut bytes.Buffer
	err := cliauth.ExecuteLoginWith([]string{"--oauth", "--principal-id", ""}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for empty principal-id, got nil")
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if resp.Error.Code != "USAGE" {
		t.Errorf("Code=%q want USAGE", resp.Error.Code)
	}
}

// --- test helpers ---

// newTestStore は t.TempDir() ベースのインメモリ的な tokenstore を返す。
func newTestStore(t *testing.T) tokenstore.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := tokenstore.Open(dir + "/tokens.db")
	if err != nil {
		t.Fatalf("open tokenstore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
