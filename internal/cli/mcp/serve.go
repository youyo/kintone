package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	upstream "github.com/youyo/idproxy"

	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/mcp/facade"
	mcpserver "github.com/youyo/kintone/internal/mcp/server"
	serviceapi "github.com/youyo/kintone/internal/service/api"
	"github.com/youyo/kintone/internal/store"
)

// NewCmd は `kintone mcp` サブコマンドツリーを構築する。
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP サーバー関連コマンド",
		Long: `kintone を MCP サーバーとして起動するためのコマンド群です。

サブコマンド:
  serve   stdio または HTTP/Streamable で MCP サーバーを起動する`,
	}
	cmd.AddCommand(newServeCmd())
	return cmd
}

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "MCP サーバーを stdio または HTTP で起動する",
		Long: `MCP サーバーを起動します。

モード:
  --listen 未指定           stdio JSON-RPC（既定。Claude Desktop 等の子プロセス起動向け）
  --listen :8080            HTTP/Streamable で /mcp を公開（remote MCP）

認証:
  --auth none|oidc          リクエスト認証（既定: none）。oidc は idproxy v0.4.2 連携
  --authz api-token|oauth   upstream kintone への認証方式（既定: api-token）

設定（domain / api-token）は ~/.config/kintone/config.toml または KINTONE_* 環境変数から読み込みます。
auth=oidc を使う場合は別途 KINTONE_MCP_OIDC_ISSUER / KINTONE_MCP_OIDC_CLIENT_ID /
KINTONE_MCP_EXTERNAL_URL / KINTONE_MCP_COOKIE_SECRET 等が必要です。`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runServe,
	}
	cmd.Flags().String("listen", os.Getenv("KINTONE_MCP_LISTEN_ADDR"), "HTTP リッスン address（host:port）。空で stdio")
	cmd.Flags().String("auth", os.Getenv("KINTONE_MCP_AUTH_MODE"), "認証モード: none / oidc")
	cmd.Flags().String("authz", os.Getenv("KINTONE_MCP_AUTHZ_MODE"), "認可モード: api-token / oauth")
	return cmd
}

func runServe(cmd *cobra.Command, _ []string) error {
	listenAddr, _ := cmd.Flags().GetString("listen")
	authStr, _ := cmd.Flags().GetString("auth")
	authzStr, _ := cmd.Flags().GetString("authz")

	auth, err := mcpserver.ParseAuthMode(authStr)
	if err != nil {
		return err
	}
	authz, err := mcpserver.ParseAuthZMode(authzStr)
	if err != nil {
		return err
	}
	serveMode := mcpserver.PickServeMode(listenAddr)
	if err := mcpserver.ValidateModes(serveMode, auth, authz); err != nil {
		// stdio + authz=oauth は server 層で typed sentinel として返るので、CLI 層で
		// 復旧手順を含む UsageError にラップし `USAGE` envelope に分類されるようにする（M15）。
		if errors.Is(err, mcpserver.ErrStdioOAuthUnsupported) {
			return clierr.NewUsageError(
				"mcp serve: authz=oauth is not supported on stdio transport " +
					"(stdio runs in a single-user process; OAuth requires per-request " +
					"principal binding which is only available with --listen for HTTP). " +
					"Fix: drop --authz=oauth (or unset KINTONE_MCP_AUTHZ_MODE) to use API Token, " +
					"or specify --listen <addr> --auth oidc --authz oauth for multi-user HTTP mode.",
			)
		}
		if errors.Is(err, mcpserver.ErrHTTPNoneOAuthUnsupported) {
			return clierr.NewUsageError(
				"mcp serve: authz=oauth requires auth=oidc " +
					"(auth=none provides no Principal injection, so /oauth/kintone/start always returns 401). " +
					"Fix: add --auth oidc (and set KINTONE_MCP_OIDC_ISSUER / KINTONE_MCP_OIDC_CLIENT_ID etc.), " +
					"or drop --authz=oauth to use API Token instead.",
			)
		}
		return err
	}

	// HTTP + authz=oauth では PrincipalAPIFactory が per-request にユーザー別 token から
	// API client を生成するため、起動時の固定 API client は不要かつ誤動作の原因。
	// このパスでは buildAPI を skip し、runHTTP に nil を渡して OAuth setup に委譲する（M15）。
	skipBuildAPI := serveMode == mcpserver.ServeModeHTTP && authz == mcpserver.AuthZModeOAuth

	var api serviceapi.API
	if !skipBuildAPI {
		built, err := buildAPI(cmd)
		if err != nil {
			return err
		}
		api = built
	}

	switch serveMode {
	case mcpserver.ServeModeStdio:
		srv := mcpserver.New(api)
		return mcpserver.ServeStdio(srv)
	case mcpserver.ServeModeHTTP:
		// --profile / --config を反映して再 Load し、runHTTP に渡す（OAuth setup で参照）
		cli := readCLIConfig(cmd)
		resolved, err := config.Load(config.LoadOptions{CLI: cli})
		if err != nil {
			return err
		}
		return runHTTP(cmd.Context(), api, resolved, listenAddr, auth, authz)
	default:
		return fmt.Errorf("mcp serve: unsupported serve mode")
	}
}

// runHTTP は HTTP モードで MCP server を起動する。
//
// auth=oidc の場合は internal/idproxy パッケージで idproxy.Auth.Wrap を構築して挟む。
// authz=oauth の場合は PrincipalAPIFactory + OAuth callback handler を組み立て、
// /oauth/kintone/start, /callback を ExtraRoutes として登録する（M13）。
// SIGINT / SIGTERM で graceful shutdown する。
func runHTTP(ctx context.Context, api serviceapi.API, resolved *config.Resolved, addr string, auth mcpserver.AuthMode, authz mcpserver.AuthZMode) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// AuthZ=oauth では PrincipalAPIFactory と OAuth callback handler を組み立てる（M13）。
	// setup を先に構築することで M17 OnAuthenticated フックに tokens と domain を渡せる。
	container := store.ContainerFromContext(ctx)
	setup, err := buildOAuthSetup(ctx, resolved, container, authz)
	if err != nil {
		return err
	}

	// M17: auth=oidc + authz=oauth のとき、idproxy OnAuthenticated フックを構築する。
	// setup != nil（authz=oauth）かつ auth=oidc の場合のみ hook を生成する。
	// authz=api-token（setup==nil）では hook も nil → 既存動作維持。
	var hook func(http.ResponseWriter, *http.Request, *upstream.User) (string, bool)
	if setup != nil {
		// setup リソースを buildHTTPMiddleware より前に defer することで、
		// buildHTTPMiddleware がエラーを返した場合でも closeStates が確実に呼ばれる。
		defer setup.closeStates()
		if auth == mcpserver.AuthModeOIDC {
			// setup.StartURL を渡してサブパス配備（KINTONE_MCP_EXTERNAL_URL=https://host/base）でも
			// 正しいパスに redirect されるようにする。hook 内で url.Parse(startURL).Path を使う。
			hook = buildOnAuthenticatedHook(setup.Tokens, resolved.Domain, setup.StartURL)
		}
	}

	mw, err := buildHTTPMiddleware(ctx, auth, authz, hook)
	if err != nil {
		return err
	}

	// 不変条件（M15）:
	//   - authz=api-token: api != nil（runServe で buildAPI を必ず呼ぶ）
	//   - authz=oauth     : api == nil & setup != nil（PrincipalAPIFactory が deps を提供）
	// この invariant により、deps.API が nil で NewWithDeps に渡るのは setup が deps を
	// 上書きする場合のみ。将来 wiring が変更されても fail-fast できるよう defensive に検証する。
	deps := facade.ToolDeps{}
	if api != nil {
		deps.API = api
	}
	var extraRoutes []mcpserver.RouteEntry
	if setup != nil {
		deps = setup.Deps
		extraRoutes = setup.ExtraRoutes

		// auth=oidc + authz=oauth のとき cascade middleware を OIDC middleware の内側に合成する（M16）。
		// 合成順: idproxy.Auth.Wrap → PrincipalMiddleware → EnsureKintoneOAuthConnected → mux
		// mw が既に (idproxy.Wrap + PrincipalMiddleware) を合成済みなので、
		// cascade を inner とした新しい mw を組み立てる。
		// M17 では OnAuthenticated フックが認証完了直後にカスケードを行うため、
		// per-request EnsureKintoneOAuthConnected はフォールバックとして温存する。
		if mw != nil && auth == mcpserver.AuthModeOIDC {
			cascade := EnsureKintoneOAuthConnected(setup.Tokens, resolved.Domain, setup.StartURL)
			outer := mw
			mw = func(h http.Handler) http.Handler {
				return outer(cascade(h))
			}
		}
	}
	if deps.API == nil && deps.Factory == nil {
		return errors.New("mcp serve: internal error - neither API nor Factory configured (wiring bug)")
	}
	srv := mcpserver.NewWithDeps(deps)

	return mcpserver.ServeHTTP(ctx, srv, mcpserver.HTTPServeOptions{
		Addr:        addr,
		Middleware:  mw,
		ExtraRoutes: extraRoutes,
	})
}

// buildHTTPMiddleware は AuthMode に応じた http middleware を返す。
// idproxy 依存は別ファイル idproxy_glue.go に切り出し、auth=none では idproxy package を実行しない。
//
// hook は idproxy v0.5.0 の OnAuthenticated フック（M17）。nil でも動作する。
func buildHTTPMiddleware(ctx context.Context, auth mcpserver.AuthMode, authz mcpserver.AuthZMode, hook func(http.ResponseWriter, *http.Request, *upstream.User) (string, bool)) (mcpserver.MiddlewareFunc, error) {
	switch auth {
	case mcpserver.AuthModeNone:
		return nil, nil // ServeHTTP で no-op に解決される
	case mcpserver.AuthModeOIDC:
		return buildOIDCMiddleware(ctx, string(auth), string(authz), hook)
	default:
		return nil, errors.New("mcp serve: unknown auth mode")
	}
}
