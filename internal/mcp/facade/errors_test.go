package facade_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/mcp/facade"
	"github.com/youyo/kintone/internal/service/operations"
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
