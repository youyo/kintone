package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
	"github.com/youyo/kintone/internal/kintoneapi"
)

// CA-Describe-1: 正常
func TestOpsAppDescribe_OK(t *testing.T) {
	stub := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return &kintoneapi.GetAppResponse{
				AppID: "42", Code: "x", Name: "テスト",
			}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			if req.Lang != "ja" {
				t.Errorf("lang=%q want ja", req.Lang)
			}
			return &kintoneapi.GetFormFieldsResponse{
				Properties: map[string]map[string]any{"name": {"type": "SINGLE_LINE_TEXT"}},
				Revision:   "1",
			}, nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "app", "describe", "--app", "42", "--lang", "ja",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / out=%s", err, out.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			App struct {
				AppID string `json:"app_id"`
				Name  string `json:"name"`
			} `json:"app"`
			Fields   map[string]map[string]any `json:"fields"`
			Revision string                    `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v / %s", err, out.String())
	}
	if !got.OK || got.Data.App.AppID != "42" || got.Data.App.Name != "テスト" {
		t.Errorf("got=%+v", got)
	}
}

// CA-Describe-2: --app 必須
func TestOpsAppDescribe_MissingApp(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"ops", "app", "describe"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CA-Describe-3: API エラー透過 → KINTONE_UNAUTHORIZED
func TestOpsAppDescribe_APIError(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 401, Category: kintoneapi.CategoryUnauthorized, Message: "auth"}
	stub := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return nil, apiErr
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"ops", "app", "describe", "--app", "1"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"KINTONE_UNAUTHORIZED"`) {
		t.Errorf("expected KINTONE_UNAUTHORIZED: %s", out.String())
	}
}
