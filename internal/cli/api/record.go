package api

import (
	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
)

// recordGetData は `record get` の data 部分。
// kintoneapi 生レスポンスをそのまま転載（フィールド構造は kintone の dynamic 型のため）。
type recordGetData struct {
	Record map[string]any `json:"record"`
}

// newRecordCmd は `kintone api record` ツリーを構築する（現状は get のみ）。
func newRecordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record",
		Short: "レコード単件操作",
	}
	cmd.AddCommand(newRecordGetCmd())
	return cmd
}

// newRecordGetCmd は `kintone api record get` を構築する。
//
// フラグ:
//
//	--app  int64  必須
//	--id   int64  必須
//
// 出力例:
//
//	{"ok":true,"data":{"record":{...}}}
func newRecordGetCmd() *cobra.Command {
	var app, id int64
	cmd := &cobra.Command{
		Use:   "get",
		Short: "レコード単件を取得する",
		Long:  "GET /k/v1/record.json を呼び、record を JSON で返します。",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			resp, err := a.GetRecord(cmd.Context(), kintoneapi.GetRecordRequest{App: app, ID: id})
			if err != nil {
				return err
			}
			payload, err := output.Success(recordGetData{Record: resp.Record})
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	cmd.Flags().Int64Var(&id, "id", 0, "レコード ID（必須）")
	_ = cmd.MarkFlagRequired("app")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
