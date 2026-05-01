package mcp

import (
	"context"
	"net/http"

	idp "github.com/youyo/kintone/internal/idproxy"
	mcpserver "github.com/youyo/kintone/internal/mcp/server"
	"github.com/youyo/kintone/internal/store"
)

// buildOIDCMiddleware は idproxy v0.4.2 を構築し、Wrap + PrincipalMiddleware を合成して返す。
//
// 環境変数（KINTONE_MCP_OIDC_*, KINTONE_MCP_EXTERNAL_URL, KINTONE_MCP_COOKIE_SECRET 等）から
// idproxy.Env を読み Validate → BuildAuth する。失敗時はエラーを上位に返し、
// runServe で SilenceUsage に従い CLI が exit する。
func buildOIDCMiddleware(ctx context.Context, authMode, authZMode string) (mcpserver.MiddlewareFunc, error) {
	env := idp.LoadEnvFromOS()
	if err := env.Validate(); err != nil {
		return nil, err
	}
	container := store.ContainerFromContext(ctx)
	auth, err := idp.BuildAuth(ctx, &env, authMode, authZMode, container)
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
