package facade

import (
	"context"
	"errors"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/kintoneapi"
)

// appsSearchTool は apps_search MCP tool の定義を返す。
func appsSearchTool() mcp.Tool {
	return mcp.NewTool("apps_search",
		mcp.WithDescription(
			"kintone のアプリを検索する。ids/codes/name/space_ids/limit/offset のいずれかの組み合わせを受け取る。"+
				"全てのフィールドは任意で、未指定なら全アプリ（権限のあるもの）を最大 100 件返す。"+
				"結果は JSON envelope で返す: {\"ok\":true,\"data\":{\"apps\":[...]}}。エラー時は {\"ok\":false,\"error\":{...}}。",
		),
		mcp.WithArray("ids",
			mcp.Description("検索対象のアプリ ID 配列（数値）。"),
			mcp.Items(map[string]any{"type": "number"}),
		),
		mcp.WithArray("codes",
			mcp.Description("検索対象のアプリコード配列（文字列）。"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("name",
			mcp.Description("アプリ名の部分一致検索文字列。"),
		),
		mcp.WithArray("space_ids",
			mcp.Description("検索対象のスペース ID 配列（数値）。"),
			mcp.Items(map[string]any{"type": "number"}),
		),
		mcp.WithNumber("limit",
			mcp.Description("最大取得件数（1-100、未指定で 100）。"),
			mcp.Min(1),
			mcp.Max(100),
		),
		mcp.WithNumber("offset",
			mcp.Description("取得開始オフセット（未指定で 0）。"),
			mcp.Min(0),
		),
	)
}

// AppsSearchHandler は apps_search のハンドラを返す（テスト用に export）。
func AppsSearchHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return appsSearchHandler(deps)
}

func appsSearchHandler(deps ToolDeps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		ids, err := toInt64Slice(args["ids"], "ids")
		if err != nil {
			return invalidParams(err.Error())
		}
		codes, err := toStringSlice(args["codes"], "codes")
		if err != nil {
			return invalidParams(err.Error())
		}
		spaceIDs, err := toInt64Slice(args["space_ids"], "space_ids")
		if err != nil {
			return invalidParams(err.Error())
		}
		name, err := optString(args, "name")
		if err != nil {
			return invalidParams(err.Error())
		}
		limit, err := optInt64(args, "limit")
		if err != nil {
			return invalidParams(err.Error())
		}
		offset, err := optInt64(args, "offset")
		if err != nil {
			return invalidParams(err.Error())
		}

		resp, err := deps.API.ListApps(ctx, kintoneapi.ListAppsRequest{
			IDs:      ids,
			Codes:    codes,
			Name:     name,
			SpaceIDs: spaceIDs,
			Limit:    limit,
			Offset:   offset,
		})
		if err != nil {
			return errorResult(err)
		}
		return successResult(map[string]any{"apps": resp.Apps})
	}
}

// errInvalidArgType は引数の JSON 型が想定と異なる場合のエラー。
var errInvalidArgType = errors.New("invalid argument type")

// toInt64Slice は arg を []int64 に変換する。
//
// JSON-RPC では数値は float64 で渡るため、これを int64 に丸める。
// nil のときは nil を返す（任意フィールドの未指定）。
func toInt64Slice(arg any, field string) ([]int64, error) {
	if arg == nil {
		return nil, nil
	}
	raw, ok := arg.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an array of numbers", errInvalidArgType, field)
	}
	out := make([]int64, 0, len(raw))
	for i, v := range raw {
		n, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("%w: %s[%d] must be a number", errInvalidArgType, field, i)
		}
		out = append(out, int64(n))
	}
	return out, nil
}

// toStringSlice は arg を []string に変換する。
func toStringSlice(arg any, field string) ([]string, error) {
	if arg == nil {
		return nil, nil
	}
	raw, ok := arg.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an array of strings", errInvalidArgType, field)
	}
	out := make([]string, 0, len(raw))
	for i, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%w: %s[%d] must be a string", errInvalidArgType, field, i)
		}
		out = append(out, s)
	}
	return out, nil
}

// optString は args[field] を文字列として取得する。未指定なら空文字。
func optString(args map[string]any, field string) (string, error) {
	v, ok := args[field]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%w: %s must be a string", errInvalidArgType, field)
	}
	return s, nil
}

// optInt64 は args[field] を int64 として取得する。未指定なら 0。
func optInt64(args map[string]any, field string) (int64, error) {
	v, ok := args[field]
	if !ok || v == nil {
		return 0, nil
	}
	n, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("%w: %s must be a number", errInvalidArgType, field)
	}
	return int64(n), nil
}

// requireInt64 は args[field] を必須数値として取得する。
func requireInt64(args map[string]any, field string) (int64, error) {
	v, ok := args[field]
	if !ok || v == nil {
		return 0, fmt.Errorf("%w: %s is required", errInvalidArgType, field)
	}
	n, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("%w: %s must be a number", errInvalidArgType, field)
	}
	return int64(n), nil
}

// optBool は args[field] を bool として取得する。
func optBool(args map[string]any, field string) (bool, error) {
	v, ok := args[field]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s must be a boolean", errInvalidArgType, field)
	}
	return b, nil
}

// optMap は args[field] を map[string]any として取得する。
func optMap(args map[string]any, field string) (map[string]any, error) {
	v, ok := args[field]
	if !ok || v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an object", errInvalidArgType, field)
	}
	return m, nil
}

// optMapSlice は args[field] を []map[string]any として取得する。
func optMapSlice(arg any, field string) ([]map[string]any, error) {
	if arg == nil {
		return nil, nil
	}
	raw, ok := arg.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: %s must be an array of objects", errInvalidArgType, field)
	}
	out := make([]map[string]any, 0, len(raw))
	for i, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: %s[%d] must be an object", errInvalidArgType, field, i)
		}
		out = append(out, m)
	}
	return out, nil
}
