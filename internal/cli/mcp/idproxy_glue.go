package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	upstream "github.com/youyo/idproxy"

	idp "github.com/youyo/kintone/internal/idproxy"
	mcpserver "github.com/youyo/kintone/internal/mcp/server"
	"github.com/youyo/kintone/internal/store"
)

// buildOIDCMiddleware は idproxy v0.5.0 を構築し、Wrap + PrincipalMiddleware を合成して返す。
//
// 環境変数（KINTONE_MCP_OIDC_*, KINTONE_MCP_EXTERNAL_URL, KINTONE_MCP_COOKIE_SECRET 等）から
// idproxy.Env を読み Validate → BuildAuth する。失敗時はエラーを上位に返し、
// runServe で SilenceUsage に従い CLI が exit する。
//
// hook が non-nil の場合、idproxy の OnAuthenticated フックとして設定される（M17）。
func buildOIDCMiddleware(ctx context.Context, authMode, authZMode string, hook func(http.ResponseWriter, *http.Request, *upstream.User) (string, bool)) (mcpserver.MiddlewareFunc, error) {
	env := idp.LoadEnvFromOS()
	if err := env.Validate(); err != nil {
		return nil, err
	}
	container := store.ContainerFromContext(ctx)
	auth, err := idp.BuildAuth(ctx, &env, authMode, authZMode, container, hook)
	if err != nil {
		return nil, err
	}
	// idproxy.Auth.Wrap → PrincipalMiddleware の二段ラップ。
	// Wrap 内で User が context に注入され、PrincipalMiddleware で kintone Principal に変換される。
	mw := func(next http.Handler) http.Handler {
		return auth.Wrap(idp.PrincipalMiddleware(next))
	}
	return mw, nil
}

// buildOnAuthenticatedHook は OnAuthenticated フックを構築する。
//
// kill switch（KINTONE_MCP_DISABLE_OAUTH_CASCADE=1）と tokens==nil で nil を返す。
//
// startURL は絶対 URL（例: "https://host/base/oauth/kintone/start"）を受け取り、
// url.Parse(startURL).Path でパス部分（"/base/oauth/kintone/start"）を抽出して返す。
// これにより StrictPostLoginRedirectValidator（相対パス OK）に通りつつ、
// KINTONE_MCP_EXTERNAL_URL にサブパスが含まれる配備でも正しく動作する。
func buildOnAuthenticatedHook(tokens store.TokenStore, domain, startURL string) func(http.ResponseWriter, *http.Request, *upstream.User) (string, bool) {
	if tokens == nil || os.Getenv("KINTONE_MCP_DISABLE_OAUTH_CASCADE") == "1" {
		return nil
	}
	// startURL からパス部分だけを抽出（例: "/base/oauth/kintone/start"）。
	// url.Parse が失敗した場合は "/oauth/kintone/start" にフォールバックする。
	startPath := "/oauth/kintone/start"
	if u, err := url.Parse(startURL); err == nil && u.Path != "" {
		startPath = u.Path
	}

	return func(w http.ResponseWriter, r *http.Request, user *upstream.User) (string, bool) {
		if user == nil {
			return "", false
		}
		if user.Issuer == "" || user.Subject == "" {
			return "", false
		}
		principalID := user.Issuer + ":" + user.Subject
		_, err := tokens.Get(r.Context(), domain, principalID, store.AuthTypeOAuth)
		if errors.Is(err, store.ErrNotFound) {
			return startPath, false
		}
		if err != nil {
			// 非 ErrNotFound（network 障害等）: safe default に倒すが警告ログを出す
			slog.WarnContext(r.Context(), "idproxy.OnAuthenticated: token lookup failed",
				slog.String("domain", domain),
				slog.String("err_class", "token_lookup"),
			)
		}
		return "", false
	}
}
