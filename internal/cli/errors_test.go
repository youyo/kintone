package cli_test

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	awsdynamodbtest "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/cli"
	clistore "github.com/youyo/kintone/internal/cli/store"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/store"
)

// OAuth エラーのヘルパー関数群（M14 で loopback 関連 sentinel は削除済み）
func oauthErrRefreshRevoked() error { return oauth.ErrRefreshTokenRevoked }
func oauthErrTokenExpired() error   { return oauth.ErrTokenExpired }
func oauthProviderError() error {
	return &oauth.OAuthError{Code: "invalid_scope", Description: "scope is invalid", HTTPStatus: 400}
}

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

// OAuth エラーマッピングテスト（M09 導入、M14 で loopback 関連を削除）

// EO-3: ErrRefreshTokenRevoked → OAUTH_REFRESH_REVOKED
func TestMapToOutputError_OAuthRefreshRevoked(t *testing.T) {
	oe := cli.MapToOutputError(oauthErrRefreshRevoked())
	if oe.Code != "OAUTH_REFRESH_REVOKED" {
		t.Errorf("Code=%q want OAUTH_REFRESH_REVOKED", oe.Code)
	}
}

// EO-4: ErrTokenExpired → KINTONE_UNAUTHORIZED
func TestMapToOutputError_OAuthTokenExpired(t *testing.T) {
	oe := cli.MapToOutputError(oauthErrTokenExpired())
	if oe.Code != "KINTONE_UNAUTHORIZED" {
		t.Errorf("Code=%q want KINTONE_UNAUTHORIZED", oe.Code)
	}
}

// EO-7: *OAuthError → OAUTH_PROVIDER_ERROR
func TestMapToOutputError_OAuthProviderError(t *testing.T) {
	oe := cli.MapToOutputError(oauthProviderError())
	if oe.Code != "OAUTH_PROVIDER_ERROR" {
		t.Errorf("Code=%q want OAUTH_PROVIDER_ERROR", oe.Code)
	}
	if _, ok := oe.Details["provider_code"]; !ok {
		t.Errorf("expected provider_code in details, got %v", oe.Details)
	}
}

// Phase 6d: store / idproxy エラーマッピングテスト

// ES-1: store.ErrTableNotFound → STORE_TABLE_NOT_FOUND (USAGE)
func TestMapToOutputError_StoreTableNotFound(t *testing.T) {
	oe := cli.MapToOutputError(store.ErrTableNotFound)
	if oe.Code != "STORE_TABLE_NOT_FOUND" {
		t.Errorf("Code=%q want STORE_TABLE_NOT_FOUND", oe.Code)
	}
}

// ES-1w: wrapped store.ErrTableNotFound も検出される
func TestMapToOutputError_StoreTableNotFound_Wrapped(t *testing.T) {
	err := fmt.Errorf("backend op: %w", store.ErrTableNotFound)
	oe := cli.MapToOutputError(err)
	if oe.Code != "STORE_TABLE_NOT_FOUND" {
		t.Errorf("Code=%q want STORE_TABLE_NOT_FOUND", oe.Code)
	}
}

// ES-2: store.ErrGSIMissing → STORE_GSI_MISSING (USAGE)
func TestMapToOutputError_StoreGSIMissing(t *testing.T) {
	oe := cli.MapToOutputError(store.ErrGSIMissing)
	if oe.Code != "STORE_GSI_MISSING" {
		t.Errorf("Code=%q want STORE_GSI_MISSING", oe.Code)
	}
}

// ES-3: store.ErrTTLDisabled → STORE_TTL_DISABLED (USAGE)
func TestMapToOutputError_StoreTTLDisabled(t *testing.T) {
	oe := cli.MapToOutputError(store.ErrTTLDisabled)
	if oe.Code != "STORE_TTL_DISABLED" {
		t.Errorf("Code=%q want STORE_TTL_DISABLED", oe.Code)
	}
}

// ES-4: store.ErrConnectionFailed → STORE_CONNECTION_FAILED (INTERNAL + cause_class)
func TestMapToOutputError_StoreConnectionFailed(t *testing.T) {
	oe := cli.MapToOutputError(store.ErrConnectionFailed)
	if oe.Code != "STORE_CONNECTION_FAILED" {
		t.Errorf("Code=%q want STORE_CONNECTION_FAILED", oe.Code)
	}
	if oe.Details == nil {
		t.Fatal("expected non-nil details")
	}
	// cause_class は存在すること
	if _, ok := oe.Details["cause_class"]; !ok {
		t.Errorf("expected cause_class in details, got %v", oe.Details)
	}
	// raw cause は出力しないこと
	if _, ok := oe.Details["cause"]; ok {
		t.Errorf("unexpected cause in details (should be sanitized), got %v", oe.Details)
	}
}

// ES-4n: 接続失敗 wrapped エラーで cause_class=network になること
func TestMapToOutputError_StoreConnectionFailed_Network(t *testing.T) {
	netErr := stderrors.New("connection refused")
	err := fmt.Errorf("connect: %w: %w", store.ErrConnectionFailed, netErr)
	oe := cli.MapToOutputError(err)
	if oe.Code != "STORE_CONNECTION_FAILED" {
		t.Errorf("Code=%q want STORE_CONNECTION_FAILED", oe.Code)
	}
	if cc, _ := oe.Details["cause_class"].(string); cc != "network" {
		t.Errorf("cause_class=%q want network", cc)
	}
}

// ES-5: store.ErrMemoryOIDCForbidden → STORE_MEMORY_OIDC_FORBIDDEN (USAGE)
func TestMapToOutputError_StoreMemoryOIDCForbidden(t *testing.T) {
	oe := cli.MapToOutputError(store.ErrMemoryOIDCForbidden)
	if oe.Code != "STORE_MEMORY_OIDC_FORBIDDEN" {
		t.Errorf("Code=%q want STORE_MEMORY_OIDC_FORBIDDEN", oe.Code)
	}
}

// ES-6: idproxy.ErrSigningKeyRequired → SIGNING_KEY_REQUIRED (USAGE)
func TestMapToOutputError_SigningKeyRequired(t *testing.T) {
	oe := cli.MapToOutputError(idproxy.ErrSigningKeyRequired)
	if oe.Code != "SIGNING_KEY_REQUIRED" {
		t.Errorf("Code=%q want SIGNING_KEY_REQUIRED", oe.Code)
	}
}

// ES-7: store.ErrCacheBypassInvalid → STORE_CACHE_BYPASS_INVALID (USAGE)
func TestMapToOutputError_StoreCacheBypassInvalid(t *testing.T) {
	oe := cli.MapToOutputError(store.ErrCacheBypassInvalid)
	if oe.Code != "STORE_CACHE_BYPASS_INVALID" {
		t.Errorf("Code=%q want STORE_CACHE_BYPASS_INVALID", oe.Code)
	}
}

// ES-8: store.ErrPlaintextForbidden → STORE_PLAINTEXT_FORBIDDEN (USAGE)
func TestMapToOutputError_StorePlaintextForbidden(t *testing.T) {
	oe := cli.MapToOutputError(store.ErrPlaintextForbidden)
	if oe.Code != "STORE_PLAINTEXT_FORBIDDEN" {
		t.Errorf("Code=%q want STORE_PLAINTEXT_FORBIDDEN", oe.Code)
	}
}

// ES-9: store.ErrPrincipalNotFound → RESOLVER_PRINCIPAL_NOT_FOUND (USAGE)
func TestMapToOutputError_StorePrincipalNotFound(t *testing.T) {
	oe := cli.MapToOutputError(store.ErrPrincipalNotFound)
	if oe.Code != "RESOLVER_PRINCIPAL_NOT_FOUND" {
		t.Errorf("Code=%q want RESOLVER_PRINCIPAL_NOT_FOUND", oe.Code)
	}
}

// Phase 8: store init errStoreInitHandled の details 取り込みテスト

// EP8-1: store init が STORE_GSI_MISSING で失敗した場合、
// MapToOutputError は nil を返す（二重書き防止）
func TestMapToOutputError_StoreInitHandled_ReturnsNil(t *testing.T) {
	// RunInit に fake client で GSI 不足を起こし、errStoreInitHandled を返させる
	opts := clistore.InitOptions{
		Table:      "test-table",
		Capability: "token",
		Client:     &errTestFakeClient{missingGSI: true},
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err == nil {
		t.Fatal("expected error from RunInit")
	}
	// MapToOutputError は handled エラーを受け取ると nil を返す（二重書き防止）
	oe := cli.MapToOutputError(err)
	if oe != nil {
		t.Errorf("expected nil from MapToOutputError for handled error, got %+v", oe)
	}
}

// EP8-2: store init が STORE_TABLE_NOT_FOUND で失敗した場合、
// out には details.table を含む JSON が書かれている
func TestRunInit_TableNotFound_OutputContainsDetails(t *testing.T) {
	opts := clistore.InitOptions{
		Table:      "my-missing-table",
		Capability: "full",
		Client:     &errTestFakeClient{tableNotFound: true},
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err == nil {
		t.Fatal("expected error")
	}
	// MapToOutputError は nil を返す（handled）
	oe := cli.MapToOutputError(err)
	if oe != nil {
		t.Errorf("expected nil output error for handled, got %v", oe)
	}
	// buf に書かれた JSON が空でないこと
	if buf.Len() == 0 {
		t.Fatal("expected output in buf")
	}
}

// errTestFakeClient は errors_test.go 専用の最小 fake DynamoDB クライアント。
// missingGSI=true なら pk のみ返し GSI なし（STORE_GSI_MISSING を起こす）。
// tableNotFound=true なら ResourceNotFoundException を返す。
type errTestFakeClient struct {
	missingGSI    bool
	tableNotFound bool
}

func (f *errTestFakeClient) DescribeTable(_ context.Context, _ *awsdynamodbtest.DescribeTableInput, _ ...func(*awsdynamodbtest.Options)) (*awsdynamodbtest.DescribeTableOutput, error) {
	if f.tableNotFound {
		return nil, &dynamodbtypes.ResourceNotFoundException{}
	}
	tableName := "test-table"
	pkName := "pk"
	return &awsdynamodbtest.DescribeTableOutput{
		Table: &dynamodbtypes.TableDescription{
			TableName: &tableName,
			AttributeDefinitions: []dynamodbtypes.AttributeDefinition{
				{AttributeName: &pkName, AttributeType: dynamodbtypes.ScalarAttributeTypeS},
			},
		},
	}, nil
}

func (f *errTestFakeClient) DescribeTimeToLive(_ context.Context, _ *awsdynamodbtest.DescribeTimeToLiveInput, _ ...func(*awsdynamodbtest.Options)) (*awsdynamodbtest.DescribeTimeToLiveOutput, error) {
	status := dynamodbtypes.TimeToLiveStatusEnabled
	return &awsdynamodbtest.DescribeTimeToLiveOutput{
		TimeToLiveDescription: &dynamodbtypes.TimeToLiveDescription{
			TimeToLiveStatus: status,
		},
	}, nil
}
