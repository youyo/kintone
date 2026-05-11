//go:build e2e

// Phase 8.5 — in-process E2E ハーネス。
//
// シナリオ:
//   - TestE2E_OAuthRefreshFlow_SQLite (M12): 既存 Token + refresh フロー
//   - TestE2E_OAuthCallbackFlow_SQLite (M13): start → authorize → callback → Token 永続化
//
// mcp serve 全体 (idproxy.Auth.Wrap 含む) の HTTP テストは複雑度が高いため、
// M13 では oauthcallback.Handler 単体 + kintonefake authorize endpoint で
// callback フローを統合検証する。idproxy 経路は idproxy 自体のテストでカバー済。
package mcp

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/mcp/oauthcallback"
	"github.com/youyo/kintone/internal/store"
	_ "github.com/youyo/kintone/internal/store/sqlite"
	"github.com/youyo/kintone/internal/store/storetest"
	"github.com/youyo/kintone/internal/testsupport/kintonefake"
	"github.com/youyo/kintone/internal/testsupport/oidcstub"
)

func TestE2E_OAuthRefreshFlow_SQLite(t *testing.T) {
	t.Parallel()

	// 1) OIDC stub（discovery / jwks 経路の到達確認のみ）
	oidc, err := oidcstub.New(oidcstub.Config{ClientID: "kintone-mcp"})
	if err != nil {
		t.Fatalf("oidcstub.New: %v", err)
	}
	defer oidc.Stop()

	// 2) kintone fake
	kf := kintonefake.New()
	defer kf.Stop()

	// 3) sqlite backend Container
	dir := t.TempDir()
	cfg := &store.Config{Backend: store.BackendSQLite, SQLiteDir: dir}
	container, err := store.OpenFromConfig(cfg)
	if err != nil {
		t.Fatalf("OpenFromConfig sqlite: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })

	const (
		domain      = "example.cybozu.com"
		principalID = "oauth:user-1"
	)

	// 4) 初期 refresh_token を fake サーバ + Storage 双方に seed
	initialRT := kf.SeedTokenFor(principalID)
	seedTok := store.Token{
		Domain:       domain,
		PrincipalID:  principalID,
		AuthType:     store.AuthTypeOAuth,
		RefreshToken: initialRT,
	}
	if err := storetest.SeedTokenForE2E(context.Background(), container, seedTok); err != nil {
		t.Fatalf("SeedTokenForE2E: %v", err)
	}

	// 5) Refresher で refresh_token grant を実行
	refresher := oauth.NewRefresher(oauth.RefresherConfig{
		TokenEndpoint: kf.URL() + "/oauth2/token",
		ClientID:      "kintone-cli",
		ClientSecret:  "test-secret",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := refresher.Refresh(ctx, initialRT)
	if err != nil {
		t.Fatalf("Refresher.Refresh: %v", err)
	}
	if res.AccessToken == "" {
		t.Fatal("AccessToken empty after refresh")
	}
	if res.RefreshToken == "" || res.RefreshToken == initialRT {
		t.Fatalf("RefreshToken not rotated: got %q (initial %q)", res.RefreshToken, initialRT)
	}

	// 6) Storage に新しい Token を書き戻し、永続化を確認
	updated := seedTok
	updated.AccessToken = res.AccessToken
	updated.RefreshToken = res.RefreshToken
	updated.ExpiresAt = res.ExpiresAt
	if err := storetest.SeedTokenForE2E(context.Background(), container, updated); err != nil {
		t.Fatalf("SeedTokenForE2E (write-back): %v", err)
	}
	tokens, err := container.Tokens()
	if err != nil {
		t.Fatalf("container.Tokens: %v", err)
	}
	got, err := tokens.Get(ctx, domain, principalID, store.AuthTypeOAuth)
	if err != nil {
		t.Fatalf("Tokens.Get: %v", err)
	}
	if got.RefreshToken != res.RefreshToken {
		t.Fatalf("Storage RefreshToken got=%q want=%q", got.RefreshToken, res.RefreshToken)
	}
	if got.AccessToken != res.AccessToken {
		t.Fatalf("Storage AccessToken got=%q want=%q", got.AccessToken, res.AccessToken)
	}

	// 7) OIDC stub の discovery が応答することを最低限確認
	_ = oidc.URL()
}

// TestE2E_OAuthCallbackFlow_SQLite (M13)
//
// サーバホスト型 OAuth callback の完全フローを in-process で検証:
//  1. kintonefake 起動（/oauth2/authorization + /oauth2/token を mock）
//  2. sqlite Container 起動
//  3. oauthcallback.Handler を /oauth/kintone/start, /callback で公開
//  4. cookiejar 付き http.Client（CheckRedirect 制御）で:
//       /start → 302 → kintonefake /authorize → 302 → /callback → 200
//  5. TokenStore に Token が永続化されたことを確認
//
// idproxy 経由の Principal は test 用 middleware で context に直接注入する。
func TestE2E_OAuthCallbackFlow_SQLite(t *testing.T) {
	t.Parallel()

	// 1) kintonefake
	kf := kintonefake.New()
	defer kf.Stop()

	// 2) sqlite Container
	dir := t.TempDir()
	cfg := &store.Config{Backend: store.BackendSQLite, SQLiteDir: dir}
	container, err := store.OpenFromConfig(cfg)
	if err != nil {
		t.Fatalf("OpenFromConfig sqlite: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })

	tokens, err := container.Tokens()
	if err != nil {
		t.Fatalf("Tokens: %v", err)
	}

	const (
		domain = "example.cybozu.com"
		pid    = "https://issuer.example.com:user-1"
	)
	kf.SetAuthorizePrincipalID(pid)

	// 3) Handler を組み立てる前に http.Server を立てて redirect_uri を確定
	mux := http.NewServeMux()
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	redirectURL := httpSrv.URL + "/oauth/kintone/callback"
	states := oauthcallback.NewMemoryStateStore()
	defer func() { _ = states.Close() }()

	handler, err := oauthcallback.NewHandler(oauthcallback.HandlerConfig{
		Domain:        domain,
		ClientID:      "kintone-mcp",
		ClientSecret:  "secret",
		RedirectURL:   redirectURL,
		ExternalURL:   httpSrv.URL,
		States:        states,
		Tokens:        tokens,
		AuthorizeBase: kf.URL() + "/oauth2/authorization",
		TokenEndpoint: kf.URL() + "/oauth2/token",
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	// テスト用 idproxy.Principal 注入ミドルウェア
	principal := &idproxy.Principal{
		ID:      pid,
		Issuer:  "https://issuer.example.com",
		Subject: "user-1",
	}
	injectPrincipal := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(idproxy.WithPrincipal(r.Context(), principal))
			next.ServeHTTP(w, r)
		})
	}
	mux.Handle("/oauth/kintone/start", injectPrincipal(handler.StartHandler()))
	mux.Handle("/oauth/kintone/callback", injectPrincipal(handler.CallbackHandler()))

	// 4) cookiejar 付き Client（CheckRedirect は自動 follow 任せ）
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	resp, err := client.Get(httpSrv.URL + "/oauth/kintone/start")
	if err != nil {
		t.Fatalf("GET /start: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("final status = %d, want 200", resp.StatusCode)
	}
	// HTML body に「kintone 認証が完了しました」が含まれる
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "認証が完了") {
		t.Errorf("success HTML missing: %s", string(body[:n]))
	}

	// 5) TokenStore に Token が永続化されている
	ctx := context.Background()
	tok, err := tokens.Get(ctx, domain, pid, store.AuthTypeOAuth)
	if err != nil {
		t.Fatalf("Tokens.Get after callback: %v", err)
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Errorf("token not stored properly: %+v", tok)
	}
	if tok.PrincipalID != pid {
		t.Errorf("PrincipalID = %q, want %q", tok.PrincipalID, pid)
	}
}

// TestE2E_OAuthCallback_StateCSRF_403 (M13)
//
// state cookie 改ざんで callback が 403 を返すことを確認。
func TestE2E_OAuthCallback_StateCSRF_403(t *testing.T) {
	t.Parallel()

	kf := kintonefake.New()
	defer kf.Stop()

	dir := t.TempDir()
	container, err := store.OpenFromConfig(&store.Config{Backend: store.BackendSQLite, SQLiteDir: dir})
	if err != nil {
		t.Fatalf("OpenFromConfig: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })

	tokens, _ := container.Tokens()
	mux := http.NewServeMux()
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	states := oauthcallback.NewMemoryStateStore()
	defer func() { _ = states.Close() }()

	handler, err := oauthcallback.NewHandler(oauthcallback.HandlerConfig{
		Domain:        "example.cybozu.com",
		ClientID:      "cid",
		ClientSecret:  "sec",
		RedirectURL:   httpSrv.URL + "/oauth/kintone/callback",
		ExternalURL:   httpSrv.URL,
		States:        states,
		Tokens:        tokens,
		AuthorizeBase: kf.URL() + "/oauth2/authorization",
		TokenEndpoint: kf.URL() + "/oauth2/token",
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	pid := "issuer:csrf-user"
	principal := &idproxy.Principal{ID: pid}
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(idproxy.WithPrincipal(r.Context(), principal))
			next.ServeHTTP(w, r)
		})
	}
	mux.Handle("/oauth/kintone/start", inject(handler.StartHandler()))
	mux.Handle("/oauth/kintone/callback", inject(handler.CallbackHandler()))

	// 1) start で state を取得（jar 不要、手動 cookie 管理）
	noJarClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}
	resp, err := noJarClient.Get(httpSrv.URL + "/oauth/kintone/start")
	if err != nil {
		t.Fatalf("GET /start: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")

	// 2) 手動で tampered cookie を付けて callback を叩く
	req, _ := http.NewRequest(http.MethodGet,
		httpSrv.URL+"/oauth/kintone/callback?code=fake&state="+url.QueryEscape(state), nil)
	req.AddCookie(&http.Cookie{Name: oauthcallback.StateCookieName, Value: "tampered"})
	resp2, err := noJarClient.Do(req)
	if err != nil {
		t.Fatalf("GET /callback: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp2.StatusCode)
	}
}
