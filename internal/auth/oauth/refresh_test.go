package oauth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth/oauth"
)

// RF-1: refresh_token で新 access_token を取得できること。
func TestRefresher_Refresh_Success(t *testing.T) {
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

	refresher := oauth.NewRefresher(oauth.RefresherConfig{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})

	result, err := refresher.Refresh(t.Context(), "old-refresh-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AccessToken != "new-access-token" {
		t.Errorf("access_token: got %q", result.AccessToken)
	}
	if result.RefreshToken != "new-refresh-token" {
		t.Errorf("refresh_token: got %q", result.RefreshToken)
	}
}

// RF-2: レスポンスに refresh_token なし → 引数 oldRefresh を維持
func TestRefresher_Refresh_NoNewRefreshToken(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access-token",
			"expires_in":   3600,
			// refresh_token フィールドなし
		})
	}))
	defer srv.Close()

	refresher := oauth.NewRefresher(oauth.RefresherConfig{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})

	result, err := refresher.Refresh(t.Context(), "old-refresh-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RefreshToken != "old-refresh-token" {
		t.Errorf("refresh_token: got %q, want old-refresh-token", result.RefreshToken)
	}
}

// RF-3: invalid_grant → ErrRefreshTokenRevoked
func TestRefresher_Refresh_InvalidGrant(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "invalid_grant",
		})
	}))
	defer srv.Close()

	refresher := oauth.NewRefresher(oauth.RefresherConfig{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})

	_, err := refresher.Refresh(t.Context(), "revoked-refresh-token")
	if err == nil {
		t.Fatal("expected ErrRefreshTokenRevoked, got nil")
	}
	if !errors.Is(err, oauth.ErrRefreshTokenRevoked) {
		t.Errorf("expected ErrRefreshTokenRevoked, got: %v", err)
	}
}

// RF-4: 5xx → 1 回リトライ後 *OAuthError
func TestRefresher_Refresh_ServerError_Retry(t *testing.T) {
	t.Parallel()
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "server_error"})
	}))
	defer srv.Close()

	refresher := oauth.NewRefresher(oauth.RefresherConfig{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
		Sleep:         func(_ time.Duration) {},
	})

	_, err := refresher.Refresh(t.Context(), "token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

// RF-5: client_id/secret が form / Basic Auth に乗ること
func TestRefresher_Refresh_BasicAuth(t *testing.T) {
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

	refresher := oauth.NewRefresher(oauth.RefresherConfig{
		TokenEndpoint: srv.URL + "/oauth2/token",
		ClientID:      "my-client",
		ClientSecret:  "my-secret",
		HTTPClient:    srv.Client(),
		Now:           func() time.Time { return time.Unix(1000, 0) },
	})

	_, err := refresher.Refresh(t.Context(), "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotUser != "my-client" {
		t.Errorf("Basic Auth user: got %q", gotUser)
	}
	if gotPass != "my-secret" {
		t.Errorf("Basic Auth pass: got %q", gotPass)
	}
}
