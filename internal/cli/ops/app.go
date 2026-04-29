package ops

import (
	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/service/operations"
)

// newAppCmd は `kintone ops app` ツリーを構築する（describe のみ）。
func newAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "アプリ情報の合成記述（LLM 向け）",
	}
	cmd.AddCommand(newAppDescribeCmd())
	return cmd
}

// newAppDescribeCmd は `kintone ops app describe` を構築する。
//
// 仕様書「CLI ops」配下にも describe を露出させる（LLM が `ops` 名前空間で発見できるように）。
// 実装は operations.AppDescribe を呼ぶだけの薄い wrapper（M04 の `kintone api app describe` と
// 同等の出力。両者を独立に保つ設計判断は plans/kintone-m05-cli-ops-write.md 参照）。
func newAppDescribeCmd() *cobra.Command {
	var (
		app  int64
		lang string
	)
	cmd := &cobra.Command{
		Use:   "describe",
		Short: "アプリ情報とフィールド定義を合成して返す",
		Long: `operations.AppDescribe を呼び、app + fields を 1 つの JSON にまとめて返します。
LLM 向けにフィールド名は snake_case で統一されています。`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			out, err := operations.AppDescribe(cmd.Context(), a, operations.AppDescribeInput{
				App: app, Lang: lang,
			})
			if err != nil {
				return err
			}
			return writeJSON(cmd, out)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	cmd.Flags().StringVar(&lang, "lang", "", "表示言語（ja/en/zh/user/default）")
	_ = cmd.MarkFlagRequired("app")
	return cmd
}
