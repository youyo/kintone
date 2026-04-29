// Package facade は MCP サーバーのツールハンドラ群を提供する。
//
// 設計判断:
//   - 各 MCP tool ハンドラは internal/service/operations を直接呼ぶ
//   - 出力は CLI と同じ envelope（{"ok":true,"data":...} / {"ok":false,"error":...}）を
//     CallToolResult.Content[0].Text に埋める。internal/output を再利用
//   - facade 経路では cli.MapToOutputError は使えないため、独立した error mapper を持つ
//   - 入力は map[string]any として受け、JSONSchema は mcp.NewTool の WithXxx で表現
package facade

import (
	"context"
	"errors"
	"net/url"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/service/operations"
)

// MapError は facade 経路の任意 error を *output.Error に変換する。
//
// CLI 経路の cli.MapToOutputError と同じ意味論を持ちつつ、cobra/USAGE 概念は持たない。
// 操作上のバリデーションエラー（operations.Err*）は INVALID_PARAMS、
// kintone REST のエラーは KINTONE_*、ネットワーク系は KINTONE_NETWORK、
// それ以外は INTERNAL に分類する。
//
// nil を渡すと nil を返す。
func MapError(err error) *output.Error {
	if err == nil {
		return nil
	}

	// operations のバリデーションエラー → INVALID_PARAMS
	switch {
	case errors.Is(err, operations.ErrInvalidApp),
		errors.Is(err, operations.ErrEmptyRecords),
		errors.Is(err, operations.ErrConflictingRecords),
		errors.Is(err, operations.ErrMissingUpdateKey),
		errors.Is(err, operations.ErrConflictingUpdateKey),
		errors.Is(err, operations.ErrEmptyRecord),
		errors.Is(err, operations.ErrEmptyIDs),
		errors.Is(err, operations.ErrInvalidID),
		errors.Is(err, operations.ErrRevisionsLengthMismatch):
		return &output.Error{Code: "INVALID_PARAMS", Message: err.Error()}
	}

	// kintone REST API のエラー
	var apiErr *kintoneapi.APIError
	if errors.As(err, &apiErr) {
		details := map[string]any{"http_status": apiErr.HTTPStatus}
		if apiErr.Code != "" {
			details["kintone_code"] = apiErr.Code
		}
		if apiErr.ID != "" {
			details["kintone_id"] = apiErr.ID
		}
		if apiErr.RetryAfter > 0 {
			details["retry_after_sec"] = int(apiErr.RetryAfter.Seconds())
		}
		return &output.Error{
			Code:    apiErrorCode(apiErr),
			Message: apiErr.Error(),
			Details: details,
		}
	}

	// ネットワーク系
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return &output.Error{
			Code:    "KINTONE_NETWORK",
			Message: urlErr.Error(),
			Details: map[string]any{"timeout": urlErr.Timeout()},
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &output.Error{
			Code:    "KINTONE_NETWORK",
			Message: err.Error(),
			Details: map[string]any{"timeout": true},
		}
	}

	return &output.Error{Code: "INTERNAL", Message: err.Error()}
}

// apiErrorCode は APIError.Category に対応する output.Error.Code を返す。
//
// cli.mapAPIErrorCode と同等内容を facade ローカルに持つ（M11 polish 時に共通化を検討）。
func apiErrorCode(e *kintoneapi.APIError) string {
	switch e.Category {
	case kintoneapi.CategoryUnauthorized:
		return "KINTONE_UNAUTHORIZED"
	case kintoneapi.CategoryForbidden:
		return "KINTONE_FORBIDDEN"
	case kintoneapi.CategoryNotFound:
		return "KINTONE_NOT_FOUND"
	case kintoneapi.CategoryRateLimited:
		return "KINTONE_RATE_LIMITED"
	case kintoneapi.CategoryValidation, kintoneapi.CategoryClientError:
		return "KINTONE_VALIDATION"
	case kintoneapi.CategoryServerError:
		return "KINTONE_INTERNAL"
	default:
		return "KINTONE_INTERNAL"
	}
}
