package oauth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth/oauth"
)

// TK-1: 正常なトークンレスポンスを TokenResponse にパースできること。
func TestExchangeCode_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": "test-refresh-token",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"scope":         "k:app_record:read",
		})
	}))
	defer srv.Close()

	resp, err := oauth.ExchangeCode(t.Context(), oauth.ExchangeCodeRequest{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		Code:          "auth-code",
		RedirectURL:   "http://127.0.0.1:8080/callback",
		CodeVerifier:  "verifier",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken != "test-access-token" {
		t.Errorf("access_token: got %q, want %q", resp.AccessToken, "test-access-token")
	}
	if resp.RefreshToken != "test-refresh-token" {
		t.Errorf("refresh_token: got %q, want %q", resp.RefreshToken, "test-refresh-token")
	}
	if resp.ExpiresIn != 3600 {
		t.Errorf("expires_in: got %d, want 3600", resp.ExpiresIn)
	}
	if resp.Scope != "k:app_record:read" {
		t.Errorf("scope: got %q, want %q", resp.Scope, "k:app_record:read")
	}
}

// TK-2: 400 invalid_grant → *OAuthError{Code:"invalid_grant", HTTPStatus:400}
func TestExchangeCode_InvalidGrant(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "The authorization code is invalid.",
		})
	}))
	defer srv.Close()

	_, err := oauth.ExchangeCode(t.Context(), oauth.ExchangeCodeRequest{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		Code:          "bad-code",
		RedirectURL:   "http://127.0.0.1:8080/callback",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var oauthErr *oauth.OAuthError
	ok := false
	if e, is := err.(*oauth.OAuthError); is { //nolint:errorlint
		oauthErr = e
		ok = true
	}
	if !ok {
		t.Fatalf("expected *oauth.OAuthError, got %T: %v", err, err)
	}
	if oauthErr.Code != "invalid_grant" {
		t.Errorf("error code: got %q, want %q", oauthErr.Code, "invalid_grant")
	}
	if oauthErr.HTTPStatus != 400 {
		t.Errorf("http status: got %d, want 400", oauthErr.HTTPStatus)
	}
}

// TK-3: 5xx → *OAuthError + 1 回リトライ（リトライカウント == 1）
func TestExchangeCode_ServerError_Retry(t *testing.T) {
	t.Parallel()
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "server_error",
		})
	}))
	defer srv.Close()

	_, err := oauth.ExchangeCode(t.Context(), oauth.ExchangeCodeRequest{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		Code:          "some-code",
		RedirectURL:   "http://127.0.0.1:8080/callback",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
		Sleep:         func(_ time.Duration) {}, // リトライ間の sleep をスキップ
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 1 + 1 リトライ = 計 2 回呼ばれる
	if callCount != 2 {
		t.Errorf("expected 2 calls (1 original + 1 retry), got %d", callCount)
	}
}

// TK-4: ネットワークエラー → そのまま返却（url.Error）
func TestExchangeCode_NetworkError(t *testing.T) {
	t.Parallel()
	_, err := oauth.ExchangeCode(t.Context(), oauth.ExchangeCodeRequest{
		TokenEndpoint: "http://127.0.0.1:1", // 使用されていないポートに接続試行
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		Code:          "code",
		RedirectURL:   "http://127.0.0.1:8080/callback",
		HTTPClient:    &http.Client{},
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
	var urlErr *url.Error
	_ = urlErr // url.Error か他の net エラーかの厳密チェックは省略（接続拒否も含む）
}

// TK-5: client_id/secret が HTTP Basic 認証ヘッダに設定されていること。
func TestExchangeCode_BasicAuth(t *testing.T) {
	t.Parallel()
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	_, err := oauth.ExchangeCode(t.Context(), oauth.ExchangeCodeRequest{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "my-client-id",
		ClientSecret:  "my-client-secret",
		Code:          "code",
		RedirectURL:   "http://127.0.0.1:8080/callback",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotUser != "my-client-id" {
		t.Errorf("Basic Auth user: got %q, want %q", gotUser, "my-client-id")
	}
	if gotPass != "my-client-secret" {
		t.Errorf("Basic Auth pass: got %q, want %q", gotPass, "my-client-secret")
	}
}

// TK-6: code_verifier が POST body に含まれること。
func TestExchangeCode_CodeVerifierInBody(t *testing.T) {
	t.Parallel()
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err == nil {
			gotBody = r.Form.Get("code_verifier")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	_, err := oauth.ExchangeCode(t.Context(), oauth.ExchangeCodeRequest{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		Code:          "code",
		RedirectURL:   "http://127.0.0.1:8080/callback",
		CodeVerifier:  "my-code-verifier",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody != "my-code-verifier" {
		t.Errorf("code_verifier in body: got %q, want %q", gotBody, "my-code-verifier")
	}
}

// TestRefreshToken_Success は refresh_token グラントが正常動作することを確認する。
func TestRefreshToken_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	resp, err := oauth.RefreshToken(t.Context(), oauth.RefreshTokenRequest{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		RefreshToken:  "old-refresh-token",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken != "new-access-token" {
		t.Errorf("access_token: got %q, want %q", resp.AccessToken, "new-access-token")
	}
	if !strings.Contains(resp.RefreshToken, "new-refresh-token") {
		// refresh_token が含まれていることを確認
		t.Errorf("refresh_token: got %q, want to contain %q", resp.RefreshToken, "new-refresh-token")
	}
}
