package api

import (
	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/resolver"
	"github.com/youyo/kintone/internal/service/operations"
)

// appFieldsData は `app fields` の data 部分。kintoneapi の生レスポンス形式を踏襲。
type appFieldsData struct {
	Properties map[string]map[string]any `json:"properties"`
	Revision   string                    `json:"revision,omitempty"`
}

// newAppCmd は `kintone api app` ツリー（get / fields / describe）を構築する。
func newAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "アプリ情報・フィールド定義の取得",
	}
	cmd.AddCommand(newAppGetCmd())
	cmd.AddCommand(newAppFieldsCmd())
	cmd.AddCommand(newAppDescribeCmd())
	return cmd
}

// newAppGetCmd は `kintone api app get` を構築する。
// 出力は AppSummary 形式（snake_case 統一）。
//
// M08: --app-ref 追加（--app と排他、どちらか必須）。
func newAppGetCmd() *cobra.Command {
	var (
		app    int64
		appRef string
	)
	cmd := &cobra.Command{
		Use:   "get",
		Short: "アプリ情報を取得する",
		Long:  "GET /k/v1/app.json を呼び、AppSummary 形式の JSON を返します。--app と --app-ref はどちらか一方を指定してください。",
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
			resp, err := a.GetApp(cmd.Context(), kintoneapi.GetAppRequest{ID: appID})
			if err != nil {
				return err
			}
			summary := operations.AppSummary{
				AppID:       resp.AppID,
				Code:        resp.Code,
				Name:        resp.Name,
				Description: resp.Description,
				SpaceID:     resp.SpaceID,
				ThreadID:    resp.ThreadID,
				CreatedAt:   resp.CreatedAt,
				Creator:     resp.Creator,
				ModifiedAt:  resp.ModifiedAt,
				Modifier:    resp.Modifier,
			}
			payload, err := output.Success(summary)
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（数値直指定、--app-ref と排他）")
	cmd.Flags().StringVar(&appRef, "app-ref", "", "kintone アプリ参照（数値文字列 / code / name / partial、--app と排他）")
	return cmd
}

// newAppFieldsCmd は `kintone api app fields` を構築する。
//
// M08: --app-ref 追加（--app と排他、どちらか必須）。
func newAppFieldsCmd() *cobra.Command {
	var (
		app    int64
		appRef string
		lang   string
	)
	cmd := &cobra.Command{
		Use:   "fields",
		Short: "フィールド定義を取得する",
		Long:  "GET /k/v1/app/form/fields.json を呼び、properties と revision を JSON で返します。--app と --app-ref はどちらか一方を指定してください。",
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
			resp, err := a.GetFormFields(cmd.Context(), kintoneapi.GetFormFieldsRequest{App: appID, Lang: lang})
			if err != nil {
				return err
			}
			payload, err := output.Success(appFieldsData{
				Properties: resp.Properties,
				Revision:   resp.Revision,
			})
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（数値直指定、--app-ref と排他）")
	cmd.Flags().StringVar(&appRef, "app-ref", "", "kintone アプリ参照（--app と排他）")
	cmd.Flags().StringVar(&lang, "lang", "", "表示言語（ja/en/zh/user/default）")
	return cmd
}

// newAppDescribeCmd は `kintone api app describe` を構築する。
// operations.AppDescribe を呼び、app + fields の合成 JSON を返す。
//
// M08: --app-ref 追加（--app と排他、どちらか必須）。
func newAppDescribeCmd() *cobra.Command {
	var (
		app    int64
		appRef string
		lang   string
	)
	cmd := &cobra.Command{
		Use:   "describe",
		Short: "アプリ情報とフィールド定義を合成して返す（operations 経由）",
		Long: `operations.AppDescribe を呼び、app + fields を 1 つの JSON にまとめて返します。
LLM 向けにフィールド名は snake_case で統一されています。--app と --app-ref はどちらか一方を指定してください。`,
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
			r := resolver.New(a)
			out, err := operations.AppDescribe(cmd.Context(), a, r, operations.AppDescribeInput{
				App: app, AppRef: appRef, Lang: lang,
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
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（数値直指定、--app-ref と排他）")
	cmd.Flags().StringVar(&appRef, "app-ref", "", "kintone アプリ参照（--app と排他）")
	cmd.Flags().StringVar(&lang, "lang", "", "表示言語（ja/en/zh/user/default）")
	return cmd
}
