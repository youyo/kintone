package api

import (
	"github.com/spf13/cobra"
)

// NewCmd は `kintone api` サブコマンドツリーを構築する。
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "kintone REST API を直接叩く透過コマンド群",
		Long: `kintone REST API を薄く透過する CLI 群です。
LLM / スクリプトから JSON で結果を受け取れます。

サブコマンド:
  records  get      レコード一覧取得
  record   get      レコード単件取得
  app      get      アプリ情報取得
  app      fields   フィールド定義取得
  app      describe app + fields 合成（operations 経由）`,
	}
	cmd.AddCommand(newRecordsCmd())
	cmd.AddCommand(newRecordCmd())
	cmd.AddCommand(newAppCmd())
	return cmd
}
