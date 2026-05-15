package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/resolver"
	"github.com/youyo/kintone/internal/service/operations"
)

func appDescribeTool() mcp.Tool {
	return mcp.NewTool("app_describe",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithDescription(
			"単一の kintone アプリの詳細情報（基本属性 + フォームのフィールド定義）を取得する。"+
				"app（数値 ID）または app_ref（数値文字列 / code / name / partial）のいずれか必須・両方指定不可。"+
				"結果は {\"ok\":true,\"data\":{\"app\":{...},\"fields\":{...},\"revision\":\"...\"}} 形式の JSON envelope。",
		),
		mcp.WithNumber("app",
			mcp.Description("アプリ ID（数値・app_ref と排他）。"),
			mcp.Min(1),
		),
		mcp.WithString("app_ref",
			mcp.Description("アプリ参照（数値文字列 / code / name / partial、app と排他）。"),
		),
		mcp.WithString("lang",
			mcp.Description("フィールド表示言語: ja|en|zh|user|default。未指定で kintone のデフォルト。"),
			mcp.Enum("ja", "en", "zh", "user", "default"),
		),
	)
}

// AppDescribeHandler はテスト用 export。
func AppDescribeHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return appDescribeHandler(deps)
}

func appDescribeHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		app, err := optInt64(args, "app")
		if err != nil {
			return invalidParams(err.Error())
		}
		appRef, err := optString(args, "app_ref")
		if err != nil {
			return invalidParams(err.Error())
		}
		lang, err := optString(args, "lang")
		if err != nil {
			return invalidParams(err.Error())
		}
		api, err := resolveAPI(ctx, deps)
		if err != nil {
			return errorResultWithDeps(err, deps)
		}
		r := resolver.New(api)
		out, err := operations.AppDescribe(ctx, api, r, operations.AppDescribeInput{
			App:    app,
			AppRef: appRef,
			Lang:   lang,
		})
		if err != nil {
			return errorResultWithDeps(err, deps)
		}
		return successResult(out)
	}
}
