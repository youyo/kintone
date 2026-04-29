package facade_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/mcp/facade"
)

// FD-1: app_describe 成功
func TestAppDescribe_OK(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return &kintoneapi.GetAppResponse{AppID: "42", Code: "hr", Name: "人事"}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{
				Properties: map[string]map[string]any{
					"name": {"type": "SINGLE_LINE_TEXT"},
				},
				Revision: "3",
			}, nil
		},
	}
	h := facade.AppDescribeHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app": 42.0, "lang": "ja"})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
	var data struct {
		App      map[string]any            `json:"app"`
		Fields   map[string]map[string]any `json:"fields"`
		Revision string                    `json:"revision"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.App["app_id"] != "42" || data.App["name"] != "人事" {
		t.Errorf("app=%v", data.App)
	}
	if _, ok := data.Fields["name"]; !ok {
		t.Errorf("fields=%v", data.Fields)
	}
	if data.Revision != "3" {
		t.Errorf("revision=%q", data.Revision)
	}
	// lang が GetFormFields に渡されている
	if m.gotGetFormField == nil || m.gotGetFormField.Lang != "ja" {
		t.Errorf("lang=%v", m.gotGetFormField)
	}
}

// FD-2: app=0（必須・正の整数バリデーション）
func TestAppDescribe_AppZero(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.AppDescribeHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app": 0.0})
	e := parseEnvelope(t, got)
	if e.OK {
		t.Fatalf("ok=true: %s", got)
	}
	if e.Error.Code != "INVALID_PARAMS" {
		t.Errorf("code=%s", e.Error.Code)
	}
}

// FD-3: app 未指定 → INVALID_PARAMS（required）
func TestAppDescribe_AppMissing(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.AppDescribeHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{})
	e := parseEnvelope(t, got)
	if e.OK {
		t.Fatalf("ok=true: %s", got)
	}
	if e.Error.Code != "INVALID_PARAMS" {
		t.Errorf("code=%s", e.Error.Code)
	}
}
