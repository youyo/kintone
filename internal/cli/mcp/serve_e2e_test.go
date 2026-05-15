//go:build e2e

// Phase 8.5 — in-process E2E ハーネス。
//
// シナリオ:
//   - TestE2E_OAuthRefreshFlow_SQLite (M12): 既存 Token + refresh フロー
//   - TestE2E_OAuthCallbackFlow_SQLite (M13): start → authorize → callback → Token 永続化
//   - TestE2E_BearerJWT_ForContext_OIDCIssuerPropagation (fix): Bearer JWT 経由での
//     principal_id 一致確認（AUTH_REQUIRED バグ回帰テスト）
//
// mcp serve 全体 (idproxy.Auth.Wrap 含む) の HTTP テストは複雑度が高いため、
// M13 では oauthcallback.Handler 単体 + kintonefake authorize endpoint で
// callback フローを統合検証する。idproxy 経路は idproxy 自体のテストでカバー済。
package mcp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	upstream "github.com/youyo/idproxy"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/mcp/oauthcallback"
	serviceapi "github.com/youyo/kintone/internal/service/api"
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
//     /start → 302 → kintonefake /authorize → 302 → /callback → 200
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

// TestE2E_CascadeMiddleware_OAuthCallbackFlow_SQLite (M16)
//
// cascade middleware（EnsureKintoneOAuthConnected）を介した kintone OAuth 完全フローを検証:
//  1. kintonefake 起動
//  2. sqlite Container 起動
//  3. cascade middleware + oauthcallback.Handler を組み合わせて mux を構築
//  4. cookiejar 付き http.Client で "/" から開始:
//     GET / → cascade → 302 /oauth/kintone/start
//     → 302 kintonefake /authorize → 302 /callback → 200 htmlAuthSuccess
//  5. TokenStore に Token が永続化されたことを確認
//  6. 2 回目の GET / は cascade をスキップ（token 有）して next に届く（404）
//
// idproxy は使わず、injectPrincipal ミドルウェアで Principal を context に直接注入する。
func TestE2E_CascadeMiddleware_OAuthCallbackFlow_SQLite(t *testing.T) {
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
		pid    = "https://issuer.example.com:cascade-user"
	)
	kf.SetAuthorizePrincipalID(pid)

	// 3) Handler + cascade middleware を組み立て
	mux := http.NewServeMux()
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	redirectURL := httpSrv.URL + "/oauth/kintone/callback"
	startURL := httpSrv.URL + "/oauth/kintone/start"
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

	// テスト用 Principal 注入 middleware（idproxy の代替）
	principal := &idproxy.Principal{
		ID:      pid,
		Issuer:  "https://issuer.example.com",
		Subject: "cascade-user",
	}
	injectPrincipal := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(idproxy.WithPrincipal(r.Context(), principal))
			next.ServeHTTP(w, r)
		})
	}

	// cascade middleware（EnsureKintoneOAuthConnected）
	cascade := EnsureKintoneOAuthConnected(tokens, domain, startURL)

	// mux: cascade は injectPrincipal の内側に配置（Principal が context に乗ってから cascade が動く）
	// ルート登録: cascade → mux の flow を再現するため、/ を catch-all ハンドラとして登録
	mux.Handle("/oauth/kintone/start", injectPrincipal(handler.StartHandler()))
	mux.Handle("/oauth/kintone/callback", injectPrincipal(handler.CallbackHandler()))
	// "/" へのアクセスは injectPrincipal → cascade → 404 not found（cascade が pass-through する場合）
	// cascade が 302 する場合はそちらが先に応答する
	notFoundHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.Handle("/", injectPrincipal(cascade(notFoundHandler)))

	// 4) cookiejar 付き Client（自動リダイレクト follow）
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
		// cascade は Accept: text/html を持つリクエストにのみ発火する。
		// リダイレクト follow 時も Accept ヘッダを維持するために CheckRedirect で引き継ぐ。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 {
				if accept := via[0].Header.Get("Accept"); accept != "" {
					req.Header.Set("Accept", accept)
				}
			}
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	// GET "/" から開始 → cascade が /oauth/kintone/start へ 302 → kintonefake を経由 → callback → 200
	req, _ := http.NewRequest(http.MethodGet, httpSrv.URL+"/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("final status = %d, want 200", resp.StatusCode)
	}
	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "認証が完了") {
		t.Errorf("success HTML missing: got %q", string(body[:n]))
	}

	// 5) TokenStore に Token が永続化されている
	ctx := context.Background()
	tok, err := tokens.Get(ctx, domain, pid, store.AuthTypeOAuth)
	if err != nil {
		t.Fatalf("Tokens.Get after cascade+callback: %v", err)
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		t.Errorf("token not stored: %+v", tok)
	}
	if tok.PrincipalID != pid {
		t.Errorf("PrincipalID = %q, want %q", tok.PrincipalID, pid)
	}

	// 6) 2 回目の GET "/" では cascade がトークン存在を確認して次へ委譲（404 を返す）
	// Accept: text/html を付けないと cascade が token チェックをスキップするため、
	// 必ず HTML accept を設定して「token あり → pass-through」ブランチを踏む。
	req2, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/", nil)
	if err != nil {
		t.Fatalf("NewRequest GET / 2nd: %v", err)
	}
	req2.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("GET / 2nd: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	// token が存在するため cascade は next に委譲 → "/" の notFoundHandler → 404
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("2nd GET / status = %d, want 404 (cascade should pass-through when token exists)", resp2.StatusCode)
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

// TestE2E_BearerJWT_ForContext_OIDCIssuerPropagation は
// Bearer JWT 経由の MCP リクエストで OIDC Issuer が principal_id に正しく伝播し、
// PrincipalAPIFactory.ForContext が kintone OAuth トークンを取得できることを確認する。
//
// 回帰テスト: idproxy v0.5.0 では Bearer JWT の User.Issuer が ExternalURL に設定されていたため
// kintone OAuth コールバック時の principal_id（OIDC issuer + ":" + sub）と不一致が発生し、
// AUTH_REQUIRED が返るバグがあった。
// idproxy v0.6.0 で Bearer JWT の oidc_issuer クレームにより修正済み。
//
// フロー:
//  1. oidcstub を起動（OIDC プロバイダ）
//  2. idproxy.New でAuth instance を作成（OAuth AS 含む）
//  3. ブラウザフロー: /login → oidcstub → idproxy /callback → セッション発行
//  4. idproxy OAuth AS: /authorize（セッション cookie 付き）→ /token → Bearer JWT 取得
//  5. TokenStore に kintone OAuth トークンを seed（OIDC issuer ベースの principal_id）
//  6. Bearer JWT で /test/forcontext にアクセス → ForContext 成功を確認
func TestE2E_BearerJWT_ForContext_OIDCIssuerPropagation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1) OIDC stub（OIDC プロバイダのシミュレート）
	const oidcSub = "bearer-e2e-user"
	oidcSrv, err := oidcstub.New(oidcstub.Config{
		ClientID:   "idproxy-client",
		SubjectFor: func(*http.Request) string { return oidcSub },
	})
	if err != nil {
		t.Fatalf("oidcstub.New: %v", err)
	}
	defer oidcSrv.Stop()
	oidcIssuer := oidcSrv.Issuer()

	// 2) SQLite Container + TokenStore
	dir := t.TempDir()
	container, err := store.OpenFromConfig(&store.Config{Backend: store.BackendSQLite, SQLiteDir: dir})
	if err != nil {
		t.Fatalf("OpenFromConfig: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(ctx) })

	tokens, err := container.Tokens()
	if err != nil {
		t.Fatalf("container.Tokens: %v", err)
	}

	// 3) idproxy Store（Bearer JWT 発行・検証用）
	idpStore, err := container.IDProxyStore()
	if err != nil {
		t.Fatalf("IDProxyStore: %v", err)
	}

	// 4) ECDSA 署名鍵（idproxy OAuth AS 用）
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}

	// 5) httptest.Server を先に起動して URL を確定（auth 構築より前に URL が必要）
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	defer srv.Close()
	externalURL := srv.URL

	// 6) idproxy.New でAuth instance を作成
	cookieSecret := make([]byte, 32)
	if _, err := rand.Read(cookieSecret); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	authCfg := upstream.Config{
		Providers: []upstream.OIDCProvider{{
			Issuer:       oidcIssuer,
			ClientID:     "idproxy-client",
			ClientSecret: "dummy-secret", // oidcstub は client_secret を検証しない
		}},
		ExternalURL:     externalURL,
		CookieSecret:    cookieSecret,
		Store:           idpStore,
		OAuth:           &upstream.OAuthConfig{SigningKey: privKey},
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		SessionMaxAge:   24 * time.Hour,
	}
	auth, err := upstream.New(ctx, authCfg)
	if err != nil {
		t.Fatalf("upstream.New: %v", err)
	}

	// 7) PrincipalAPIFactory（kintone OAuth トークンを ForContext で取得するファクトリ）
	const domain = "example.cybozu.com"
	factory, err := serviceapi.NewPrincipalAPIFactory(serviceapi.PrincipalAPIFactoryConfig{
		Base:      &config.Resolved{Domain: domain},
		Mode:      serviceapi.AuthZModeOAuth,
		Store:     tokens,
		Refresher: nil, // テストでは refresh は不要
		Fallback:  &noPrincipalFallback{},
	})
	if err != nil {
		t.Fatalf("NewPrincipalAPIFactory: %v", err)
	}

	// 8) ルートを設定（auth.Wrap → mux）
	// /test/forcontext: PrincipalMiddleware → ForContext テスト
	forContextResultCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.Handle("/test/forcontext", idproxy.PrincipalMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := factory.ForContext(r.Context())
		select {
		case forContextResultCh <- err:
		default:
		}
		w.WriteHeader(http.StatusOK)
	})))
	// auth.Wrap は /login, /callback, OAuth AS パス（/authorize, /token 等）を内部処理し、
	// その他は mux に委譲する
	srv.Config.Handler = auth.Wrap(mux)

	// 9) ブラウザフロー: OIDC 認証 → セッション発行
	jar, _ := cookiejar.New(nil)
	browserClient := &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
	}
	// GET /login → oidcstub /authorize → oidcstub /token → idproxy /callback → セッション発行
	// 最終的に redirect_to（"/" にデフォルト）へ 302 → 404 になる
	loginResp, err := browserClient.Get(externalURL + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	_ = loginResp.Body.Close()
	// セッション cookie が jar に設定されているはず

	// 10) idproxy OAuth AS で Bearer JWT を取得
	// PKCE (S256) を生成
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		t.Fatalf("rand.Read verifier: %v", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	state := "e2e-state"
	clientID := "claude-mcp-client"
	// redirect_uri は localhost のみ許可（idproxy のデフォルト）
	redirectURI := "http://localhost:9999/e2e-callback"

	// /authorize: セッション cookie 付きでアクセス → code が付いてリダイレクト
	noFollowClient := &http.Client{
		Jar: jar, // セッション cookie を共有
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}
	authorizeURL := fmt.Sprintf(
		"%s/authorize?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s&scope=%s",
		externalURL,
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(challenge),
		url.QueryEscape(state),
		url.QueryEscape("openid email profile"),
	)
	authorizeResp, err := noFollowClient.Get(authorizeURL)
	if err != nil {
		t.Fatalf("GET /authorize: %v", err)
	}
	_ = authorizeResp.Body.Close()
	if authorizeResp.StatusCode != http.StatusFound {
		t.Fatalf("authorize status = %d, want 302", authorizeResp.StatusCode)
	}
	locHeader := authorizeResp.Header.Get("Location")
	locURL, err := url.Parse(locHeader)
	if err != nil {
		t.Fatalf("parse /authorize Location %q: %v", locHeader, err)
	}
	code := locURL.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in /authorize redirect: %s", locHeader)
	}

	// /token: code → Bearer JWT
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	tokenResp, err := http.PostForm(externalURL+"/token", tokenForm)
	if err != nil {
		t.Fatalf("POST /token: %v", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()
	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		t.Fatalf("token status = %d: %s", tokenResp.StatusCode, body)
	}
	var tokenJSON struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenJSON); err != nil {
		t.Fatalf("decode /token response: %v", err)
	}
	if tokenJSON.AccessToken == "" {
		t.Fatal("empty access_token from /token")
	}
	bearerJWT := tokenJSON.AccessToken

	// 11) kintone OAuth トークンを TokenStore に seed
	// principal_id = oidcIssuer + ":" + oidcSub（コールバック時と同じ形式）
	expectedPID := oidcIssuer + ":" + oidcSub
	if err := tokens.Put(ctx, store.Token{
		Domain:       domain,
		PrincipalID:  expectedPID,
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  "kintone-access-token",
		RefreshToken: "kintone-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("tokens.Put: %v", err)
	}

	// 12) Bearer JWT で /test/forcontext にアクセス
	bearerReq, _ := http.NewRequest(http.MethodGet, externalURL+"/test/forcontext", nil)
	bearerReq.Header.Set("Authorization", "Bearer "+bearerJWT)
	bearerClient := &http.Client{Timeout: 5 * time.Second}
	fcResp, err := bearerClient.Do(bearerReq)
	if err != nil {
		t.Fatalf("Bearer request /test/forcontext: %v", err)
	}
	defer func() { _ = fcResp.Body.Close() }()
	if fcResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(fcResp.Body)
		t.Fatalf("forcontext status = %d: %s", fcResp.StatusCode, body)
	}

	// 13) ForContext の結果を確認（AUTH_REQUIRED が返らないこと）
	select {
	case err := <-forContextResultCh:
		if err != nil {
			t.Errorf("ForContext with Bearer JWT returned error: %v\n"+
				"  principal_id in JWT should be %q\n"+
				"  token was stored with principal_id %q",
				err, expectedPID, expectedPID)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for ForContext result")
	}
}
