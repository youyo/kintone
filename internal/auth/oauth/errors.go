// Package oauth は kintone OAuth 2.0 (Authorization Code Grant + PKCE) を実装する。
//
// 主要コンポーネント:
//   - PKCE 生成 (pkce.go)
//   - state 生成（state.go、storage は internal/store の StateStore に委譲）
//   - token endpoint クライアント (token.go)
//   - refresh_token grant + 自動更新 Authenticator (refresh.go, provider.go)
//
// 補足: M14 で OAuth loopback サーバ実装（旧 flow.go / callback.go / browser.go）は
// 物理削除した。kintone OAuth は redirect_uri に HTTPS を強制するため CLI loopback
// フローは成立せず、代わりに MCP サーバホスト型 callback (internal/mcp/oauthcallback)
// を使う。
package oauth

import (
	"errors"
	"fmt"
)

// sentinel エラー群。
var (
	// ErrRefreshTokenRevoked は refresh_token が無効（invalid_grant）のとき。
	ErrRefreshTokenRevoked = errors.New("oauth: refresh token revoked")
	// ErrTokenExpired は access_token が期限切れで refresh_token もないとき。
	ErrTokenExpired = errors.New("oauth: access token expired and no refresh token")
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
