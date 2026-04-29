// Package operations_test の M08 追加分: AppRef / UpdateKeyFieldRef ハイブリッド解決を検証する。
package operations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/resolver"
	"github.com/youyo/kintone/internal/service/operations"
)

// resolverStubAPI は stubAPI に listAppsFn を載せた拡張版。
//
// resolver は service/api.API.ListApps / GetFormFields を使うため、
// 既存 stubAPI に ListApps / GetFormFields のレスポンスを差し替える hook を持たせる。
type resolverStubAPI struct {
	stubAPI
	listAppsFn func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error)
}

func (s *resolverStubAPI) ListApps(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
	if s.listAppsFn != nil {
		return s.listAppsFn(ctx, req)
	}
	return &kintoneapi.ListAppsResponse{}, nil
}

// OP-1: 既存 App int64 経路は resolver=nil でも動く（後方互換）
func TestRecordsQuery_BackwardCompat_AppOnly(t *testing.T) {
	s := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return &kintoneapi.GetRecordsResponse{Records: []map[string]any{}}, nil
		},
	}
	out, err := operations.RecordsQuery(context.Background(), s, nil, operations.RecordsQueryInput{App: 42})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out == nil {
		t.Fatal("nil output")
	}
	if s.gotRecordsReq.App != 42 {
		t.Errorf("App=%d want 42", s.gotRecordsReq.App)
	}
}

// OP-2: AppRef 経路で resolver が呼ばれて App ID に変換される
func TestRecordsQuery_AppRefResolves(t *testing.T) {
	s := &resolverStubAPI{
		stubAPI: stubAPI{
			getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
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
	r := resolver.New(s)
	_, err := operations.RecordsQuery(context.Background(), s, r, operations.RecordsQueryInput{AppRef: "sales"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if s.gotRecordsReq.App != 42 {
		t.Errorf("App=%d want 42", s.gotRecordsReq.App)
	}
}

// OP-3: App と AppRef 両方指定 → ErrConflictingAppRef
func TestRecordsQuery_ConflictingAppRef(t *testing.T) {
	_, err := operations.RecordsQuery(context.Background(), &stubAPI{}, nil, operations.RecordsQueryInput{App: 42, AppRef: "sales"})
	if !errors.Is(err, operations.ErrConflictingAppRef) {
		t.Fatalf("expected ErrConflictingAppRef, got %v", err)
	}
}

// OP-4: どちらも未指定 → ErrInvalidApp
func TestRecordsQuery_MissingBoth(t *testing.T) {
	_, err := operations.RecordsQuery(context.Background(), &stubAPI{}, nil, operations.RecordsQueryInput{})
	if !errors.Is(err, operations.ErrInvalidApp) {
		t.Fatalf("expected ErrInvalidApp, got %v", err)
	}
}

// OP-5: resolver=nil + AppRef 指定 → ErrResolverUnavailable
func TestRecordsQuery_ResolverUnavailable(t *testing.T) {
	_, err := operations.RecordsQuery(context.Background(), &stubAPI{}, nil, operations.RecordsQueryInput{AppRef: "sales"})
	if !errors.Is(err, operations.ErrResolverUnavailable) {
		t.Fatalf("expected ErrResolverUnavailable, got %v", err)
	}
}

// OP-6: RecordUpdate UpdateKeyFieldRef → ResolveField で field code に解決
func TestRecordUpdate_UpdateKeyFieldRefResolves(t *testing.T) {
	s := &resolverStubAPI{
		stubAPI: stubAPI{
			getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
				return &kintoneapi.GetFormFieldsResponse{Properties: map[string]map[string]any{
					"customer_name": {"label": "顧客名", "type": "SINGLE_LINE_TEXT"},
				}}, nil
			},
			updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
				if req.UpdateKey == nil || req.UpdateKey.Field != "customer_name" {
					t.Errorf("expected UpdateKey.Field=customer_name, got %+v", req.UpdateKey)
				}
				return &kintoneapi.UpdateRecordResponse{Revision: "5"}, nil
			},
		},
	}
	r := resolver.New(s)
	out, err := operations.RecordUpdate(context.Background(), s, r, operations.RecordUpdateInput{
		App:               42,
		UpdateKeyFieldRef: "顧客名",
		UpdateKeyValue:    "山田",
		Record:            map[string]any{"x": map[string]any{"value": "1"}},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.Revision != 5 {
		t.Errorf("Revision=%d want 5", out.Revision)
	}
}

// OP-7: UpdateKeyField と UpdateKeyFieldRef 両方指定 → ErrConflictingUpdateKeyFieldRef
func TestRecordUpdate_ConflictingUpdateKeyFieldRef(t *testing.T) {
	_, err := operations.RecordUpdate(context.Background(), &stubAPI{}, nil, operations.RecordUpdateInput{
		App:               42,
		UpdateKeyField:    "name",
		UpdateKeyFieldRef: "顧客名",
		UpdateKeyValue:    "山田",
		Record:            map[string]any{"x": map[string]any{"value": "1"}},
	})
	if !errors.Is(err, operations.ErrConflictingUpdateKeyFieldRef) {
		t.Fatalf("expected ErrConflictingUpdateKeyFieldRef, got %v", err)
	}
}

// AppDescribe / RecordCreate / RecordDelete も AppRef 経路をスモークテスト。
func TestAppDescribe_AppRefResolves(t *testing.T) {
	s := &resolverStubAPI{
		stubAPI: stubAPI{
			getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
				return &kintoneapi.GetAppResponse{AppID: "42", Code: "sales", Name: "営業"}, nil
			},
			getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
				return &kintoneapi.GetFormFieldsResponse{Properties: map[string]map[string]any{}, Revision: "1"}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
	}
	r := resolver.New(s)
	_, err := operations.AppDescribe(context.Background(), s, r, operations.AppDescribeInput{AppRef: "sales"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if s.gotAppReq.ID != 42 {
		t.Errorf("ID=%d want 42", s.gotAppReq.ID)
	}
}

func TestRecordCreate_AppRefResolves(t *testing.T) {
	s := &resolverStubAPI{
		stubAPI: stubAPI{
			insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
				if req.App != 42 {
					t.Errorf("App=%d want 42", req.App)
				}
				return &kintoneapi.InsertRecordsResponse{IDs: []string{"1"}, Revisions: []string{"1"}}, nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
	}
	r := resolver.New(s)
	_, err := operations.RecordCreate(context.Background(), s, r, operations.RecordCreateInput{
		AppRef: "sales",
		Record: map[string]any{"x": map[string]any{"value": "1"}},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestRecordDelete_AppRefResolves(t *testing.T) {
	s := &resolverStubAPI{
		stubAPI: stubAPI{
			deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
				if req.App != 42 {
					t.Errorf("App=%d want 42", req.App)
				}
				return nil
			},
		},
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "42", Code: "sales", Name: "営業"}}}, nil
		},
	}
	r := resolver.New(s)
	_, err := operations.RecordDelete(context.Background(), s, r, operations.RecordDeleteInput{
		AppRef: "sales",
		IDs:    []int64{100},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
