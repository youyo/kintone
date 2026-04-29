package cache

import (
	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/output"
)

// scopePrefixMap は --scope 値をキャッシュキーの prefix に変換するマップ。
//
// キャッシュキーは必ず "v1:" で始まる（バージョンプレフィックス / 計画書 line 81）。
var scopePrefixMap = map[string]string{
	"apps":      "v1:app:",
	"fields":    "v1:fields:",
	"list_apps": "v1:list_apps:",
	"all":       "v1:",
}

// clearResult は `kintone cache clear` の JSON 出力形式。
type clearResult struct {
	Scope   string `json:"scope"`
	Deleted int    `json:"deleted"`
}

// newClearCmd は `kintone cache clear` コマンドを構築する。
func newClearCmd() *cobra.Command {
	var scope string

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "キャッシュエントリを削除する",
		Long: `キャッシュエントリを --scope で指定した範囲で削除します。

スコープ:
  apps       アプリ情報キャッシュ（v1:app: prefix）
  fields     フィールド定義キャッシュ（v1:fields: prefix）
  list_apps  アプリ一覧キャッシュ（v1:list_apps: prefix）
  all        全キャッシュ（デフォルト）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// scope バリデーション（advisor 加筆 C）
			prefix, ok := scopePrefixMap[scope]
			if !ok {
				return clierr.NewUsageError("--scope must be one of: apps, fields, list_apps, all")
			}

			store, exists, err := NewStoreBuilder()
			if err != nil {
				return err
			}

			// DB 不在時は何もせず deleted=0 を返す（計画書 line 475）
			if !exists || store == nil {
				payload, err := output.Success(clearResult{Scope: scope, Deleted: 0})
				if err != nil {
					return err
				}
				return output.Write(cmd.OutOrStdout(), payload)
			}
			defer func() { _ = store.Close() }()

			deleted, err := store.DeleteByPrefix(cmd.Context(), prefix)
			if err != nil {
				return err
			}

			payload, err := output.Success(clearResult{Scope: scope, Deleted: deleted})
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "all", "削除スコープ: apps|fields|list_apps|all")
	return cmd
}
