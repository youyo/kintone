package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/service/operations"
)

func appDescribeTool() mcp.Tool {
	return mcp.NewTool("app_describe",
		mcp.WithDescription(
			"単一の kintone アプリの詳細情報（基本属性 + フォームのフィールド定義）を取得する。"+
				"結果は {\"ok\":true,\"data\":{\"app\":{...},\"fields\":{...},\"revision\":\"...\"}} 形式の JSON envelope。",
		),
		mcp.WithNumber("app",
			mcp.Required(),
			mcp.Description("アプリ ID（必須・正の整数）。"),
			mcp.Min(1),
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
		app, err := requireInt64(args, "app")
		if err != nil {
			return invalidParams(err.Error())
		}
		lang, err := optString(args, "lang")
		if err != nil {
			return invalidParams(err.Error())
		}
		out, err := operations.AppDescribe(ctx, deps.API, operations.AppDescribeInput{
			App:  app,
			Lang: lang,
		})
		if err != nil {
			return errorResult(err)
		}
		return successResult(out)
	}
}
