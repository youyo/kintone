package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/service/operations"
)

func recordUpdateTool() mcp.Tool {
	return mcp.NewTool("record_update",
		mcp.WithDescription(
			"kintone のレコードを単件更新する。"+
				"id（数値）または update_key_field+update_key_value（文字列フィールドの値で特定）のいずれか一方で対象を指定する。"+
				"record は更新フィールドの object（{\"フィールドコード\":{\"value\":\"値\"}}）で必須。"+
				"revision を指定すると楽観ロック（指定 revision と一致する場合のみ更新）。"+
				"結果は {\"ok\":true,\"data\":{\"revision\":N}} 形式の JSON envelope。",
		),
		mcp.WithNumber("app",
			mcp.Required(),
			mcp.Description("アプリ ID（必須・正の整数）。"),
			mcp.Min(1),
		),
		mcp.WithNumber("id",
			mcp.Description("対象レコード ID。update_key_* と排他。"),
			mcp.Min(1),
		),
		mcp.WithString("update_key_field",
			mcp.Description("updateKey 用フィールドコード（重複禁止のフィールドのみ）。update_key_value と併用。"),
		),
		mcp.WithString("update_key_value",
			mcp.Description("updateKey 用の値（文字列）。update_key_field と併用。"),
		),
		mcp.WithNumber("revision",
			mcp.Description("楽観ロック用 revision（任意・未指定で省略）。"),
		),
		mcp.WithObject("record",
			mcp.Required(),
			mcp.Description("更新フィールド（{フィールドコード:{value:値}}）。空オブジェクトは不可。"),
		),
	)
}

// RecordUpdateHandler はテスト用 export。
func RecordUpdateHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return recordUpdateHandler(deps)
}

func recordUpdateHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		app, err := requireInt64(args, "app")
		if err != nil {
			return invalidParams(err.Error())
		}
		id, err := optInt64(args, "id")
		if err != nil {
			return invalidParams(err.Error())
		}
		ukField, err := optString(args, "update_key_field")
		if err != nil {
			return invalidParams(err.Error())
		}
		ukValue, err := optString(args, "update_key_value")
		if err != nil {
			return invalidParams(err.Error())
		}
		// revision は optional。0 もしくは未指定で nil ポインタとする。
		var revPtr *int64
		if v, ok := args["revision"]; ok && v != nil {
			f, ok := v.(float64)
			if !ok {
				return invalidParams("invalid argument type: revision must be a number")
			}
			n := int64(f)
			revPtr = &n
		}
		record, err := optMap(args, "record")
		if err != nil {
			return invalidParams(err.Error())
		}
		out, err := operations.RecordUpdate(ctx, deps.API, operations.RecordUpdateInput{
			App:            app,
			ID:             id,
			UpdateKeyField: ukField,
			UpdateKeyValue: ukValue,
			Revision:       revPtr,
			Record:         record,
		})
		if err != nil {
			return errorResult(err)
		}
		return successResult(out)
	}
}
