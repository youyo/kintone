package auth

import (
	"errors"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/output"
)

// mapOAuthError は OAuth 関連エラーを *output.Error に変換する。
// cli.MapToOutputError と同じロジックを auth パッケージから再利用できるよう
// 最小限の変換のみを実装する（循環依存回避のため cli パッケージを参照しない）。
func mapOAuthError(err error) *output.Error {
	if err == nil {
		return nil
	}

	// sentinel エラー（M14 で loopback 関連の sentinel は削除済み。
	// 残るのは refresh / token expiry のみ）
	if errors.Is(err, oauth.ErrRefreshTokenRevoked) {
		return &output.Error{Code: "OAUTH_REFRESH_REVOKED", Message: err.Error()}
	}
	if errors.Is(err, oauth.ErrTokenExpired) {
		return &output.Error{Code: "KINTONE_UNAUTHORIZED", Message: err.Error()}
	}

	// *OAuthError
	var oauthErr *oauth.OAuthError
	if errors.As(err, &oauthErr) {
		return &output.Error{
			Code:    "OAUTH_PROVIDER_ERROR",
			Message: oauthErr.Error(),
			Details: map[string]any{
				"provider_code": oauthErr.Code,
				"http_status":   oauthErr.HTTPStatus,
			},
		}
	}

	// UsageError
	var ue *clierr.UsageError
	if errors.As(err, &ue) {
		return &output.Error{Code: "USAGE", Message: ue.Error()}
	}

	return &output.Error{Code: "INTERNAL", Message: err.Error()}
}
