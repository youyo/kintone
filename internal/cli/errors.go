package cli

import (
	"strings"

	"github.com/youyo/kintone/internal/output"
)

// MapToOutputError は cobra のエラーを *output.Error に変換する。
// nil を渡すと nil を返す。
// M1 段階のマッピング:
//   - cobra の "unknown command" / flag parse error → Code:"USAGE"
//   - その他 → Code:"INTERNAL"
//
// M2 以降で CONFIG_NOT_FOUND 等のコードを追加する。
func MapToOutputError(err error) *output.Error {
	if err == nil {
		return nil
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
