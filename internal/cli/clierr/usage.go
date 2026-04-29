// Package clierr は CLI 全サブパッケージで共有する型付きエラー sentinel を提供する。
//
// 配置方針: `internal/cli` 本体は `internal/cli/ops` 等を import するため、
// 逆向き依存（ops → cli）は循環になる。よって UsageError 等は中立な
// `internal/cli/clierr` に置き、cli / cli/api / cli/ops / 将来の cli/cache 等の
// すべてのサブパッケージから自然に import できるようにする（M05 advisor 指摘 #3 反映）。
package clierr

import "fmt"

// UsageError は CLI のサブコマンドが「ユーザー入力ミス」を表す型付き sentinel エラー。
//
// internal/cli の MapToOutputError はこの型を errors.As で検出し、
// output.Error{Code:"USAGE"} に変換する。文字列 prefix マッチに依存せず、
// メッセージ変更や cobra エラーフォーマット変更に強い堅牢な USAGE 分類を実現する。
type UsageError struct {
	Msg string
}

// Error は CLI 出力に表示されるメッセージを返す。
func (e *UsageError) Error() string { return e.Msg }

// NewUsageError は usage エラーを生成するヘルパ。
//
// fmt.Sprintf 互換の format / args を受ける。
func NewUsageError(format string, args ...any) *UsageError {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}
