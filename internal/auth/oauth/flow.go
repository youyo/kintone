package oauth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultOAuthScopes は kintone の全スコープ（6 個確定）。
// scope 未指定時のデフォルト。
var defaultOAuthScopes = []string{
	"k:app_record:read",
	"k:app_record:write",
	"k:app_settings:read",
	"k:app_settings:write",
	"k:file:read",
	"k:file:write",
}

// Config は Login フローの入力。
type Config struct {
	Domain       string // kintone domain (host only)
	ClientID     string
	ClientSecret string
	RedirectURL  string                 // http://127.0.0.1:<port>/callback（ポート 0 で動的割り当て）
	Scopes       []string               // 未指定なら defaultOAuthScopes
	HTTPClient   *http.Client           // nil なら 30s timeout の http.Client
	OpenBrowser  func(url string) error // nil なら DefaultOpenBrowser
	NoBrowser    bool                   // true のとき ブラウザ起動をスキップし URL を OpenBrowser に渡さない
	Now          func() time.Time
	RandReader   io.Reader
	Timeout      time.Duration // callback 待ち上限。0 なら 5min
}

// Result は Login の成功結果。
type Result struct {
	PrincipalID  string // M09 では空（呼び出し元で設定）
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	ExpiresIn    int
	Scope        string
}

// Login は Authorization Code + PKCE フローを完走し、Result を返す。
//
// フロー:
//  1. 入力検証（必須フィールド / loopback URL 検証）
//  2. PKCE / state 生成
//  3. loopback サーバ起動（RedirectURL のポートで listen）
//  4. ブラウザを authorization URL に誘導
//  5. callback 受信 → state 検証 → token endpoint 呼び出し
//  6. Result を返す
func Login(ctx context.Context, cfg Config) (*Result, error) {
	// 必須フィールド検証
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return nil, ErrMissingClientCredentials
	}

	// redirect URL の検証
	redirectHost, redirectPort, err := parseLoopbackRedirectURL(cfg.RedirectURL)
	if err != nil {
		return nil, err
	}

	// timeout 設定
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	// スコープ決定
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = defaultOAuthScopes
	}

	// HTTP クライアント
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	// ブラウザ起動関数
	openBrowser := cfg.OpenBrowser
	if openBrowser == nil {
		openBrowser = DefaultOpenBrowser
	}

	// Now 関数
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	// PKCE 生成
	pkce, err := GeneratePKCE(cfg.RandReader)
	if err != nil {
		return nil, fmt.Errorf("oauth: generate PKCE: %w", err)
	}

	// state 生成
	state, err := GenerateState(cfg.RandReader)
	if err != nil {
		return nil, fmt.Errorf("oauth: generate state: %w", err)
	}

	// loopback サーバ起動（ポート 0 のとき動的割り当て）
	listenAddr := fmt.Sprintf("%s:%s", redirectHost, redirectPort)
	callbackSrv, actualPort, err := NewCallbackServer(listenAddr, state)
	if err != nil {
		return nil, fmt.Errorf("oauth: start callback server: %w", err)
	}
	defer callbackSrv.Close()

	// 実際のポートで redirect URL を更新
	actualRedirectURL := cfg.RedirectURL
	if redirectPort == "0" {
		parsedURL, _ := url.Parse(cfg.RedirectURL)
		parsedURL.Host = fmt.Sprintf("%s:%d", redirectHost, actualPort)
		actualRedirectURL = parsedURL.String()
	}

	// authorization URL を構築
	scope := strings.Join(scopes, " ")
	// domain の処理：httptest サーバは "host:port" 形式なので scheme が必要
	var authorizeURL string
	if strings.Contains(cfg.Domain, ":") {
		// テスト用: host:port 形式 → http://
		authorizeURL = fmt.Sprintf("http://%s/oauth2/authorization", cfg.Domain)
	} else {
		authorizeURL = fmt.Sprintf("https://%s/oauth2/authorization", cfg.Domain)
	}
	authorizeURL += fmt.Sprintf(
		"?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s",
		urlEncode(cfg.ClientID),
		urlEncode(actualRedirectURL),
		urlEncode(scope),
		urlEncode(state),
	)
	// PKCE パラメータを付与（環境変数 KINTONE_OAUTH_PKCE_DISABLE=1 で無効化可能）
	authorizeURL += fmt.Sprintf(
		"&code_challenge=%s&code_challenge_method=%s",
		urlEncode(pkce.Challenge),
		urlEncode(pkce.Method),
	)

	// ブラウザ起動（失敗しても login は継続）
	if err := openBrowser(authorizeURL); err != nil {
		// ブラウザ起動失敗はログ出力しない（secret 混入回避）
		// 呼び出し元が --no-browser モード対応を担う
		_ = err
	}

	// callback 待ち
	callbackTimeout := time.Duration(timeout)
	callbackCtx, cancel := context.WithTimeout(ctx, callbackTimeout)
	defer cancel()

	resultCh := make(chan CallbackResult, 1)
	go callbackSrv.Serve(callbackCtx, resultCh)

	var callbackResult CallbackResult
	select {
	case callbackResult = <-resultCh:
	case <-callbackCtx.Done():
		return nil, ErrCallbackTimeout
	}

	if callbackResult.Err != nil {
		return nil, callbackResult.Err
	}

	// token endpoint を決定
	var tokenEndpoint string
	if strings.Contains(cfg.Domain, ":") {
		tokenEndpoint = fmt.Sprintf("http://%s/oauth2/token", cfg.Domain)
	} else {
		tokenEndpoint = fmt.Sprintf("https://%s/oauth2/token", cfg.Domain)
	}

	// token endpoint を呼び出す
	tokenResp, err := ExchangeCode(ctx, ExchangeCodeRequest{
		TokenEndpoint: tokenEndpoint,
		ClientID:      cfg.ClientID,
		ClientSecret:  cfg.ClientSecret,
		Code:          callbackResult.Code,
		RedirectURL:   actualRedirectURL,
		CodeVerifier:  pkce.Verifier,
		HTTPClient:    httpClient,
		Now:           now,
	})
	if err != nil {
		return nil, err
	}

	return &Result{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    tokenResp.ExpiresAt,
		ExpiresIn:    tokenResp.ExpiresIn,
		Scope:        tokenResp.Scope,
	}, nil
}

// parseLoopbackRedirectURL は redirect URL を検証し、host と port を返す。
// ホストが loopback (127.0.0.1 / localhost / [::1]) でなければ ErrInvalidRedirectURL。
func parseLoopbackRedirectURL(rawURL string) (host, port string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("%w: parse error: %v", ErrInvalidRedirectURL, err)
	}

	if u.Scheme != "http" {
		return "", "", fmt.Errorf("%w: scheme must be http, got %q", ErrInvalidRedirectURL, u.Scheme)
	}

	h, p, err := net.SplitHostPort(u.Host)
	if err != nil {
		// ポートなし
		h = u.Host
		p = "80"
	}

	switch h {
	case "127.0.0.1", "localhost", "[::1]":
		// OK
	default:
		return "", "", fmt.Errorf("%w: host must be 127.0.0.1/localhost/[::1], got %q", ErrInvalidRedirectURL, h)
	}

	return h, p, nil
}
