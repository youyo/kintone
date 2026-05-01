// Package store は kintone store サブコマンド (init 等) を提供する。
package store

import "github.com/spf13/cobra"

// NewCmd は "kintone store" サブコマンドのルートを返す。
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Storage バックエンドの管理",
		Long:  "kintone CLI/MCP の永続化層 (DynamoDB/Redis/SQLite) を初期化・検証するコマンド群",
	}
	cmd.AddCommand(newInitCmd())
	return cmd
}
