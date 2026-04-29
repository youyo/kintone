package api

import (
	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/service/operations"
)

// newRecordsCmd は `kintone api records` ツリーを構築する。
func newRecordsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "records",
		Short: "レコード一覧操作",
	}
	cmd.AddCommand(newRecordsGetCmd())
	return cmd
}

// newRecordsGetCmd は `kintone api records get` を構築する。
//
// フラグ:
//
//	--app          int64    必須（kintone アプリ ID）
//	--query        string   任意（kintone クエリ言語）
//	--field        []string 任意・複数指定可（--field a --field b）
//	--total-count  bool     任意（true で total_count を返す）
//
// 出力例:
//
//	{"ok":true,"data":{"records":[...],"total_count":3}}
func newRecordsGetCmd() *cobra.Command {
	var (
		app        int64
		query      string
		fields     []string
		totalCount bool
	)
	cmd := &cobra.Command{
		Use:   "get",
		Short: "レコード一覧を取得する",
		Long:  "GET /k/v1/records.json を呼び、records と total_count を JSON で返します。",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			out, err := operations.RecordsQuery(cmd.Context(), a, operations.RecordsQueryInput{
				App:        app,
				Query:      query,
				Fields:     fields,
				TotalCount: totalCount,
			})
			if err != nil {
				return err
			}
			payload, err := output.Success(out)
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	cmd.Flags().StringVar(&query, "query", "", "kintone クエリ言語")
	cmd.Flags().StringArrayVar(&fields, "field", nil, "取得するフィールドコード（複数指定可: --field a --field b）")
	cmd.Flags().BoolVar(&totalCount, "total-count", false, "total_count を含める")
	_ = cmd.MarkFlagRequired("app")
	return cmd
}
