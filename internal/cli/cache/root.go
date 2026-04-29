package cache

import "github.com/spf13/cobra"

// NewCmd は `kintone cache` 親コマンドを構築する。
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "kintone CLI のキャッシュを管理する",
		Long: `kintone CLI が利用する SQLite キャッシュを管理します。

サブコマンド:
  stats   キャッシュ統計を JSON で出力
  clear   キャッシュエントリを削除`,
	}
	cmd.AddCommand(newStatsCmd())
	cmd.AddCommand(newClearCmd())
	return cmd
}
