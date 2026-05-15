package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/resolver"
	"github.com/youyo/kintone/internal/service/operations"
)

func recordsQueryTool() mcp.Tool {
	return mcp.NewTool("records_query",
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithDescription(
			"kintone のレコード一覧を取得する。kintone クエリ言語（query フィールド）でフィルタ・並び替え・ページングを行える。"+
				"app（数値 ID）または app_ref（数値文字列 / code / name / partial）のいずれか必須・両方指定不可。"+
				"結果は {\"ok\":true,\"data\":{\"records\":[...],\"total_count\":N?}} 形式の JSON envelope。",
		),
		mcp.WithNumber("app",
			mcp.Description("アプリ ID（数値・app_ref と排他）。"),
			mcp.Min(1),
		),
		mcp.WithString("app_ref",
			mcp.Description("アプリ参照（数値文字列 / code / name / partial、app と排他）。"),
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
		app, err := optInt64(args, "app")
		if err != nil {
			return invalidParams(err.Error())
		}
		appRef, err := optString(args, "app_ref")
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
		api, err := resolveAPI(ctx, deps)
		if err != nil {
			return errorResultWithDeps(err, deps)
		}
		r := resolver.New(api)
		out, err := operations.RecordsQuery(ctx, api, r, operations.RecordsQueryInput{
			App:        app,
			AppRef:     appRef,
			Query:      query,
			Fields:     fields,
			TotalCount: totalCount,
		})
		if err != nil {
			return errorResultWithDeps(err, deps)
		}
		return successResult(out)
	}
}
