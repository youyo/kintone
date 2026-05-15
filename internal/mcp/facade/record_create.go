package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/resolver"
	"github.com/youyo/kintone/internal/service/operations"
)

func recordCreateTool() mcp.Tool {
	return mcp.NewTool("record_create",
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithDescription(
			"kintone にレコードを新規作成する。record（単件）または records（複数件、最大 100 件）のいずれか一方を指定する。"+
				"app（数値 ID）または app_ref（数値文字列 / code / name / partial）のいずれか必須・両方指定不可。"+
				"レコードのフィールド形式は {\"フィールドコード\":{\"value\":\"値\"}} の object。"+
				"結果は {\"ok\":true,\"data\":{\"ids\":[...],\"revisions\":[...]}} 形式の JSON envelope。",
		),
		mcp.WithNumber("app",
			mcp.Description("アプリ ID（数値・app_ref と排他）。"),
			mcp.Min(1),
		),
		mcp.WithString("app_ref",
			mcp.Description("アプリ参照（数値文字列 / code / name / partial、app と排他）。"),
		),
		mcp.WithObject("record",
			mcp.Description("単件登録用 fields。records と排他。"),
		),
		mcp.WithArray("records",
			mcp.Description("複数件登録用 fields の配列（最大 100 件）。record と排他。"),
			mcp.Items(map[string]any{"type": "object"}),
		),
	)
}

// RecordCreateHandler はテスト用 export。
func RecordCreateHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return recordCreateHandler(deps)
}

func recordCreateHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		record, err := optMap(args, "record")
		if err != nil {
			return invalidParams(err.Error())
		}
		records, err := optMapSlice(args["records"], "records")
		if err != nil {
			return invalidParams(err.Error())
		}
		api, err := resolveAPI(ctx, deps)
		if err != nil {
			return errorResultWithDeps(err, deps)
		}
		r := resolver.New(api)
		out, err := operations.RecordCreate(ctx, api, r, operations.RecordCreateInput{
			App:     app,
			AppRef:  appRef,
			Record:  record,
			Records: records,
		})
		if err != nil {
			return errorResultWithDeps(err, deps)
		}
		return successResult(out)
	}
}
