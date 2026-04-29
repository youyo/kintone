package api

import (
	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/resolver"
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
// フラグ（M08 で --app-ref 追加）:
//
//	--app      int64   アプリ ID 直指定（--app-ref と排他）
//	--app-ref  string  アプリ参照（数値文字列 / code / name / partial、--app と排他）
//	--id       int64   レコード ID（必須）
//
// 出力例:
//
//	{"ok":true,"data":{"record":{...}}}
func newRecordGetCmd() *cobra.Command {
	var (
		app    int64
		appRef string
		id     int64
	)
	cmd := &cobra.Command{
		Use:   "get",
		Short: "レコード単件を取得する",
		Long:  "GET /k/v1/record.json を呼び、record を JSON で返します。--app と --app-ref はどちらか一方を指定してください。",
		RunE: func(cmd *cobra.Command, args []string) error {
			if app > 0 && appRef != "" {
				return clierr.NewUsageError("--app and --app-ref are mutually exclusive")
			}
			if app == 0 && appRef == "" {
				return clierr.NewUsageError("either --app or --app-ref is required")
			}
			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			appID := app
			if appRef != "" {
				r := resolver.New(a)
				resolved, err := r.ResolveApp(cmd.Context(), appRef)
				if err != nil {
					return err
				}
				appID = resolved
			}
			resp, err := a.GetRecord(cmd.Context(), kintoneapi.GetRecordRequest{App: appID, ID: id})
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
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（数値直指定、--app-ref と排他）")
	cmd.Flags().StringVar(&appRef, "app-ref", "", "kintone アプリ参照（数値文字列 / code / name / partial、--app と排他）")
	cmd.Flags().Int64Var(&id, "id", 0, "レコード ID（必須）")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
