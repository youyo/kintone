// Package ops_test の M08 追加分: --app-ref / --update-key-field-ref / 排他バリデーション。
package ops_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
	"github.com/youyo/kintone/internal/kintoneapi"
)

// resolverStub は stubAPI を継承して ListApps / GetFormFields の挙動を差し替える。
type resolverStub struct {
	stubAPI
	listAppsFn      func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error)
	getFormFieldsFn func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)
}

func (s *resolverStub) ListApps(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
	if s.listAppsFn != nil {
		return s.listAppsFn(ctx, req)
	}
	return &kintoneapi.ListAppsResponse{}, nil
}

func (s *resolverStub) GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
	if s.getFormFieldsFn != nil {
		return s.getFormFieldsFn(ctx, req)
	}
	return &kintoneapi.GetFormFieldsResponse{Properties: map[string]map[string]any{}}, nil
}

func TestOpsRecordCreate_AppRefResolves(t *testing.T) {
	stub := &resolverStub{
		stubAPI: stubAPI{
			insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
				if req.App != 42 {
					t.Errorf("expected App=42, got %d", req.App)
				}
				return &kintoneapi.InsertRecordsResponse{IDs: []string{"1"}, Revisions: []string{"1"}}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) == 1 && req.Codes[0] == "sales" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--app-ref", "sales",
		"--record-json", `{"name":{"value":"x"}}`,
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v out=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("expected ok:true, got %s", out.String())
	}
}

func TestOpsRecordUpdate_UpdateKeyFieldRefResolves(t *testing.T) {
	stub := &resolverStub{
		stubAPI: stubAPI{
			updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
				if req.UpdateKey == nil || req.UpdateKey.Field != "customer_name" {
					t.Errorf("expected UpdateKey.Field=customer_name, got %+v", req.UpdateKey)
				}
				return &kintoneapi.UpdateRecordResponse{Revision: "5"}, nil
			},
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{
				Properties: map[string]map[string]any{
					"customer_name": {"label": "顧客名", "type": "SINGLE_LINE_TEXT"},
				},
			}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "42",
		"--update-key-field-ref", "顧客名",
		"--update-key-value", "山田",
		"--record-json", `{"phone":{"value":"080-0000-0000"}}`,
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v out=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("expected ok:true, got %s", out.String())
	}
}

func TestOpsRecordCreate_AppRefAndAppBothUsage(t *testing.T) {
	withStubAPI(t, &resolverStub{})

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--app", "42", "--app-ref", "sales",
		"--record-json", `{}`,
	}, &out, &errOut)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Fatalf("expected USAGE, got %s", out.String())
	}
}

func TestOpsRecordCreate_AppRefMissingUsage(t *testing.T) {
	withStubAPI(t, &resolverStub{})

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--record-json", `{}`,
	}, &out, &errOut)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Fatalf("expected USAGE, got %s", out.String())
	}
}

func TestOpsRecordUpdate_UpdateKeyFieldRefBothUsage(t *testing.T) {
	withStubAPI(t, &resolverStub{})

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "1",
		"--update-key-field", "name",
		"--update-key-field-ref", "顧客名",
		"--update-key-value", "x",
		"--record-json", `{}`,
	}, &out, &errOut)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Fatalf("expected USAGE, got %s", out.String())
	}
}

func TestOpsRecordDelete_AppRefResolves(t *testing.T) {
	stub := &resolverStub{
		stubAPI: stubAPI{
			deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
				if req.App != 42 {
					t.Errorf("expected App=42, got %d", req.App)
				}
				return nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) == 1 && req.Codes[0] == "sales" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "delete",
		"--app-ref", "sales",
		"--id", "100",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v out=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"deleted":1`) {
		t.Fatalf("expected deleted:1, got %s", out.String())
	}
}

func TestOpsRecordCreate_AppRefAndDryRun(t *testing.T) {
	// dry-run + --app-ref の組み合わせ: 実 API 呼び出しはせず、resolver で解決した App ID で
	// 送信予定 body を出力する。
	insertCalled := false
	stub := &resolverStub{
		stubAPI: stubAPI{
			insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
				insertCalled = true
				return &kintoneapi.InsertRecordsResponse{}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) == 1 && req.Codes[0] == "sales" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--app-ref", "sales",
		"--record-json", `{"name":{"value":"x"}}`,
		"--dry-run",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v out=%s", err, out.String())
	}
	if insertCalled {
		t.Errorf("InsertRecords should NOT be called in dry-run mode")
	}
	if !strings.Contains(out.String(), `"dry_run":true`) {
		t.Errorf("expected dry_run:true in output, got %s", out.String())
	}
	// resolver で解決された App ID=42 が body に含まれる
	if !strings.Contains(out.String(), `"app":42`) {
		t.Errorf("expected app:42 (resolved) in body, got %s", out.String())
	}
}

func TestOpsRecordDelete_AppRefAndDryRun(t *testing.T) {
	deleteCalled := false
	stub := &resolverStub{
		stubAPI: stubAPI{
			deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
				deleteCalled = true
				return nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "delete",
		"--app-ref", "sales",
		"--id", "100",
		"--dry-run",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v out=%s", err, out.String())
	}
	if deleteCalled {
		t.Errorf("DeleteRecords should NOT be called in dry-run mode")
	}
	if !strings.Contains(out.String(), `"app":42`) {
		t.Errorf("expected app:42 (resolved) in body, got %s", out.String())
	}
}

func TestOpsRecordUpdate_AppRefAndUpdateKeyFieldRefDryRun(t *testing.T) {
	updateCalled := false
	stub := &resolverStub{
		stubAPI: stubAPI{
			updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
				updateCalled = true
				return &kintoneapi.UpdateRecordResponse{}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			if req.App != 42 {
				t.Errorf("ResolveField called with App=%d (expected 42)", req.App)
			}
			return &kintoneapi.GetFormFieldsResponse{
				Properties: map[string]map[string]any{
					"customer_name": {"label": "顧客名", "type": "SINGLE_LINE_TEXT"},
				},
			}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app-ref", "sales",
		"--update-key-field-ref", "顧客名",
		"--update-key-value", "山田",
		"--record-json", `{"phone":{"value":"080-0000-0000"}}`,
		"--dry-run",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v out=%s", err, out.String())
	}
	if updateCalled {
		t.Errorf("UpdateRecord should NOT be called in dry-run mode")
	}
	// dry-run でも resolver で App / Field 両方解決される
	if !strings.Contains(out.String(), `"app":42`) {
		t.Errorf("expected app:42 (resolved), got %s", out.String())
	}
	if !strings.Contains(out.String(), `"field":"customer_name"`) {
		t.Errorf("expected field:customer_name (resolved), got %s", out.String())
	}
}

func TestOpsAppDescribe_AppRefResolves(t *testing.T) {
	stub := &resolverStub{
		stubAPI: stubAPI{
			getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
				if req.ID != 42 {
					t.Errorf("expected ID=42, got %d", req.ID)
				}
				return &kintoneapi.GetAppResponse{AppID: "42", Code: "sales", Name: "営業"}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "app", "describe",
		"--app-ref", "sales",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v out=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Fatalf("expected ok:true, got %s", out.String())
	}
}
