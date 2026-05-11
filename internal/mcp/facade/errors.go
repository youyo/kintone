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

	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/resolver"
	serviceapi "github.com/youyo/kintone/internal/service/api"
	"github.com/youyo/kintone/internal/service/operations"
	"github.com/youyo/kintone/internal/store"
)

// MapError は facade 経路の任意 error を *output.Error に変換する。
//
// authorize URL builder は nil 渡しで M11 互換セマンティクス（AUTH_REQUIRED に details なし）。
func MapError(err error) *output.Error {
	return MapErrorWithBuilder(err, nil)
}

// MapErrorWithBuilder は MapError の拡張版（M13）。
//
// builder != nil の場合、AuthRequiredError を捕捉して AUTH_REQUIRED envelope の
// details に authorize_url / principal_id / domain を含める。
// builder == nil の場合、details なしで Code=AUTH_REQUIRED のみ返す（M11 互換）。
//
// CLI 経路の cli.MapToOutputError と同じ意味論を持ちつつ、cobra/USAGE 概念は持たない。
// 操作上のバリデーションエラー（operations.Err*）は INVALID_PARAMS、
// kintone REST のエラーは KINTONE_*、ネットワーク系は KINTONE_NETWORK、
// それ以外は INTERNAL に分類する。
//
// nil を渡すと nil を返す。
func MapErrorWithBuilder(err error, builder func(principalID string) string) *output.Error {
	if err == nil {
		return nil
	}

	// 構造化 AuthRequiredError → AUTH_REQUIRED + details（M13）
	var authReq *serviceapi.AuthRequiredError
	if errors.As(err, &authReq) {
		details := map[string]any{}
		if authReq.PrincipalID != "" {
			details["principal_id"] = authReq.PrincipalID
		}
		if authReq.Domain != "" {
			details["domain"] = authReq.Domain
		}
		if builder != nil && authReq.PrincipalID != "" {
			if url := builder(authReq.PrincipalID); url != "" {
				details["authorize_url"] = url
			}
		}
		out := &output.Error{Code: "AUTH_REQUIRED", Message: err.Error()}
		if len(details) > 0 {
			out.Details = details
		}
		return out
	}

	// PrincipalAPIFactory が Principal 不在で返す plain ErrAuthRequired → AUTH_REQUIRED（M11 互換）
	if errors.Is(err, serviceapi.ErrAuthRequired) {
		return &output.Error{Code: "AUTH_REQUIRED", Message: err.Error()}
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

	// store / idproxy の sentinel（Phase 6d）
	// INVALID_PARAMS 相当（設定ミス・未対応組合せ）
	switch {
	case errors.Is(err, store.ErrTableNotFound):
		return &output.Error{Code: "STORE_TABLE_NOT_FOUND", Message: err.Error()}
	case errors.Is(err, store.ErrGSIMissing):
		return &output.Error{Code: "STORE_GSI_MISSING", Message: err.Error()}
	case errors.Is(err, store.ErrTTLDisabled):
		return &output.Error{Code: "STORE_TTL_DISABLED", Message: err.Error()}
	case errors.Is(err, store.ErrMemoryOIDCForbidden):
		return &output.Error{Code: "STORE_MEMORY_OIDC_FORBIDDEN", Message: err.Error()}
	case errors.Is(err, store.ErrCacheBypassInvalid):
		return &output.Error{Code: "STORE_CACHE_BYPASS_INVALID", Message: err.Error()}
	case errors.Is(err, store.ErrPlaintextForbidden):
		return &output.Error{Code: "STORE_PLAINTEXT_FORBIDDEN", Message: err.Error()}
	case errors.Is(err, store.ErrPrincipalNotFound):
		return &output.Error{Code: "RESOLVER_PRINCIPAL_NOT_FOUND", Message: err.Error()}
	case errors.Is(err, idproxy.ErrSigningKeyRequired):
		return &output.Error{Code: "SIGNING_KEY_REQUIRED", Message: err.Error()}
	}
	// INTERNAL 系（接続失敗）— cause_class で分類、raw message は出さない
	if errors.Is(err, store.ErrConnectionFailed) {
		return &output.Error{
			Code:    "STORE_CONNECTION_FAILED",
			Message: err.Error(),
			Details: map[string]any{"cause_class": output.ClassifyBackendError(err)},
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
