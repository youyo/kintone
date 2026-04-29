// Package oauth は kintone OAuth 2.0 (Authorization Code Grant + PKCE) を実装する。
//
// 主要コンポーネント:
//   - PKCE 生成 (pkce.go)
//   - state 生成・検証 (state.go)
//   - token endpoint クライアント (token.go)
//   - loopback callback サーバ (callback.go)
//   - Authorization Code Grant フロー (flow.go)
//   - refresh_token grant + 自動更新 Authenticator (refresh.go, provider.go)
//   - クロスプラットフォームブラウザ起動 (browser.go)
package oauth

import (
	"errors"
	"fmt"
)

// sentinel エラー群。
var (
	// ErrStateMismatch は callback の state パラメータが期待値と不一致のとき。
	ErrStateMismatch = errors.New("oauth: state mismatch")
	// ErrAuthorizationCodeMissing は callback に code パラメータがないとき。
	ErrAuthorizationCodeMissing = errors.New("oauth: authorization code missing")
	// ErrCallbackTimeout は loopback サーバが timeout 内に callback を受信できなかったとき。
	ErrCallbackTimeout = errors.New("oauth: callback timeout")
	// ErrRefreshTokenRevoked は refresh_token が無効（invalid_grant）のとき。
	ErrRefreshTokenRevoked = errors.New("oauth: refresh token revoked")
	// ErrTokenExpired は access_token が期限切れで refresh_token もないとき。
	ErrTokenExpired = errors.New("oauth: access token expired and no refresh token")
	// ErrInvalidRedirectURL は redirect_url が loopback http でないとき。
	ErrInvalidRedirectURL = errors.New("oauth: redirect URL must be loopback http://127.0.0.1:<port>/callback")
	// ErrMissingClientCredentials は client_id / client_secret / redirect_url のいずれかが未設定のとき。
	ErrMissingClientCredentials = errors.New("oauth: client_id / client_secret / redirect_url must be set")
)

// OAuthError は kintone OAuth エンドポイントからのエラーレスポンスを表す。
type OAuthError struct {
	Code        string // "invalid_grant" / "invalid_request" / ...
	Description string
	HTTPStatus  int
}

// Error は人間可読なエラーメッセージを返す。
func (e *OAuthError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("oauth: provider error %d: %s: %s", e.HTTPStatus, e.Code, e.Description)
	}
	return fmt.Sprintf("oauth: provider error %d: %s", e.HTTPStatus, e.Code)
}
