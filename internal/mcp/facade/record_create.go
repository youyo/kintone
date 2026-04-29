package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/service/operations"
)

func recordCreateTool() mcp.Tool {
	return mcp.NewTool("record_create",
		mcp.WithDescription(
			"kintone にレコードを新規作成する。record（単件）または records（複数件、最大 100 件）のいずれか一方を指定する。"+
				"レコードのフィールド形式は {\"フィールドコード\":{\"value\":\"値\"}} の object。"+
				"結果は {\"ok\":true,\"data\":{\"ids\":[...],\"revisions\":[...]}} 形式の JSON envelope。",
		),
		mcp.WithNumber("app",
			mcp.Required(),
			mcp.Description("アプリ ID（必須・正の整数）。"),
			mcp.Min(1),
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
		app, err := requireInt64(args, "app")
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
		out, err := operations.RecordCreate(ctx, deps.API, operations.RecordCreateInput{
			App:     app,
			Record:  record,
			Records: records,
		})
		if err != nil {
			return errorResult(err)
		}
		return successResult(out)
	}
}
