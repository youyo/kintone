// Package auth は kintone CLI の認証関連コマンド（auth login/status/logout）を提供する。
//
// M09: OAuth 2.0 (Authorization Code Grant + PKCE) のログイン/ステータス/ログアウト。
// M10 以降: idproxy / multi-user remote MCP への拡張を予定。
package auth

import (
	"github.com/spf13/cobra"
)

// NewCmd は `kintone auth` 親コマンドを構築する。
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "認証情報を管理する",
		Long:  "auth login / status / logout コマンドで OAuth 認証情報を管理します。",
	}
	cmd.AddCommand(newLoginCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newLogoutCmd())
	return cmd
}
