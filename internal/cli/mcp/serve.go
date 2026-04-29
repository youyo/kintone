package mcp

import (
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
  serve   stdio JSON-RPC で MCP サーバーを起動する`,
	}
	cmd.AddCommand(newServeCmd())
	return cmd
}

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "MCP サーバーを stdio で起動する",
		Long: `MCP サーバーを stdio JSON-RPC モードで起動します。

Claude Desktop 等の MCP クライアントから子プロセスとして起動し、
標準入出力経由でツール（apps_search / app_describe / records_query /
record_create / record_update / record_delete）を提供します。

設定（domain / api-token）は ~/.config/kintone/config.toml または
KINTONE_* 環境変数から読み込みます。`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			srv := mcpserver.New(api)
			return mcpserver.ServeStdio(srv)
		},
	}
}
