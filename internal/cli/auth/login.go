package auth

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/tokenstore"
)

// loginFn は oauth.Login の差し替え可能な hook（テスト時にモック注入）。
var loginFn = func(ctx context.Context, cfg oauth.Config) (*oauth.Result, error) {
	return oauth.Login(ctx, cfg)
}

// SetLoginFn は loginFn を差し替える（テスト専用）。
func SetLoginFn(fn func(ctx context.Context, cfg oauth.Config) (*oauth.Result, error)) {
	loginFn = fn
}

// ResetLoginFn は loginFn をデフォルトに戻す（テスト専用）。
func ResetLoginFn() {
	loginFn = func(ctx context.Context, cfg oauth.Config) (*oauth.Result, error) {
		return oauth.Login(ctx, cfg)
	}
}

// loginResult は `kintone auth login` の成功時 data 部分。
type loginResult struct {
	PrincipalID string `json:"principal_id"`
	Domain      string `json:"domain"`
	ExpiresAt   string `json:"expires_at"` // RFC3339
	Scope       string `json:"scope"`
}

// newLoginCmd は `kintone auth login` コマンドを構築する。
func newLoginCmd() *cobra.Command {
	var oauthFlag bool
	var principalID string
	var noBrowser bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "login",
		Short: "kintone への認証ログインを実行する",
		Long: `OAuth 2.0 (Authorization Code Grant + PKCE) でログインし、
アクセストークンを TokenStore に保存します。

使用例:
  kintone auth login --oauth --principal-id oauth:alice

必要な環境変数:
  KINTONE_DOMAIN              kintone ドメイン（例: example.cybozu.com）
  KINTONE_OAUTH_CLIENT_ID     OAuth クライアント ID
  KINTONE_OAUTH_CLIENT_SECRET OAuth クライアントシークレット
  KINTONE_OAUTH_REDIRECT_URL  コールバック URL（例: http://127.0.0.1:8080/callback）`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd.Context(), cmd.OutOrStdout(), oauthFlag, principalID, noBrowser, timeout)
		},
	}

	cmd.Flags().BoolVar(&oauthFlag, "oauth", false, "OAuth 2.0 フローでログインする（M09 では必須）")
	cmd.Flags().StringVar(&principalID, "principal-id", "", "TokenStore のキー識別子（例: oauth:alice）")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "ブラウザを起動せず authorize URL を stderr に出力する")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "callback 待ち上限時間")

	return cmd
}

// runLogin は login コマンドの本体ロジック。
// テスト時は ExecuteLoginWith 経由で呼ばれる。
func runLogin(ctx context.Context, out io.Writer, oauthFlag bool, principalID string, noBrowser bool, timeout time.Duration) error {
	// --oauth が必須（M09 では OAuth のみサポート）
	if !oauthFlag {
		return &clierr.UsageError{Msg: "oauth: --oauth フラグが必要です（M09 では --oauth を指定してください）"}
	}

	// --principal-id 必須
	if principalID == "" {
		return &clierr.UsageError{Msg: "oauth: --principal-id が必要です（例: oauth:alice）"}
	}

	// config 読み込み
	r, err := config.Load(config.LoadOptions{})
	if err != nil {
		return err
	}

	// 必須フィールド検証
	if r.Domain == "" || r.OAuthClientID == "" || r.OAuthClientSecret == "" || r.OAuthRedirectURL == "" {
		return oauth.ErrMissingClientCredentials
	}

	// TokenStore を開く
	store, err := openTokenStoreFn()
	if err != nil {
		return fmt.Errorf("oauth: open tokenstore: %w", err)
	}
	defer func() { _ = store.Close() }()

	// OAuth Login フロー
	cfg := oauth.Config{
		Domain:       r.Domain,
		ClientID:     r.OAuthClientID,
		ClientSecret: r.OAuthClientSecret,
		RedirectURL:  r.OAuthRedirectURL,
		Scopes:       r.OAuthScopes,
		Timeout:      timeout,
		NoBrowser:    noBrowser,
	}

	result, err := loginFn(ctx, cfg)
	if err != nil {
		return err
	}

	// TokenStore に保存
	tok := tokenstore.Token{
		Domain:       r.Domain,
		PrincipalID:  principalID,
		AuthType:     tokenstore.AuthTypeOAuth,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    result.ExpiresAt,
	}
	if putErr := store.Put(ctx, tok); putErr != nil {
		return fmt.Errorf("oauth: store token: %w", putErr)
	}

	// 成功 JSON 出力
	payload, err := output.Success(loginResult{
		PrincipalID: principalID,
		Domain:      r.Domain,
		ExpiresAt:   result.ExpiresAt.Format(time.RFC3339),
		Scope:       result.Scope,
	})
	if err != nil {
		return err
	}
	return output.Write(out, payload)
}

// ExecuteLoginWith はテスト用エントリポイント。
// args / out / errOut を差し替え可能にし、エラー時は output.Failure を out に書く。
func ExecuteLoginWith(args []string, out, errOut io.Writer) error {
	cmd := newLoginCmd()
	cmd.SetArgs(args)
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	if err := cmd.Execute(); err != nil {
		oe := mapOAuthError(err)
		payload, _ := output.Failure(oe)
		_ = output.Write(out, payload)
		return err
	}
	return nil
}
