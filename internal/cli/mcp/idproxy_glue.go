package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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
// ⚠️ 戻り値は必ず相対パスにすること。StrictPostLoginRedirectValidator は
// HTTP スキームの絶対 URL を reject するため。
func buildOnAuthenticatedHook(tokens store.TokenStore, domain string) func(http.ResponseWriter, *http.Request, *upstream.User) (string, bool) {
	const kintoneStartPath = "/oauth/kintone/start"

	if tokens == nil || os.Getenv("KINTONE_MCP_DISABLE_OAUTH_CASCADE") == "1" {
		return nil
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
			return kintoneStartPath, false
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
