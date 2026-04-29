// Package facade_test の M08 追加分: app_ref / update_key_field_ref のハンドラ経路。
package facade_test

import (
	"context"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/mcp/facade"
)

func TestRecordsQuery_AppRefResolves(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			if req.App != 42 {
				t.Errorf("expected App=42, got %d", req.App)
			}
			return &kintoneapi.GetRecordsResponse{Records: []map[string]any{}}, nil
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) == 1 && req.Codes[0] == "sales" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	h := facade.RecordsQueryHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app_ref": "sales"})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Errorf("ok=false: %s", got)
	}
}

func TestRecordsQuery_AppRefBoth(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordsQueryHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app": 42.0, "app_ref": "sales"})
	e := parseEnvelope(t, got)
	if e.OK {
		t.Errorf("expected ok=false: %s", got)
	}
	if !strings.Contains(got, "INVALID_PARAMS") {
		t.Errorf("expected INVALID_PARAMS, got %s", got)
	}
}

func TestRecordsQuery_AppRefAmbiguous(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) > 0 {
				return &kintoneapi.ListAppsResponse{}, nil
			}
			if req.Name == "営業" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{
					{AppID: "42", Code: "sales", Name: "営業 A"},
					{AppID: "55", Code: "sales2", Name: "営業 B"},
				}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	h := facade.RecordsQueryHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app_ref": "営業"})
	e := parseEnvelope(t, got)
	if e.OK {
		t.Errorf("expected ok=false: %s", got)
	}
	if !strings.Contains(got, "RESOLVER_APP_AMBIGUOUS") {
		t.Errorf("expected RESOLVER_APP_AMBIGUOUS, got %s", got)
	}
	if !strings.Contains(got, "candidates") {
		t.Errorf("expected candidates, got %s", got)
	}
}

func TestAppDescribe_AppRefResolves(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			if req.ID != 42 {
				t.Errorf("expected ID=42, got %d", req.ID)
			}
			return &kintoneapi.GetAppResponse{AppID: "42", Code: "sales", Name: "営業"}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{Properties: map[string]map[string]any{}, Revision: "1"}, nil
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
	}
	h := facade.AppDescribeHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app_ref": "sales"})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Errorf("ok=false: %s", got)
	}
}

func TestRecordCreate_AppRefResolves(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			if req.App != 42 {
				t.Errorf("expected App=42, got %d", req.App)
			}
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"1"}, Revisions: []string{"1"}}, nil
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
	}
	h := facade.RecordCreateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app_ref": "sales",
		"record":  map[string]any{"x": map[string]any{"value": "1"}},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Errorf("ok=false: %s", got)
	}
}

func TestRecordUpdate_UpdateKeyFieldRefResolves(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{Properties: map[string]map[string]any{
				"customer_name": {"label": "顧客名", "type": "SINGLE_LINE_TEXT"},
			}}, nil
		},
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			if req.UpdateKey == nil || req.UpdateKey.Field != "customer_name" {
				t.Errorf("expected Field=customer_name, got %+v", req.UpdateKey)
			}
			return &kintoneapi.UpdateRecordResponse{Revision: "5"}, nil
		},
	}
	h := facade.RecordUpdateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":                  42.0,
		"update_key_field_ref": "顧客名",
		"update_key_value":     "山田",
		"record":               map[string]any{"phone": map[string]any{"value": "x"}},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Errorf("ok=false: %s", got)
	}
}

func TestRecordDelete_AppRefResolves(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
			if req.App != 42 {
				t.Errorf("expected App=42, got %d", req.App)
			}
			return nil
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
	}
	h := facade.RecordDeleteHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app_ref": "sales",
		"ids":     []any{100.0},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Errorf("ok=false: %s", got)
	}
}
