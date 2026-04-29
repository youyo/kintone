package oauth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth/oauth"
)

// defaultScopes は kintone の全スコープ（6 個）。
var defaultScopes = []string{
	"k:app_record:read",
	"k:app_record:write",
	"k:app_settings:read",
	"k:app_settings:write",
	"k:file:read",
	"k:file:write",
}

// FL-1: OpenBrowser hook が authorize URL を受け取る（URL の検証）。
func TestLogin_AuthorizeURL(t *testing.T) {
	t.Parallel()
	var gotURL string
	cfg := oauth.Config{
		Domain:       "example.cybozu.com",
		ClientID:     "my-client-id",
		ClientSecret: "my-client-secret",
		RedirectURL:  "http://127.0.0.1:0/callback", // ポート 0 は flow 内でポートを動的に割り当て
		Scopes:       defaultScopes,
		Timeout:      100 * time.Millisecond,
		OpenBrowser: func(urlStr string) error {
			gotURL = urlStr
			// login は継続するが callback が来ないため timeout になる（想定内）
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	_, _ = oauth.Login(ctx, cfg) // timeout エラーは想定内

	if gotURL == "" {
		t.Fatal("OpenBrowser was not called")
	}

	u, err := url.Parse(gotURL)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}
	q := u.Query()

	if q.Get("response_type") != "code" {
		t.Errorf("response_type: got %q, want %q", q.Get("response_type"), "code")
	}
	if q.Get("client_id") != "my-client-id" {
		t.Errorf("client_id: got %q, want %q", q.Get("client_id"), "my-client-id")
	}
	if q.Get("state") == "" {
		t.Error("state is empty")
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge is empty")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method: got %q, want %q", q.Get("code_challenge_method"), "S256")
	}
	// redirect_uri が含まれること（ポートは動的）
	if q.Get("redirect_uri") == "" {
		t.Error("redirect_uri is empty")
	}
	// scope が含まれること
	if q.Get("scope") == "" {
		t.Error("scope is empty")
	}
}

// FL-2: 正常 flow（mock OAuth サーバ + hook ブラウザ） → Result
func TestLogin_Success(t *testing.T) {
	t.Parallel()

	// mock kintone OAuth サーバ
	var receivedState string
	var receivedCodeVerifier string
	mux := http.NewServeMux()

	tokenSrv := httptest.NewServer(mux)
	defer tokenSrv.Close()

	// OAuth token endpoint
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		receivedCodeVerifier = r.Form.Get("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"scope":         strings.Join(defaultScopes, " "),
		})
	})

	// コールバックサーバを開始して実際のポートを取得
	// OpenBrowser フックで callback を自動送信する
	var callbackURL string

	cfg := oauth.Config{
		Domain:       tokenSrv.Listener.Addr().String(), // httptest の host:port
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		// RedirectURL は flow 内で動的に決まるため、"http://127.0.0.1:0/callback" を使う
		RedirectURL: "http://127.0.0.1:0/callback",
		Scopes:      defaultScopes,
		HTTPClient:  tokenSrv.Client(),
		Timeout:     5 * time.Second,
		OpenBrowser: func(authorizeURL string) error {
			// authorize URL から state を取得
			u, err := url.Parse(authorizeURL)
			if err != nil {
				return err
			}
			receivedState = u.Query().Get("state")

			// callbackURL（redirect_uri + code + state）に GET を送信
			redirectURIStr := u.Query().Get("redirect_uri")
			redirectURI, err := url.Parse(redirectURIStr)
			if err != nil {
				return err
			}
			callbackURL = fmt.Sprintf("http://%s/callback?code=test-auth-code&state=%s",
				redirectURI.Host, receivedState)

			go func() {
				time.Sleep(50 * time.Millisecond)
				resp, err := http.Get(callbackURL) //nolint:noctx
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
			return nil
		},
		Now: func() time.Time { return time.Unix(1000, 0) },
	}

	result, err := oauth.Login(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if result.AccessToken != "test-access-token" {
		t.Errorf("access_token: got %q", result.AccessToken)
	}
	if result.RefreshToken != "test-refresh-token" {
		t.Errorf("refresh_token: got %q", result.RefreshToken)
	}
	if result.ExpiresAt.IsZero() {
		t.Error("expires_at is zero")
	}
	// code_verifier が token endpoint に送信されていること（PKCE）
	if receivedCodeVerifier == "" {
		t.Error("code_verifier was not sent to token endpoint")
	}
}

// FL-3: OpenBrowser エラー → URL を stdout に出力。Login は継続。
func TestLogin_OpenBrowserError(t *testing.T) {
	t.Parallel()
	cfg := oauth.Config{
		Domain:       "example.cybozu.com",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://127.0.0.1:0/callback",
		Timeout:      100 * time.Millisecond,
		OpenBrowser: func(_ string) error {
			return fmt.Errorf("browser exec failed")
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	// エラーになるのは timeout（ErrCallbackTimeout）
	_, err := oauth.Login(ctx, cfg)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// ErrCallbackTimeout または context.DeadlineExceeded を想定
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("unexpected error type: %v", err)
	}
}

// FL-4: redirect URL が loopback でない → ErrInvalidRedirectURL
func TestLogin_InvalidRedirectURL(t *testing.T) {
	t.Parallel()
	cfg := oauth.Config{
		Domain:       "example.cybozu.com",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://example.com/callback", // loopback でない
	}
	_, err := oauth.Login(t.Context(), cfg)
	if err == nil {
		t.Fatal("expected ErrInvalidRedirectURL, got nil")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Errorf("expected loopback error, got: %v", err)
	}
}

// FL-5: client_id/secret/redirect 未設定 → ErrMissingClientCredentials
func TestLogin_MissingCredentials(t *testing.T) {
	t.Parallel()
	cfg := oauth.Config{
		Domain: "example.cybozu.com",
		// ClientID と ClientSecret と RedirectURL が未設定
	}
	_, err := oauth.Login(t.Context(), cfg)
	if err == nil {
		t.Fatal("expected ErrMissingClientCredentials, got nil")
	}
}

// FL-6: callback timeout
func TestLogin_CallbackTimeout(t *testing.T) {
	t.Parallel()
	cfg := oauth.Config{
		Domain:       "example.cybozu.com",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://127.0.0.1:0/callback",
		Timeout:      50 * time.Millisecond, // 短い timeout
		OpenBrowser:  func(_ string) error { return nil }, // ブラウザ起動しない
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err := oauth.Login(ctx, cfg)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// FL-7: scope 未指定 → デフォルト 6 スコープ
func TestLogin_DefaultScopes(t *testing.T) {
	t.Parallel()
	var gotScope string
	cfg := oauth.Config{
		Domain:       "example.cybozu.com",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://127.0.0.1:0/callback",
		// Scopes 未指定
		Timeout: 100 * time.Millisecond,
		OpenBrowser: func(urlStr string) error {
			u, _ := url.Parse(urlStr)
			gotScope = u.Query().Get("scope")
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	_, _ = oauth.Login(ctx, cfg) // timeout 想定

	for _, s := range defaultScopes {
		if !strings.Contains(gotScope, s) {
			t.Errorf("default scope missing: %q in %q", s, gotScope)
		}
	}
}
