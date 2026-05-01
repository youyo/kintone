package cli

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/resolver"
	"github.com/youyo/kintone/internal/service/operations"
	"github.com/youyo/kintone/internal/store"
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

	// resolver の AmbiguousError → RESOLVER_*_AMBIGUOUS（候補を details に含める / M08）
	var ambErr *resolver.AmbiguousError
	if errors.As(err, &ambErr) {
		return resolverAmbiguousOutput(ambErr)
	}

	// resolver の NotFoundError → RESOLVER_*_NOT_FOUND（M08）
	var nfErr *resolver.NotFoundError
	if errors.As(err, &nfErr) {
		return resolverNotFoundOutput(nfErr)
	}

	// resolver の sentinel sentry → RESOLVER_APP_LIST_TOO_LARGE / USAGE
	if errors.Is(err, resolver.ErrAppListTooLarge) {
		return &output.Error{Code: "RESOLVER_APP_LIST_TOO_LARGE", Message: err.Error()}
	}
	if errors.Is(err, resolver.ErrEmptyRef) || errors.Is(err, resolver.ErrInvalidAppID) {
		return &output.Error{Code: "USAGE", Message: err.Error()}
	}

	// operations の人為ミス系 sentinel → USAGE（CLI では USAGE で揃える / M08 advisor #1）
	switch {
	case errors.Is(err, operations.ErrInvalidApp),
		errors.Is(err, operations.ErrConflictingAppRef),
		errors.Is(err, operations.ErrConflictingUpdateKeyFieldRef),
		errors.Is(err, operations.ErrEmptyRecord),
		errors.Is(err, operations.ErrEmptyRecords),
		errors.Is(err, operations.ErrConflictingRecords),
		errors.Is(err, operations.ErrMissingUpdateKey),
		errors.Is(err, operations.ErrConflictingUpdateKey),
		errors.Is(err, operations.ErrEmptyIDs),
		errors.Is(err, operations.ErrInvalidID),
		errors.Is(err, operations.ErrRevisionsLengthMismatch):
		return &output.Error{Code: "USAGE", Message: err.Error()}
	case errors.Is(err, operations.ErrResolverUnavailable):
		return &output.Error{Code: "INTERNAL", Message: err.Error()}
	}

	// OAuth エラーマッピング（M09）
	// sentinel 順序: 具体的なもの → 汎用なもの
	if errors.Is(err, oauth.ErrStateMismatch) {
		return &output.Error{Code: "OAUTH_STATE_MISMATCH", Message: err.Error()}
	}
	if errors.Is(err, oauth.ErrCallbackTimeout) {
		return &output.Error{Code: "OAUTH_CALLBACK_TIMEOUT", Message: err.Error()}
	}
	if errors.Is(err, oauth.ErrRefreshTokenRevoked) {
		return &output.Error{Code: "OAUTH_REFRESH_REVOKED", Message: err.Error()}
	}
	if errors.Is(err, oauth.ErrTokenExpired) {
		return &output.Error{Code: "KINTONE_UNAUTHORIZED", Message: err.Error()}
	}
	if errors.Is(err, oauth.ErrInvalidRedirectURL) {
		return &output.Error{Code: "USAGE", Message: err.Error()}
	}
	if errors.Is(err, oauth.ErrMissingClientCredentials) {
		return &output.Error{Code: "USAGE", Message: err.Error()}
	}

	// *OAuthError（provider からのエラーレスポンス）
	var oauthErr *oauth.OAuthError
	if errors.As(err, &oauthErr) {
		details := map[string]any{
			"provider_code": oauthErr.Code,
			"http_status":   oauthErr.HTTPStatus,
		}
		if oauthErr.Description != "" {
			details["description"] = oauthErr.Description
		}
		return &output.Error{
			Code:    "OAUTH_PROVIDER_ERROR",
			Message: oauthErr.Error(),
			Details: details,
		}
	}

	// store init handled エラー（Phase 8）
	// store init は RunE 内で output.Failure を直接書き込み済み。
	// ExecuteWith が再度 output.Failure を書かないよう nil を返す（二重書き防止）。
	type handledError interface {
		IsHandled() bool
	}
	var he handledError
	if errors.As(err, &he) && he.IsHandled() {
		return nil
	}

	// store / idproxy の sentinel（Phase 6d）
	// USAGE 系（設定ミス・未対応組合せ）
	//
	// errorWithDetails は store init 専用の handled エラーから details を取り出す interface。
	// cli/store パッケージへの直接 import を避け、interface 経由で details を取得する。
	type errorWithDetails interface {
		error
		DetailMap() map[string]any
	}
	switch {
	case errors.Is(err, store.ErrTableNotFound):
		oe := &output.Error{Code: "STORE_TABLE_NOT_FOUND", Message: err.Error()}
		var ed errorWithDetails
		if errors.As(err, &ed) {
			oe.Details = ed.DetailMap()
		}
		return oe
	case errors.Is(err, store.ErrGSIMissing):
		oe := &output.Error{Code: "STORE_GSI_MISSING", Message: err.Error()}
		var ed errorWithDetails
		if errors.As(err, &ed) {
			oe.Details = ed.DetailMap()
		}
		return oe
	case errors.Is(err, store.ErrTTLDisabled):
		oe := &output.Error{Code: "STORE_TTL_DISABLED", Message: err.Error()}
		var ed errorWithDetails
		if errors.As(err, &ed) {
			oe.Details = ed.DetailMap()
		}
		return oe
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

	msg := err.Error()
	if isUsageError(msg) {
		return &output.Error{Code: "USAGE", Message: msg}
	}
	return &output.Error{Code: "INTERNAL", Message: msg}
}

// resolverAmbiguousOutput は AmbiguousError を output.Error に変換する。
//
// Kind ("app" | "field") に応じて RESOLVER_APP_AMBIGUOUS / RESOLVER_FIELD_AMBIGUOUS を返し、
// 候補は details.candidates に配列で含める。
func resolverAmbiguousOutput(e *resolver.AmbiguousError) *output.Error {
	code := "RESOLVER_APP_AMBIGUOUS"
	if e.Kind == "field" {
		code = "RESOLVER_FIELD_AMBIGUOUS"
	}
	candidates := make([]map[string]any, 0, len(e.Candidates))
	for _, c := range e.Candidates {
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
		candidates = append(candidates, m)
	}
	return &output.Error{
		Code:    code,
		Message: e.Error(),
		Details: map[string]any{
			"kind":       e.Kind,
			"ref":        e.Ref,
			"candidates": candidates,
		},
	}
}

// resolverNotFoundOutput は NotFoundError を output.Error に変換する。
func resolverNotFoundOutput(e *resolver.NotFoundError) *output.Error {
	code := "RESOLVER_APP_NOT_FOUND"
	if e.Kind == "field" {
		code = "RESOLVER_FIELD_NOT_FOUND"
	}
	candidates := make([]map[string]any, 0, len(e.Candidates))
	for _, c := range e.Candidates {
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
		candidates = append(candidates, m)
	}
	return &output.Error{
		Code:    code,
		Message: e.Error(),
		Details: map[string]any{
			"kind":       e.Kind,
			"ref":        e.Ref,
			"candidates": candidates,
		},
	}
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
