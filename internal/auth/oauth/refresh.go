package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// RefresherConfig は Refresher の設定。
type RefresherConfig struct {
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
	HTTPClient    *http.Client
	Now           func() time.Time
	Sleep         func(time.Duration) // リトライ間の sleep（テスト用）
}

// Refresher は refresh_token grant で TokenStore の Token を更新する責務を持つ。
type Refresher struct {
	tokenEndpoint string
	clientID      string
	clientSecret  string
	httpClient    *http.Client
	now           func() time.Time
	sleep         func(time.Duration)
}

// NewRefresher は Refresher を構築する。
func NewRefresher(cfg RefresherConfig) *Refresher {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	sleepFn := cfg.Sleep
	if sleepFn == nil {
		sleepFn = time.Sleep
	}
	return &Refresher{
		tokenEndpoint: cfg.TokenEndpoint,
		clientID:      cfg.ClientID,
		clientSecret:  cfg.ClientSecret,
		httpClient:    httpClient,
		now:           now,
		sleep:         sleepFn,
	}
}

// Refresh は refresh_token grant を実行して新しいトークン情報を返す。
//
// レスポンスに refresh_token が含まれない場合は引数 oldRefreshToken を維持する（rotation 対応）。
// invalid_grant のとき ErrRefreshTokenRevoked を wrap して返す。
func (r *Refresher) Refresh(ctx context.Context, oldRefreshToken string) (*Result, error) {
	tokenResp, err := RefreshToken(ctx, RefreshTokenRequest{
		TokenEndpoint: r.tokenEndpoint,
		ClientID:      r.clientID,
		ClientSecret:  r.clientSecret,
		RefreshToken:  oldRefreshToken,
		HTTPClient:    r.httpClient,
		Now:           r.now,
		Sleep:         r.sleep,
	})
	if err != nil {
		// invalid_grant → ErrRefreshTokenRevoked
		var oauthErr *OAuthError
		if errors.As(err, &oauthErr) && oauthErr.Code == "invalid_grant" {
			return nil, fmt.Errorf("%w: %v", ErrRefreshTokenRevoked, err)
		}
		return nil, err
	}

	// refresh_token が空なら旧値を維持（rotation なし）
	refreshToken := tokenResp.RefreshToken
	if refreshToken == "" {
		refreshToken = oldRefreshToken
	}

	return &Result{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    tokenResp.ExpiresAt,
		ExpiresIn:    tokenResp.ExpiresIn,
		Scope:        tokenResp.Scope,
	}, nil
}
