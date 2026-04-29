package api

import (
	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
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
func newAppGetCmd() *cobra.Command {
	var app int64
	cmd := &cobra.Command{
		Use:   "get",
		Short: "アプリ情報を取得する",
		Long:  "GET /k/v1/app.json を呼び、AppSummary 形式の JSON を返します。",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			resp, err := a.GetApp(cmd.Context(), kintoneapi.GetAppRequest{ID: app})
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
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	_ = cmd.MarkFlagRequired("app")
	return cmd
}

// newAppFieldsCmd は `kintone api app fields` を構築する。
func newAppFieldsCmd() *cobra.Command {
	var (
		app  int64
		lang string
	)
	cmd := &cobra.Command{
		Use:   "fields",
		Short: "フィールド定義を取得する",
		Long:  "GET /k/v1/app/form/fields.json を呼び、properties と revision を JSON で返します。",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := buildAPI(cmd)
			if err != nil {
				return err
			}
			resp, err := a.GetFormFields(cmd.Context(), kintoneapi.GetFormFieldsRequest{App: app, Lang: lang})
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
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	cmd.Flags().StringVar(&lang, "lang", "", "表示言語（ja/en/zh/user/default）")
	_ = cmd.MarkFlagRequired("app")
	return cmd
}

// newAppDescribeCmd は `kintone api app describe` を構築する。
// operations.AppDescribe を呼び、app + fields の合成 JSON を返す。
func newAppDescribeCmd() *cobra.Command {
	var (
		app  int64
		lang string
	)
	cmd := &cobra.Command{
		Use:   "describe",
		Short: "アプリ情報とフィールド定義を合成して返す（operations 経由）",
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
			payload, err := output.Success(out)
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
	cmd.Flags().Int64Var(&app, "app", 0, "kintone アプリ ID（必須）")
	cmd.Flags().StringVar(&lang, "lang", "", "表示言語（ja/en/zh/user/default）")
	_ = cmd.MarkFlagRequired("app")
	return cmd
}
