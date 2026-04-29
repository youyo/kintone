package cli

import (
	"errors"
	"strings"

	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/output"
)

// MapToOutputError は cobra および config 関連のエラーを *output.Error に変換する。
// nil を渡すと nil を返す。
//
// 優先順位（先頭ヒットを採用）:
//  1. config.ProfileNotFoundError → CONFIG_PROFILE_NOT_FOUND
//  2. config.ParseError           → CONFIG_PARSE_ERROR
//  3. config.AlreadyExistsError   → CONFIG_ALREADY_EXISTS
//  4. config.NotFoundError        → CONFIG_NOT_FOUND
//  5. cobra USAGE 系              → USAGE
//  6. その他                      → INTERNAL
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

	msg := err.Error()
	if isUsageError(msg) {
		return &output.Error{Code: "USAGE", Message: msg}
	}
	return &output.Error{Code: "INTERNAL", Message: msg}
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
