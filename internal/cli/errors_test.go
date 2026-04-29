package cli_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/cli"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/kintoneapi"
)

// E-1: cobra の unknown command エラーが USAGE にマップされる
func TestMapToOutputError_UnknownCommand(t *testing.T) {
	err := stderrors.New(`unknown command "foo" for "kintone"`)
	oe := cli.MapToOutputError(err)
	if oe == nil {
		t.Fatal("expected non-nil *output.Error")
	}
	if oe.Code != "USAGE" {
		t.Errorf("expected code=USAGE, got %q", oe.Code)
	}
}

// E-2: 不明エラーが INTERNAL にマップされる
func TestMapToOutputError_Unknown(t *testing.T) {
	err := stderrors.New("boom")
	oe := cli.MapToOutputError(err)
	if oe == nil {
		t.Fatal("expected non-nil *output.Error")
	}
	if oe.Code != "INTERNAL" {
		t.Errorf("expected code=INTERNAL, got %q", oe.Code)
	}
	if oe.Message != "boom" {
		t.Errorf("expected message=boom, got %q", oe.Message)
	}
}

// E-3: nil 入力は nil を返す
func TestMapToOutputError_Nil(t *testing.T) {
	oe := cli.MapToOutputError(nil)
	if oe != nil {
		t.Errorf("expected nil for nil input, got %v", oe)
	}
}

// E-4: ProfileNotFoundError → CONFIG_PROFILE_NOT_FOUND にマップ
func TestMapToOutputError_ProfileNotFound(t *testing.T) {
	err := &config.ProfileNotFoundError{Name: "prod", Path: "/etc/x.toml"}
	oe := cli.MapToOutputError(err)
	if oe == nil {
		t.Fatal("expected non-nil *output.Error")
	}
	if oe.Code != "CONFIG_PROFILE_NOT_FOUND" {
		t.Errorf("Code = %q, want CONFIG_PROFILE_NOT_FOUND", oe.Code)
	}
	if got, _ := oe.Details["name"].(string); got != "prod" {
		t.Errorf("Details.name = %v, want prod", oe.Details["name"])
	}
	if got, _ := oe.Details["path"].(string); got != "/etc/x.toml" {
		t.Errorf("Details.path = %v, want /etc/x.toml", oe.Details["path"])
	}
}

// E-5: ProfileNotFoundError でも Path 空のケース
func TestMapToOutputError_ProfileNotFoundNoPath(t *testing.T) {
	err := &config.ProfileNotFoundError{Name: "x"}
	oe := cli.MapToOutputError(err)
	if oe.Code != "CONFIG_PROFILE_NOT_FOUND" {
		t.Errorf("Code = %q", oe.Code)
	}
	// Path 空時は details に含まれない
	if _, exists := oe.Details["path"]; exists {
		t.Errorf("expected no path key when empty, got %v", oe.Details)
	}
}

// E-6: ParseError → CONFIG_PARSE_ERROR
func TestMapToOutputError_ParseError(t *testing.T) {
	cause := stderrors.New("syntax")
	err := &config.ParseError{Path: "/tmp/x.toml", Err: cause}
	oe := cli.MapToOutputError(err)
	if oe.Code != "CONFIG_PARSE_ERROR" {
		t.Errorf("Code = %q, want CONFIG_PARSE_ERROR", oe.Code)
	}
	if got, _ := oe.Details["path"].(string); got != "/tmp/x.toml" {
		t.Errorf("Details.path = %v", oe.Details["path"])
	}
	if got, _ := oe.Details["cause"].(string); got != "syntax" {
		t.Errorf("Details.cause = %v", oe.Details["cause"])
	}
}

// E-7: AlreadyExistsError → CONFIG_ALREADY_EXISTS
func TestMapToOutputError_AlreadyExists(t *testing.T) {
	err := &config.AlreadyExistsError{Path: "/tmp/x.toml"}
	oe := cli.MapToOutputError(err)
	if oe.Code != "CONFIG_ALREADY_EXISTS" {
		t.Errorf("Code = %q, want CONFIG_ALREADY_EXISTS", oe.Code)
	}
	if got, _ := oe.Details["path"].(string); got != "/tmp/x.toml" {
		t.Errorf("Details.path = %v", oe.Details["path"])
	}
}

// E-8: NotFoundError → CONFIG_NOT_FOUND
func TestMapToOutputError_NotFound(t *testing.T) {
	err := &config.NotFoundError{Path: "/tmp/x.toml"}
	oe := cli.MapToOutputError(err)
	if oe.Code != "CONFIG_NOT_FOUND" {
		t.Errorf("Code = %q, want CONFIG_NOT_FOUND", oe.Code)
	}
	if got, _ := oe.Details["path"].(string); got != "/tmp/x.toml" {
		t.Errorf("Details.path = %v", oe.Details["path"])
	}
}

// E-9: ラップされた config エラーも errors.As で検出される
func TestMapToOutputError_WrappedConfigError(t *testing.T) {
	inner := &config.ParseError{Path: "/x.toml", Err: stderrors.New("e")}
	wrapped := fmt.Errorf("config: load: %w", inner)
	oe := cli.MapToOutputError(wrapped)
	if oe.Code != "CONFIG_PARSE_ERROR" {
		t.Errorf("expected CONFIG_PARSE_ERROR for wrapped error, got %q", oe.Code)
	}
}

// E-10〜15: kintoneapi.APIError → KINTONE_* マッピング
func TestMapToOutputError_KintoneAPIError(t *testing.T) {
	cases := []struct {
		name     string
		err      *kintoneapi.APIError
		wantCode string
	}{
		{"E-10 401", &kintoneapi.APIError{HTTPStatus: 401, Code: "CB_AU01", Message: "auth", Category: kintoneapi.CategoryUnauthorized}, "KINTONE_UNAUTHORIZED"},
		{"E-11 403", &kintoneapi.APIError{HTTPStatus: 403, Category: kintoneapi.CategoryForbidden}, "KINTONE_FORBIDDEN"},
		{"E-12 404", &kintoneapi.APIError{HTTPStatus: 404, Category: kintoneapi.CategoryNotFound}, "KINTONE_NOT_FOUND"},
		{"E-13 429", &kintoneapi.APIError{HTTPStatus: 429, Category: kintoneapi.CategoryRateLimited, RetryAfter: 2 * time.Second}, "KINTONE_RATE_LIMITED"},
		{"E-14 422", &kintoneapi.APIError{HTTPStatus: 422, Category: kintoneapi.CategoryValidation}, "KINTONE_VALIDATION"},
		{"E-15 500", &kintoneapi.APIError{HTTPStatus: 500, Category: kintoneapi.CategoryServerError}, "KINTONE_INTERNAL"},
		{"client error fallback", &kintoneapi.APIError{HTTPStatus: 418, Category: kintoneapi.CategoryClientError}, "KINTONE_VALIDATION"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			oe := cli.MapToOutputError(c.err)
			if oe == nil {
				t.Fatalf("nil")
			}
			if oe.Code != c.wantCode {
				t.Errorf("Code=%q want %q", oe.Code, c.wantCode)
			}
			if got, _ := oe.Details["http_status"].(int); got != c.err.HTTPStatus {
				t.Errorf("http_status=%v", oe.Details["http_status"])
			}
		})
	}
}

// E-13 補強: Retry-After が details に入る
func TestMapToOutputError_KintoneRetryAfter(t *testing.T) {
	err := &kintoneapi.APIError{HTTPStatus: 429, Category: kintoneapi.CategoryRateLimited, RetryAfter: 2 * time.Second}
	oe := cli.MapToOutputError(err)
	if got, _ := oe.Details["retry_after_sec"].(int); got != 2 {
		t.Errorf("retry_after_sec=%v want 2", oe.Details["retry_after_sec"])
	}
}

// E-10 補強: kintone_code / kintone_id が details に入る
func TestMapToOutputError_KintoneCodeID(t *testing.T) {
	err := &kintoneapi.APIError{HTTPStatus: 401, Code: "CB_AU01", ID: "req-123", Category: kintoneapi.CategoryUnauthorized}
	oe := cli.MapToOutputError(err)
	if got, _ := oe.Details["kintone_code"].(string); got != "CB_AU01" {
		t.Errorf("kintone_code=%v", oe.Details["kintone_code"])
	}
	if got, _ := oe.Details["kintone_id"].(string); got != "req-123" {
		t.Errorf("kintone_id=%v", oe.Details["kintone_id"])
	}
}

// E-16: url.Error timeout → KINTONE_NETWORK
func TestMapToOutputError_URLErrorTimeout(t *testing.T) {
	urlErr := &url.Error{Op: "Get", URL: "https://x.cybozu.com/", Err: context.DeadlineExceeded}
	oe := cli.MapToOutputError(urlErr)
	if oe.Code != "KINTONE_NETWORK" {
		t.Errorf("Code=%q want KINTONE_NETWORK", oe.Code)
	}
	if got, _ := oe.Details["timeout"].(bool); !got {
		t.Errorf("timeout=%v", oe.Details["timeout"])
	}
}

// E-16 補強: context.DeadlineExceeded 単体
func TestMapToOutputError_ContextDeadline(t *testing.T) {
	oe := cli.MapToOutputError(context.DeadlineExceeded)
	if oe.Code != "KINTONE_NETWORK" {
		t.Errorf("Code=%q want KINTONE_NETWORK", oe.Code)
	}
}

// E-17: wrap された APIError も errors.As で検出
func TestMapToOutputError_WrappedAPIError(t *testing.T) {
	inner := &kintoneapi.APIError{HTTPStatus: 401, Category: kintoneapi.CategoryUnauthorized}
	wrapped := fmt.Errorf("getApp: %w", inner)
	oe := cli.MapToOutputError(wrapped)
	if oe.Code != "KINTONE_UNAUTHORIZED" {
		t.Errorf("Code=%q want KINTONE_UNAUTHORIZED", oe.Code)
	}
}
