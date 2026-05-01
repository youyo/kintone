package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	mcpserver "github.com/youyo/kintone/internal/mcp/server"
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
		return err
	}

	api, err := buildAPI(cmd)
	if err != nil {
		return err
	}
	srv := mcpserver.New(api)

	switch serveMode {
	case mcpserver.ServeModeStdio:
		return mcpserver.ServeStdio(srv)
	case mcpserver.ServeModeHTTP:
		return runHTTP(cmd.Context(), srv, listenAddr, auth, authz)
	default:
		return fmt.Errorf("mcp serve: unsupported serve mode")
	}
}

// runHTTP は HTTP モードで MCP server を起動する。
//
// auth=oidc の場合は internal/idproxy パッケージで idproxy.Auth.Wrap を構築して挟む。
// SIGINT / SIGTERM で graceful shutdown する。
func runHTTP(ctx context.Context, srv *mcpserver.MCPServer, addr string, auth mcpserver.AuthMode, authz mcpserver.AuthZMode) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	mw, err := buildHTTPMiddleware(ctx, auth, authz)
	if err != nil {
		return err
	}

	return mcpserver.ServeHTTP(ctx, srv, mcpserver.HTTPServeOptions{
		Addr:       addr,
		Middleware: mw,
	})
}

// buildHTTPMiddleware は AuthMode に応じた http middleware を返す。
// idproxy 依存は別ファイル idproxy_glue.go に切り出し、auth=none では idproxy package を実行しない。
func buildHTTPMiddleware(ctx context.Context, auth mcpserver.AuthMode, authz mcpserver.AuthZMode) (mcpserver.MiddlewareFunc, error) {
	switch auth {
	case mcpserver.AuthModeNone:
		return nil, nil // ServeHTTP で no-op に解決される
	case mcpserver.AuthModeOIDC:
		return buildOIDCMiddleware(ctx, string(auth), string(authz))
	default:
		return nil, errors.New("mcp serve: unknown auth mode")
	}
}
