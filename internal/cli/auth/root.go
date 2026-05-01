// Package auth は kintone CLI の認証情報管理コマンド（auth status/logout）を提供する。
//
// 認証モデル:
//   - ローカル CLI 実行: API Token のみ（config.toml / 環境変数 / フラグで指定）
//   - リモート MCP サーバ: OAuth（M13 でサーバ側 callback を実装予定）
//
// kintone OAuth は redirect_uri に https を強制する（loopback http 不可）ため、
// CLI からのインタラクティブ OAuth ログインは廃止された。サーバ側ホスト型の
// OAuth フローのみサポートする。auth status/logout は TokenStore 内の
// OAuth トークンの参照・削除に用いる。
package auth

import (
	"github.com/spf13/cobra"
)

// NewCmd は `kintone auth` 親コマンドを構築する。
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "認証情報を管理する",
		Long:  "auth status / logout コマンドで TokenStore 内の OAuth トークンを管理します。CLI ログインは廃止されました（kintone OAuth は https redirect 必須のため、リモート MCP サーバ経由でのみ取得できます）。",
	}
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newLogoutCmd())
	return cmd
}
