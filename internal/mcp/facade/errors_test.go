package facade_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/mcp/facade"
	serviceapi "github.com/youyo/kintone/internal/service/api"
	"github.com/youyo/kintone/internal/service/operations"
	"github.com/youyo/kintone/internal/store"
)

// TestMapError は facade.MapError が各種エラーを正しく output.Error に変換することを確認。
//
// M05 ハンドオフ最重要事項: facade 経路は cli.MapToOutputError を使えないため、
// 専用 mapper の網羅性が品質を左右する。
func TestMapError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"FE-1 ErrInvalidApp", operations.ErrInvalidApp, "INVALID_PARAMS"},
		{"FE-2 ErrEmptyRecords", operations.ErrEmptyRecords, "INVALID_PARAMS"},
		{"FE-3 ErrConflictingRecords", operations.ErrConflictingRecords, "INVALID_PARAMS"},
		{"FE-4 ErrMissingUpdateKey", operations.ErrMissingUpdateKey, "INVALID_PARAMS"},
		{"FE-5 ErrConflictingUpdateKey", operations.ErrConflictingUpdateKey, "INVALID_PARAMS"},
		{"FE-6 ErrEmptyRecord", operations.ErrEmptyRecord, "INVALID_PARAMS"},
		{"FE-7 ErrEmptyIDs", operations.ErrEmptyIDs, "INVALID_PARAMS"},
		{"FE-8 ErrInvalidID", operations.ErrInvalidID, "INVALID_PARAMS"},
		{"FE-9 ErrRevisionsLengthMismatch", operations.ErrRevisionsLengthMismatch, "INVALID_PARAMS"},
		{"FE-10 401", &kintoneapi.APIError{HTTPStatus: 401, Category: kintoneapi.CategoryUnauthorized}, "KINTONE_UNAUTHORIZED"},
		{"FE-10b 403", &kintoneapi.APIError{HTTPStatus: 403, Category: kintoneapi.CategoryForbidden}, "KINTONE_FORBIDDEN"},
		{"FE-10c 404", &kintoneapi.APIError{HTTPStatus: 404, Category: kintoneapi.CategoryNotFound}, "KINTONE_NOT_FOUND"},
		{"FE-10d 422", &kintoneapi.APIError{HTTPStatus: 422, Category: kintoneapi.CategoryValidation}, "KINTONE_VALIDATION"},
		{"FE-10e 400", &kintoneapi.APIError{HTTPStatus: 400, Category: kintoneapi.CategoryClientError}, "KINTONE_VALIDATION"},
		{"FE-10f 500", &kintoneapi.APIError{HTTPStatus: 500, Category: kintoneapi.CategoryServerError}, "KINTONE_INTERNAL"},
		{"FE-13 context.DeadlineExceeded", context.DeadlineExceeded, "KINTONE_NETWORK"},
		{"FE-13b context.Canceled", context.Canceled, "KINTONE_NETWORK"},
		{"FE-15 ErrAuthRequired", serviceapi.ErrAuthRequired, "AUTH_REQUIRED"},
		{"FE-15b ErrAuthRequired wrapped", fmt.Errorf("wrap: %w", serviceapi.ErrAuthRequired), "AUTH_REQUIRED"},
		{"FE-14 unknown", errors.New("boom"), "INTERNAL"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := facade.MapError(tc.err)
			if got == nil {
				t.Fatalf("nil")
			}
			if got.Code != tc.wantCode {
				t.Errorf("code=%q want %q", got.Code, tc.wantCode)
			}
			if got.Message == "" {
				t.Errorf("message empty")
			}
		})
	}
}

// FE-11: 429 + RetryAfter は details.retry_after_sec に値が入る
func TestMapError_RateLimitedDetails(t *testing.T) {
	t.Parallel()
	apiErr := &kintoneapi.APIError{
		HTTPStatus: 429,
		Code:       "GAIA_TM01",
		ID:         "id-1",
		Category:   kintoneapi.CategoryRateLimited,
		RetryAfter: 5_000_000_000, // 5s
	}
	got := facade.MapError(apiErr)
	if got.Code != "KINTONE_RATE_LIMITED" {
		t.Fatalf("code=%q", got.Code)
	}
	if got.Details["retry_after_sec"] != 5 {
		t.Errorf("retry_after_sec=%v", got.Details["retry_after_sec"])
	}
	if got.Details["http_status"] != 429 {
		t.Errorf("http_status=%v", got.Details["http_status"])
	}
	if got.Details["kintone_code"] != "GAIA_TM01" {
		t.Errorf("kintone_code=%v", got.Details["kintone_code"])
	}
	if got.Details["kintone_id"] != "id-1" {
		t.Errorf("kintone_id=%v", got.Details["kintone_id"])
	}
}

// FE-12: *url.Error → KINTONE_NETWORK
func TestMapError_URLError(t *testing.T) {
	t.Parallel()
	urlErr := &url.Error{Op: "Get", URL: "https://x", Err: errors.New("boom")}
	got := facade.MapError(urlErr)
	if got.Code != "KINTONE_NETWORK" {
		t.Fatalf("code=%q", got.Code)
	}
	if got.Details["timeout"] != false {
		t.Errorf("timeout=%v", got.Details["timeout"])
	}
}

// nil 入力は nil を返す
func TestMapError_Nil(t *testing.T) {
	t.Parallel()
	if got := facade.MapError(nil); got != nil {
		t.Errorf("got=%v", got)
	}
}

// Phase 6d: store / idproxy エラーマッピングテスト（facade 経路）

// FSE-1: store.ErrTableNotFound → STORE_TABLE_NOT_FOUND
func TestMapError_StoreTableNotFound(t *testing.T) {
	t.Parallel()
	got := facade.MapError(store.ErrTableNotFound)
	if got.Code != "STORE_TABLE_NOT_FOUND" {
		t.Errorf("code=%q want STORE_TABLE_NOT_FOUND", got.Code)
	}
}

// FSE-1w: wrapped store.ErrTableNotFound も検出される
func TestMapError_StoreTableNotFound_Wrapped(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("backend: %w", store.ErrTableNotFound)
	got := facade.MapError(err)
	if got.Code != "STORE_TABLE_NOT_FOUND" {
		t.Errorf("code=%q want STORE_TABLE_NOT_FOUND", got.Code)
	}
}

// FSE-2: store.ErrGSIMissing → STORE_GSI_MISSING
func TestMapError_StoreGSIMissing(t *testing.T) {
	t.Parallel()
	got := facade.MapError(store.ErrGSIMissing)
	if got.Code != "STORE_GSI_MISSING" {
		t.Errorf("code=%q want STORE_GSI_MISSING", got.Code)
	}
}

// FSE-3: store.ErrTTLDisabled → STORE_TTL_DISABLED
func TestMapError_StoreTTLDisabled(t *testing.T) {
	t.Parallel()
	got := facade.MapError(store.ErrTTLDisabled)
	if got.Code != "STORE_TTL_DISABLED" {
		t.Errorf("code=%q want STORE_TTL_DISABLED", got.Code)
	}
}

// FSE-4: store.ErrConnectionFailed → STORE_CONNECTION_FAILED (INTERNAL + cause_class)
func TestMapError_StoreConnectionFailed(t *testing.T) {
	t.Parallel()
	got := facade.MapError(store.ErrConnectionFailed)
	if got.Code != "STORE_CONNECTION_FAILED" {
		t.Errorf("code=%q want STORE_CONNECTION_FAILED", got.Code)
	}
	if got.Details == nil {
		t.Fatal("expected non-nil details")
	}
	if _, ok := got.Details["cause_class"]; !ok {
		t.Errorf("expected cause_class in details, got %v", got.Details)
	}
	if _, ok := got.Details["cause"]; ok {
		t.Errorf("unexpected cause in details (should be sanitized), got %v", got.Details)
	}
}

// FSE-5: store.ErrMemoryOIDCForbidden → STORE_MEMORY_OIDC_FORBIDDEN
func TestMapError_StoreMemoryOIDCForbidden(t *testing.T) {
	t.Parallel()
	got := facade.MapError(store.ErrMemoryOIDCForbidden)
	if got.Code != "STORE_MEMORY_OIDC_FORBIDDEN" {
		t.Errorf("code=%q want STORE_MEMORY_OIDC_FORBIDDEN", got.Code)
	}
}

// FSE-6: idproxy.ErrSigningKeyRequired → SIGNING_KEY_REQUIRED
func TestMapError_SigningKeyRequired(t *testing.T) {
	t.Parallel()
	got := facade.MapError(idproxy.ErrSigningKeyRequired)
	if got.Code != "SIGNING_KEY_REQUIRED" {
		t.Errorf("code=%q want SIGNING_KEY_REQUIRED", got.Code)
	}
}

// FSE-7: store.ErrCacheBypassInvalid → STORE_CACHE_BYPASS_INVALID
func TestMapError_StoreCacheBypassInvalid(t *testing.T) {
	t.Parallel()
	got := facade.MapError(store.ErrCacheBypassInvalid)
	if got.Code != "STORE_CACHE_BYPASS_INVALID" {
		t.Errorf("code=%q want STORE_CACHE_BYPASS_INVALID", got.Code)
	}
}

// FSE-8: store.ErrPlaintextForbidden → STORE_PLAINTEXT_FORBIDDEN
func TestMapError_StorePlaintextForbidden(t *testing.T) {
	t.Parallel()
	got := facade.MapError(store.ErrPlaintextForbidden)
	if got.Code != "STORE_PLAINTEXT_FORBIDDEN" {
		t.Errorf("code=%q want STORE_PLAINTEXT_FORBIDDEN", got.Code)
	}
}

// FSE-9: store.ErrPrincipalNotFound → RESOLVER_PRINCIPAL_NOT_FOUND
// M13: AuthRequiredError + builder → details.authorize_url を含む。
func TestMapErrorWithBuilder_AuthRequiredHasAuthorizeURL(t *testing.T) {
	t.Parallel()

	authErr := &serviceapi.AuthRequiredError{
		PrincipalID: "https://issuer:user-1",
		Domain:      "example.cybozu.com",
	}
	builder := func(pid string) string {
		return "https://mcp.example.com/oauth/kintone/start?principal_id=" + url.QueryEscape(pid)
	}
	got := facade.MapErrorWithBuilder(authErr, builder)
	if got == nil {
		t.Fatal("got nil")
	}
	if got.Code != "AUTH_REQUIRED" {
		t.Errorf("Code = %q, want AUTH_REQUIRED", got.Code)
	}
	if got.Details == nil {
		t.Fatal("Details is nil")
	}
	if got.Details["principal_id"] != "https://issuer:user-1" {
		t.Errorf("details.principal_id = %v", got.Details["principal_id"])
	}
	if got.Details["domain"] != "example.cybozu.com" {
		t.Errorf("details.domain = %v", got.Details["domain"])
	}
	authzURL, _ := got.Details["authorize_url"].(string)
	if authzURL == "" || !contains(authzURL, "principal_id=") {
		t.Errorf("details.authorize_url = %q", authzURL)
	}
}

// M13: AuthRequiredError + builder=nil → details に authorize_url 含まれない。
func TestMapErrorWithBuilder_AuthRequiredNoBuilder(t *testing.T) {
	t.Parallel()

	authErr := &serviceapi.AuthRequiredError{
		PrincipalID: "issuer:sub",
		Domain:      "example.cybozu.com",
	}
	got := facade.MapErrorWithBuilder(authErr, nil)
	if got.Code != "AUTH_REQUIRED" {
		t.Errorf("Code = %q, want AUTH_REQUIRED", got.Code)
	}
	if got.Details != nil {
		if _, ok := got.Details["authorize_url"]; ok {
			t.Errorf("authorize_url should not be present when builder is nil")
		}
	}
}

// M13: plain ErrAuthRequired → AUTH_REQUIRED + details なし（M11 後方互換）。
func TestMapErrorWithBuilder_PlainErrAuthRequired(t *testing.T) {
	t.Parallel()

	builder := func(string) string { return "https://example.com/start" }
	got := facade.MapErrorWithBuilder(serviceapi.ErrAuthRequired, builder)
	if got.Code != "AUTH_REQUIRED" {
		t.Errorf("Code = %q", got.Code)
	}
	if got.Details != nil {
		t.Errorf("plain ErrAuthRequired should not produce details, got %+v", got.Details)
	}
}

// M13: MapError (M11 互換) は AuthRequiredError でも details なしで code のみ返す。
func TestMapError_AuthRequiredErrorBackwardCompat(t *testing.T) {
	t.Parallel()

	authErr := &serviceapi.AuthRequiredError{PrincipalID: "p", Domain: "d"}
	got := facade.MapError(authErr)
	if got.Code != "AUTH_REQUIRED" {
		t.Errorf("Code = %q", got.Code)
	}
	// MapError は builder なし → details に authorize_url なし
	if got.Details != nil {
		if _, ok := got.Details["authorize_url"]; ok {
			t.Errorf("MapError(builder=nil): authorize_url should be absent")
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestMapError_StorePrincipalNotFound(t *testing.T) {
	t.Parallel()
	got := facade.MapError(store.ErrPrincipalNotFound)
	if got.Code != "RESOLVER_PRINCIPAL_NOT_FOUND" {
		t.Errorf("code=%q want RESOLVER_PRINCIPAL_NOT_FOUND", got.Code)
	}
}
