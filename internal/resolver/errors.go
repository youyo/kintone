// Package resolver は kintone の app / field 名前解決を提供する。
//
// 解決順序（仕様書: docs/specs/kintone_spec.md）:
//   - App: ID → code → name 完全一致 → name 部分一致
//   - Field: code → label 完全一致 → label 部分一致
//
// 依存する service/api.API は CachingAPI でラップされた状態で渡されることを想定する。
// 1 度引いた ListApps / GetFormFields は CachingAPI 側の SQLite キャッシュ（TTL=1 年）で
// 再利用されるため、Resolver 内部に専用キャッシュは持たない。
package resolver

import (
	"errors"
	"fmt"
	"strings"
)

// 共通 sentinel エラー。
//
// errors.Is で判定できるよう NotFoundError / AmbiguousError は Unwrap で
// これらに到達する。
var (
	// ErrAppNotFound は app が見つからなかったエラー。NotFoundError.Unwrap が返す。
	ErrAppNotFound = errors.New("resolver: app not found")
	// ErrFieldNotFound は field が見つからなかったエラー。NotFoundError.Unwrap が返す。
	ErrFieldNotFound = errors.New("resolver: field not found")
	// ErrAppAmbiguous は app の ref が複数候補にマッチしたエラー。AmbiguousError.Unwrap が返す。
	ErrAppAmbiguous = errors.New("resolver: app reference is ambiguous")
	// ErrFieldAmbiguous は field の ref が複数候補にマッチしたエラー。AmbiguousError.Unwrap が返す。
	ErrFieldAmbiguous = errors.New("resolver: field reference is ambiguous")
	// ErrEmptyRef は ref が空文字列で渡されたエラー。
	ErrEmptyRef = errors.New("resolver: ref must not be empty")
	// ErrAppListTooLarge は ListApps のページング上限（10000 件）を超過したエラー。
	ErrAppListTooLarge = errors.New("resolver: app list exceeded resolver limit (10000)")
	// ErrInvalidAppID は ResolveField に <= 0 の appID が渡されたエラー。
	ErrInvalidAppID = errors.New("resolver: appID must be > 0")
)

// Candidate は ambiguous / not-found 時に LLM へ提示する候補情報。
//
// JSON タグは output.Error.Details の candidates 配列に直接シリアライズされる。
type Candidate struct {
	ID    string `json:"id,omitempty"`    // app の数値 ID（field の場合は空）
	Code  string `json:"code,omitempty"`  // app code or field code
	Name  string `json:"name,omitempty"`  // app name
	Label string `json:"label,omitempty"` // field label
}

// NotFoundError は app / field が見つからなかったエラー。
//
// Kind は "app" or "field"。
// Candidates は近似候補（部分一致で当たらなかった場合は空）。
type NotFoundError struct {
	Kind       string
	Ref        string
	Candidates []Candidate
}

// Error は人間可読なエラーメッセージを返す。
func (e *NotFoundError) Error() string {
	return fmt.Sprintf("resolver: %s %q not found", e.Kind, e.Ref)
}

// Unwrap は errors.Is で sentinel と比較できるようにする。
func (e *NotFoundError) Unwrap() error {
	if e.Kind == "field" {
		return ErrFieldNotFound
	}
	return ErrAppNotFound
}

// AmbiguousError は ref が複数候補にマッチしたエラー。
//
// LLM が次の試行で絞り込めるよう Candidates に全候補を含める。
type AmbiguousError struct {
	Kind       string
	Ref        string
	Candidates []Candidate
}

// Error は候補一覧を含む人間可読なエラーメッセージを返す。
func (e *AmbiguousError) Error() string {
	parts := make([]string, 0, len(e.Candidates))
	for _, c := range e.Candidates {
		switch e.Kind {
		case "app":
			parts = append(parts, fmt.Sprintf("%s(id=%s)", c.Name, c.ID))
		case "field":
			parts = append(parts, fmt.Sprintf("%s(code=%s)", c.Label, c.Code))
		}
	}
	return fmt.Sprintf("resolver: %s %q is ambiguous (%d candidates: %s)",
		e.Kind, e.Ref, len(e.Candidates), strings.Join(parts, ", "))
}

// Unwrap は errors.Is で sentinel と比較できるようにする。
func (e *AmbiguousError) Unwrap() error {
	if e.Kind == "field" {
		return ErrFieldAmbiguous
	}
	return ErrAppAmbiguous
}
