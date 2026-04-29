package cli

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
)

// MapToOutputError は cobra、config、kintoneapi 関連のエラーを *output.Error に変換する。
// nil を渡すと nil を返す。
//
// 優先順位（先頭ヒットを採用）:
//  1. config.ProfileNotFoundError → CONFIG_PROFILE_NOT_FOUND
//  2. config.ParseError           → CONFIG_PARSE_ERROR
//  3. config.AlreadyExistsError   → CONFIG_ALREADY_EXISTS
//  4. config.NotFoundError        → CONFIG_NOT_FOUND
//  5. kintoneapi.APIError         → KINTONE_*
//  6. net/url.Error / context.DeadlineExceeded → KINTONE_NETWORK
//  7. cobra USAGE 系              → USAGE
//  8. その他                      → INTERNAL
func MapToOutputError(err error) *output.Error {
	if err == nil {
		return nil
	}

	var pne *config.ProfileNotFoundError
	if errors.As(err, &pne) {
		details := map[string]any{"name": pne.Name}
		if pne.Path != "" {
			details["path"] = pne.Path
		}
		return &output.Error{
			Code:    "CONFIG_PROFILE_NOT_FOUND",
			Message: pne.Error(),
			Details: details,
		}
	}

	var pe *config.ParseError
	if errors.As(err, &pe) {
		details := map[string]any{"path": pe.Path}
		if pe.Err != nil {
			details["cause"] = pe.Err.Error()
		}
		return &output.Error{
			Code:    "CONFIG_PARSE_ERROR",
			Message: pe.Error(),
			Details: details,
		}
	}

	var ae *config.AlreadyExistsError
	if errors.As(err, &ae) {
		return &output.Error{
			Code:    "CONFIG_ALREADY_EXISTS",
			Message: ae.Error(),
			Details: map[string]any{"path": ae.Path},
		}
	}

	var nfe *config.NotFoundError
	if errors.As(err, &nfe) {
		return &output.Error{
			Code:    "CONFIG_NOT_FOUND",
			Message: nfe.Error(),
			Details: map[string]any{"path": nfe.Path},
		}
	}

	var apiErr *kintoneapi.APIError
	if errors.As(err, &apiErr) {
		code := mapAPIErrorCode(apiErr)
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
			Code:    code,
			Message: apiErr.Error(),
			Details: details,
		}
	}

	// net/url.Error（タイムアウト含む）→ KINTONE_NETWORK
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		details := map[string]any{"timeout": urlErr.Timeout()}
		return &output.Error{
			Code:    "KINTONE_NETWORK",
			Message: urlErr.Error(),
			Details: details,
		}
	}

	// context.DeadlineExceeded / context.Canceled → KINTONE_NETWORK
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &output.Error{
			Code:    "KINTONE_NETWORK",
			Message: err.Error(),
			Details: map[string]any{"timeout": true},
		}
	}

	// CLI サブパッケージ共通の UsageError（型付き sentinel） → USAGE
	// 文字列 prefix 依存（isUsageError）に頼らず errors.As で堅牢に分類する（M05 advisor 指摘 #1）。
	// 配置: internal/cli/clierr（cli / cli/ops 双方から循環なく依存可能な中立パッケージ）。
	var ue *clierr.UsageError
	if errors.As(err, &ue) {
		return &output.Error{Code: "USAGE", Message: ue.Error()}
	}

	msg := err.Error()
	if isUsageError(msg) {
		return &output.Error{Code: "USAGE", Message: msg}
	}
	return &output.Error{Code: "INTERNAL", Message: msg}
}

// mapAPIErrorCode は APIError の Category に応じた output コードを返す。
func mapAPIErrorCode(e *kintoneapi.APIError) string {
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

// isUsageError は cobra のエラーメッセージがユーザー操作ミスによるものかを判定する。
// cobra の "unknown command" prefix または flag parse エラーを USAGE として扱う。
// R-12 対応: 文字列 prefix の最小チェックに留め、cobra バージョンアップの影響を最小化。
func isUsageError(msg string) bool {
	return strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "unknown flag:") ||
		strings.HasPrefix(msg, "unknown shorthand flag:") ||
		strings.HasPrefix(msg, "invalid argument") ||
		strings.HasPrefix(msg, "required flag") ||
		strings.HasPrefix(msg, "flag needs an argument")
}
