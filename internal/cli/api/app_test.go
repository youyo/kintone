package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
	"github.com/youyo/kintone/internal/kintoneapi"
)

// CA-1: app get 正常（snake_case 化）
func TestCmd_AppGet_OK(t *testing.T) {
	stub := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			if req.ID != 42 {
				t.Errorf("req=%+v", req)
			}
			return &kintoneapi.GetAppResponse{
				AppID: "42", Code: "myapp", Name: "テスト", Description: "desc",
			}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"api", "app", "get", "--app", "42"}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / %s", err, out.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			AppID string `json:"app_id"`
			Name  string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v / %s", err, out.String())
	}
	if got.Data.AppID != "42" || got.Data.Name != "テスト" {
		t.Errorf("got=%+v", got)
	}
}

// CA-2: app fields 正常
func TestCmd_AppFields_OK(t *testing.T) {
	stub := &stubAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			if req.App != 42 || req.Lang != "ja" {
				t.Errorf("req=%+v", req)
			}
			return &kintoneapi.GetFormFieldsResponse{
				Properties: map[string]map[string]any{
					"name": {"type": "SINGLE_LINE_TEXT"},
				},
				Revision: "5",
			}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"api", "app", "fields", "--app", "42", "--lang", "ja"}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / %s", err, out.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Properties map[string]map[string]any `json:"properties"`
			Revision   string                    `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v / %s", err, out.String())
	}
	if got.Data.Revision != "5" {
		t.Errorf("revision=%q", got.Data.Revision)
	}
	if _, ok := got.Data.Properties["name"]; !ok {
		t.Errorf("properties=%v", got.Data.Properties)
	}
}

// CA-3: app describe 正常
func TestCmd_AppDescribe_OK(t *testing.T) {
	stub := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return &kintoneapi.GetAppResponse{AppID: "42", Name: "テスト", Code: "myapp"}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{
				Properties: map[string]map[string]any{
					"name": {"type": "SINGLE_LINE_TEXT"},
				},
				Revision: "5",
			}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"api", "app", "describe", "--app", "42"}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / %s", err, out.String())
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
	if got.Data.App.AppID != "42" || got.Data.App.Name != "テスト" || got.Data.Revision != "5" {
		t.Errorf("got=%+v", got)
	}
}

// CA-4: app describe 401 → KINTONE_UNAUTHORIZED
func TestCmd_AppDescribe_APIError(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 401, Category: kintoneapi.CategoryUnauthorized, Message: "auth"}
	stub := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return nil, apiErr
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"api", "app", "describe", "--app", "42"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"KINTONE_UNAUTHORIZED"`) {
		t.Errorf("output=%s", out.String())
	}
}
