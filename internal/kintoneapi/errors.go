package kintoneapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrorCategory は HTTP ステータスと kintone コードから派生したエラー分類。
type ErrorCategory int

const (
	// CategoryUnknown は分類不能（2xx 含む）。
	CategoryUnknown ErrorCategory = iota
	// CategoryUnauthorized は 401 認証失敗。
	CategoryUnauthorized
	// CategoryForbidden は 403 認可失敗。
	CategoryForbidden
	// CategoryNotFound は 404。
	CategoryNotFound
	// CategoryRateLimited は 429 レート制限。
	CategoryRateLimited
	// CategoryValidation は 4xx の validation 系（400/422 + コード prefix が "CB_VA" 等）。
	CategoryValidation
	// CategoryServerError は 5xx 系（503 含む）。
	CategoryServerError
	// CategoryClientError はその他 4xx。
	CategoryClientError
)

// String は category の人間可読名を返す。
func (c ErrorCategory) String() string {
	switch c {
	case CategoryUnauthorized:
		return "unauthorized"
	case CategoryForbidden:
		return "forbidden"
	case CategoryNotFound:
		return "not_found"
	case CategoryRateLimited:
		return "rate_limited"
	case CategoryValidation:
		return "validation"
	case CategoryServerError:
		return "server_error"
	case CategoryClientError:
		return "client_error"
	default:
		return "unknown"
	}
}

// APIError は kintone REST API のエラーレスポンスを表す。
//
// kintone 標準エラー形式:
//
//	{ "code": "GAIA_AP01", "id": "abc...", "message": "指定したアプリ..." }
//
// 一部のエラー（特にネットワーク層・5xx で空 body）では Code/ID/Message が空。
// その場合は HTTPStatus と Category のみで判別する。
type APIError struct {
	HTTPStatus int
	Code       string
	ID         string
	Message    string
	RawBody    []byte // デバッグ用（最大 4KB に切り詰め）
	RetryAfter time.Duration
	Category   ErrorCategory
}

// Error は人間向けエラーメッセージを返す。トークン等の機微情報は含めない。
func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("kintone API error: HTTP ")
	b.WriteString(strconv.Itoa(e.HTTPStatus))
	if e.Code != "" {
		b.WriteString(" (")
		b.WriteString(e.Code)
		b.WriteString(")")
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

// classify は HTTP ステータスと kintone コードからカテゴリを決定する。
func classify(status int, code string) ErrorCategory {
	switch {
	case status >= 200 && status < 300:
		return CategoryUnknown
	case status == http.StatusUnauthorized:
		return CategoryUnauthorized
	case status == http.StatusForbidden:
		return CategoryForbidden
	case status == http.StatusNotFound:
		return CategoryNotFound
	case status == http.StatusTooManyRequests:
		return CategoryRateLimited
	case status == http.StatusServiceUnavailable:
		return CategoryServerError
	case status >= 500:
		return CategoryServerError
	case status == http.StatusUnprocessableEntity || strings.HasPrefix(code, "CB_VA"):
		return CategoryValidation
	case status >= 400:
		return CategoryClientError
	default:
		return CategoryUnknown
	}
}

// parseRetryAfter は Retry-After ヘッダから待機時間を取得する。
// 数値（秒） / HTTP-date のいずれにも対応。不正値・空値は 0 を返す。
func parseRetryAfter(h http.Header, now func() time.Time) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		nowFn := now
		if nowFn == nil {
			nowFn = time.Now
		}
		d := t.Sub(nowFn())
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// truncateBody は body を maxLen バイトに切り詰める。デバッグ用 RawBody 用。
func truncateBody(b []byte, maxLen int) []byte {
	if len(b) <= maxLen {
		return b
	}
	out := make([]byte, maxLen)
	copy(out, b[:maxLen])
	return out
}

// 未使用 import 警告回避（fmt は将来用）
var _ = fmt.Sprintf
