package facade

import (
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/output"
)

// successResult は data を envelope `{"ok":true,"data":data}` でエンコードして
// CallToolResult.Content[0].Text に格納する。
//
// 戻り値の error は **常に nil** を返す（プロトコルレベルのエラーではないため）。
// ツール内部のビジネスエラーは failureResult / errorResult で envelope に格納する。
func successResult(data any) (*mcp.CallToolResult, error) {
	payload, err := output.Success(data)
	if err != nil {
		// JSON エンコード失敗は実質起こらないが、念のため INTERNAL に変換
		return errorResult(err)
	}
	return mcp.NewToolResultText(string(payload)), nil
}

// failureResult は *output.Error を envelope `{"ok":false,"error":{...}}` で返す。
func failureResult(e *output.Error) (*mcp.CallToolResult, error) {
	payload, err := output.Failure(e)
	if err != nil {
		// nil の場合のみエラーになりうる。安全側で INTERNAL に丸める。
		return mcp.NewToolResultText(`{"ok":false,"error":{"code":"INTERNAL","message":"failed to encode error envelope"}}`), nil
	}
	return mcp.NewToolResultText(string(payload)), nil
}

// errorResult は任意 error を MapError（builder なし）で *output.Error 化し failureResult を返す。
//
// AUTH_REQUIRED envelope に authorize_url を含めたい呼び出し元は errorResultWithDeps を使う。
func errorResult(err error) (*mcp.CallToolResult, error) {
	return failureResult(MapError(err))
}

// errorResultWithDeps は ToolDeps の AuthorizeURLBuilder を渡して MapError する（M13）。
//
// AuthRequiredError 発生時に details.authorize_url を envelope に含める。
func errorResultWithDeps(err error, deps ToolDeps) (*mcp.CallToolResult, error) {
	return failureResult(MapErrorWithBuilder(err, deps.AuthorizeURLBuilder))
}

// invalidParams は引数 parse / バリデーション失敗時の envelope を作る。
// MapError 経由ではなく直接 INVALID_PARAMS を返したいケースで使う。
func invalidParams(message string) (*mcp.CallToolResult, error) {
	return failureResult(&output.Error{Code: "INVALID_PARAMS", Message: message})
}
