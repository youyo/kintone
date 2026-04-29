package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/service/operations"
)

func recordsQueryTool() mcp.Tool {
	return mcp.NewTool("records_query",
		mcp.WithDescription(
			"kintone のレコード一覧を取得する。kintone クエリ言語（query フィールド）でフィルタ・並び替え・ページングを行える。"+
				"結果は {\"ok\":true,\"data\":{\"records\":[...],\"total_count\":N?}} 形式の JSON envelope。",
		),
		mcp.WithNumber("app",
			mcp.Required(),
			mcp.Description("アプリ ID（必須・正の整数）。"),
			mcp.Min(1),
		),
		mcp.WithString("query",
			mcp.Description("kintone クエリ言語。例: 'name like \"佐藤\" order by 作成日時 desc limit 10'"),
		),
		mcp.WithArray("fields",
			mcp.Description("レスポンスに含めるフィールドコードの配列。空ならアプリ全フィールドを返す。"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithBoolean("total_count",
			mcp.Description("true で total_count（フィルタ後の総数）を data に含める。"),
		),
	)
}

// RecordsQueryHandler はテスト用 export。
func RecordsQueryHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return recordsQueryHandler(deps)
}

func recordsQueryHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		app, err := requireInt64(args, "app")
		if err != nil {
			return invalidParams(err.Error())
		}
		query, err := optString(args, "query")
		if err != nil {
			return invalidParams(err.Error())
		}
		fields, err := toStringSlice(args["fields"], "fields")
		if err != nil {
			return invalidParams(err.Error())
		}
		totalCount, err := optBool(args, "total_count")
		if err != nil {
			return invalidParams(err.Error())
		}
		out, err := operations.RecordsQuery(ctx, deps.API, operations.RecordsQueryInput{
			App:        app,
			Query:      query,
			Fields:     fields,
			TotalCount: totalCount,
		})
		if err != nil {
			return errorResult(err)
		}
		return successResult(out)
	}
}
