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
	"github.com/youyo/kintone/internal/resolver"
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

	// resolver の AmbiguousError → RESOLVER_*_AMBIGUOUS（M08）
	var ambErr *resolver.AmbiguousError
	if errors.As(err, &ambErr) {
		return resolverAmbiguousFacade(ambErr)
	}

	// resolver の NotFoundError → RESOLVER_*_NOT_FOUND（M08）
	var nfErr *resolver.NotFoundError
	if errors.As(err, &nfErr) {
		return resolverNotFoundFacade(nfErr)
	}

	// resolver の sentinel
	if errors.Is(err, resolver.ErrAppListTooLarge) {
		return &output.Error{Code: "RESOLVER_APP_LIST_TOO_LARGE", Message: err.Error()}
	}
	if errors.Is(err, resolver.ErrEmptyRef) || errors.Is(err, resolver.ErrInvalidAppID) {
		return &output.Error{Code: "INVALID_PARAMS", Message: err.Error()}
	}

	// operations のバリデーションエラー → INVALID_PARAMS（M08 で AppRef 系も追加）
	switch {
	case errors.Is(err, operations.ErrInvalidApp),
		errors.Is(err, operations.ErrConflictingAppRef),
		errors.Is(err, operations.ErrConflictingUpdateKeyFieldRef),
		errors.Is(err, operations.ErrEmptyRecords),
		errors.Is(err, operations.ErrConflictingRecords),
		errors.Is(err, operations.ErrMissingUpdateKey),
		errors.Is(err, operations.ErrConflictingUpdateKey),
		errors.Is(err, operations.ErrEmptyRecord),
		errors.Is(err, operations.ErrEmptyIDs),
		errors.Is(err, operations.ErrInvalidID),
		errors.Is(err, operations.ErrRevisionsLengthMismatch):
		return &output.Error{Code: "INVALID_PARAMS", Message: err.Error()}
	case errors.Is(err, operations.ErrResolverUnavailable):
		return &output.Error{Code: "INTERNAL", Message: err.Error()}
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

// resolverAmbiguousFacade は resolver.AmbiguousError を output.Error に変換する（facade 経路 / M08）。
func resolverAmbiguousFacade(e *resolver.AmbiguousError) *output.Error {
	code := "RESOLVER_APP_AMBIGUOUS"
	if e.Kind == "field" {
		code = "RESOLVER_FIELD_AMBIGUOUS"
	}
	return &output.Error{
		Code:    code,
		Message: e.Error(),
		Details: map[string]any{
			"kind":       e.Kind,
			"ref":        e.Ref,
			"candidates": resolverCandidatesToMap(e.Candidates),
		},
	}
}

// resolverNotFoundFacade は resolver.NotFoundError を output.Error に変換する（facade 経路 / M08）。
func resolverNotFoundFacade(e *resolver.NotFoundError) *output.Error {
	code := "RESOLVER_APP_NOT_FOUND"
	if e.Kind == "field" {
		code = "RESOLVER_FIELD_NOT_FOUND"
	}
	return &output.Error{
		Code:    code,
		Message: e.Error(),
		Details: map[string]any{
			"kind":       e.Kind,
			"ref":        e.Ref,
			"candidates": resolverCandidatesToMap(e.Candidates),
		},
	}
}

// resolverCandidatesToMap は []resolver.Candidate を JSON 出力用 []map[string]any に変換する。
//
// 空フィールドは map に含めない（output.Error.Details の見やすさ優先）。
func resolverCandidatesToMap(cs []resolver.Candidate) []map[string]any {
	out := make([]map[string]any, 0, len(cs))
	for _, c := range cs {
		m := map[string]any{}
		if c.ID != "" {
			m["id"] = c.ID
		}
		if c.Code != "" {
			m["code"] = c.Code
		}
		if c.Name != "" {
			m["name"] = c.Name
		}
		if c.Label != "" {
			m["label"] = c.Label
		}
		out = append(out, m)
	}
	return out
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
