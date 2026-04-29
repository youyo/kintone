package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/service/operations"
)

func recordDeleteTool() mcp.Tool {
	return mcp.NewTool("record_delete",
		mcp.WithDescription(
			"kintone のレコードを複数件削除する。ids は対象レコード ID の配列（必須・1 件以上）。"+
				"revisions を指定する場合は ids と同じ長さで対応する revision を渡し、楽観ロックする。"+
				"結果は {\"ok\":true,\"data\":{\"deleted\":N}} 形式の JSON envelope（N は削除リクエスト件数）。",
		),
		mcp.WithNumber("app",
			mcp.Required(),
			mcp.Description("アプリ ID（必須・正の整数）。"),
			mcp.Min(1),
		),
		mcp.WithArray("ids",
			mcp.Required(),
			mcp.Description("削除対象レコード ID 配列（最低 1 件、各要素は正の整数）。"),
			mcp.Items(map[string]any{"type": "number"}),
		),
		mcp.WithArray("revisions",
			mcp.Description("対応する revision の配列（任意、指定時は len(ids) と一致する必要あり）。"),
			mcp.Items(map[string]any{"type": "number"}),
		),
	)
}

// RecordDeleteHandler はテスト用 export。
func RecordDeleteHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return recordDeleteHandler(deps)
}

func recordDeleteHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		app, err := requireInt64(args, "app")
		if err != nil {
			return invalidParams(err.Error())
		}
		ids, err := toInt64Slice(args["ids"], "ids")
		if err != nil {
			return invalidParams(err.Error())
		}
		revisions, err := toInt64Slice(args["revisions"], "revisions")
		if err != nil {
			return invalidParams(err.Error())
		}
		out, err := operations.RecordDelete(ctx, deps.API, operations.RecordDeleteInput{
			App:       app,
			IDs:       ids,
			Revisions: revisions,
		})
		if err != nil {
			return errorResult(err)
		}
		return successResult(out)
	}
}
