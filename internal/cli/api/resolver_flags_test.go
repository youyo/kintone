// Package api_test の M08 追加分: --app-ref / --app と --app-ref 排他バリデーションを検証する。
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
	cliapi "github.com/youyo/kintone/internal/cli/api"
	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// listAppsStub は ListApps のスタブのみ追加した stubAPI。
type listAppsStub struct {
	stubAPI
	listAppsFn func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error)
}

func (s *listAppsStub) ListApps(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
	if s.listAppsFn != nil {
		return s.listAppsFn(ctx, req)
	}
	return &kintoneapi.ListAppsResponse{}, nil
}

// runRoot は CLI ルートを ExecuteWith で実行し、stdout（JSON）を返す。
//
// ExecuteWith は失敗時に MapToOutputError 経由で {"ok":false,"error":{...}} を out に書く。
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith(args, &out, &errOut)
	return out.String(), err
}

// installStub は cliapi.NewAPIBuilder を stub に差し替える。
func installStub(t *testing.T, stub serviceapi.API) {
	t.Helper()
	prev := cliapi.NewAPIBuilder
	cliapi.NewAPIBuilder = func(in cliapi.LoaderInput) (serviceapi.API, error) { return stub, nil }
	t.Cleanup(func() { cliapi.NewAPIBuilder = prev })
}

func TestRecordsGet_AppRefMissingAndAppRefBoth_USAGE(t *testing.T) {
	stub := &listAppsStub{}
	installStub(t, stub)

	t.Run("どちらも未指定は USAGE", func(t *testing.T) {
		out, err := runRoot(t, "api", "records", "get")
		if err == nil {
			t.Fatalf("expected error, got out=%q", out)
		}
		if !strings.Contains(out, `"USAGE"`) {
			t.Fatalf("expected USAGE error in output, got %q", out)
		}
	})
	t.Run("両方指定は USAGE", func(t *testing.T) {
		out, err := runRoot(t, "api", "records", "get", "--app", "42", "--app-ref", "sales")
		if err == nil {
			t.Fatalf("expected error, got out=%q", out)
		}
		if !strings.Contains(out, `"USAGE"`) {
			t.Fatalf("expected USAGE error in output, got %q", out)
		}
	})
}

func TestRecordsGet_AppRefResolves(t *testing.T) {
	stub := &listAppsStub{
		stubAPI: stubAPI{
			getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
				if req.App != 42 {
					t.Errorf("expected App=42 after resolve, got %d", req.App)
				}
				return &kintoneapi.GetRecordsResponse{Records: []map[string]any{}}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) == 1 && req.Codes[0] == "sales" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	installStub(t, stub)

	out, err := runRoot(t, "api", "records", "get", "--app-ref", "sales")
	if err != nil {
		t.Fatalf("unexpected error: %v out=%q", err, out)
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if jerr := json.Unmarshal([]byte(out), &env); jerr != nil {
		t.Fatalf("invalid JSON: %v out=%q", jerr, out)
	}
	if !env.OK {
		t.Fatalf("expected ok=true, got out=%q", out)
	}
}

func TestAppDescribe_AppRefResolvesAmbiguous(t *testing.T) {
	stub := &listAppsStub{
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
	installStub(t, stub)

	out, err := runRoot(t, "api", "app", "describe", "--app-ref", "営業")
	if err == nil {
		t.Fatalf("expected error, got out=%q", out)
	}
	if !strings.Contains(out, "RESOLVER_APP_AMBIGUOUS") {
		t.Fatalf("expected RESOLVER_APP_AMBIGUOUS, got %q", out)
	}
	if !strings.Contains(out, "candidates") {
		t.Fatalf("expected candidates in details, got %q", out)
	}
}

func TestAppGet_AppRefNotFound(t *testing.T) {
	stub := &listAppsStub{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	installStub(t, stub)

	out, err := runRoot(t, "api", "app", "get", "--app-ref", "ないアプリ")
	if err == nil {
		t.Fatalf("expected error, got out=%q", out)
	}
	if !strings.Contains(out, "RESOLVER_APP_NOT_FOUND") {
		t.Fatalf("expected RESOLVER_APP_NOT_FOUND, got %q", out)
	}
}

func TestAppFields_AppDirectInt(t *testing.T) {
	// 後方互換: --app 直指定が引き続き動く
	stub := &listAppsStub{
		stubAPI: stubAPI{
			getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
				if req.App != 42 {
					t.Errorf("expected App=42, got %d", req.App)
				}
				return &kintoneapi.GetFormFieldsResponse{Properties: map[string]map[string]any{}, Revision: "1"}, nil
			},
		},
	}
	installStub(t, stub)

	out, err := runRoot(t, "api", "app", "fields", "--app", "42")
	if err != nil {
		t.Fatalf("unexpected: %v out=%q", err, out)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("expected ok:true, got %q", out)
	}
}

func TestAppFields_AppRefResolves(t *testing.T) {
	stub := &listAppsStub{
		stubAPI: stubAPI{
			getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
				if req.App != 42 {
					t.Errorf("expected App=42, got %d", req.App)
				}
				return &kintoneapi.GetFormFieldsResponse{Properties: map[string]map[string]any{}, Revision: "1"}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) == 1 && req.Codes[0] == "sales" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	installStub(t, stub)

	out, err := runRoot(t, "api", "app", "fields", "--app-ref", "sales")
	if err != nil {
		t.Fatalf("unexpected: %v out=%q", err, out)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("expected ok:true, got %s", out)
	}
}

func TestAppGet_AppRefResolves(t *testing.T) {
	stub := &listAppsStub{
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
	installStub(t, stub)

	out, err := runRoot(t, "api", "app", "get", "--app-ref", "sales")
	if err != nil {
		t.Fatalf("unexpected: %v out=%q", err, out)
	}
	if !strings.Contains(out, `"app_id":"42"`) {
		t.Fatalf("expected app_id:42, got %s", out)
	}
}

func TestRecordGet_AppRefResolves(t *testing.T) {
	stub := &listAppsStub{
		stubAPI: stubAPI{
			getRecordFn: func(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
				if req.App != 42 || req.ID != 100 {
					t.Errorf("expected App=42 ID=100, got %d / %d", req.App, req.ID)
				}
				return &kintoneapi.GetRecordResponse{Record: map[string]any{}}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) == 1 && req.Codes[0] == "sales" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	installStub(t, stub)

	out, err := runRoot(t, "api", "record", "get", "--app-ref", "sales", "--id", "100")
	if err != nil {
		t.Fatalf("unexpected: %v out=%q", err, out)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("expected ok:true, got %q", out)
	}
}
