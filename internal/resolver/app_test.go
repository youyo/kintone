package resolver

import (
	"context"
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
)

// mockAPI は service/api.API の最小実装。Resolver で使う ListApps / GetFormFields のみ振る舞う。
type mockAPI struct {
	listAppsFn      func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error)
	getFormFieldsFn func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)
	listAppsCalls   []kintoneapi.ListAppsRequest
}

func (m *mockAPI) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAPI) GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAPI) GetApp(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAPI) GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
	if m.getFormFieldsFn == nil {
		return nil, errors.New("getFormFieldsFn not set")
	}
	return m.getFormFieldsFn(ctx, req)
}
func (m *mockAPI) ListApps(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
	m.listAppsCalls = append(m.listAppsCalls, req)
	if m.listAppsFn == nil {
		return nil, errors.New("listAppsFn not set")
	}
	return m.listAppsFn(ctx, req)
}
func (m *mockAPI) InsertRecords(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAPI) UpdateRecord(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *mockAPI) DeleteRecords(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
	return errors.New("not implemented")
}

func TestResolveApp_EmptyRef_R1(t *testing.T) {
	r := New(&mockAPI{})
	_, err := r.ResolveApp(context.Background(), "")
	if !errors.Is(err, ErrEmptyRef) {
		t.Fatalf("expected ErrEmptyRef, got %v", err)
	}
}

func TestResolveApp_DirectID_R2(t *testing.T) {
	m := &mockAPI{} // listAppsFn を設定しない = 呼ばれたら error
	r := New(m)
	id, err := r.ResolveApp(context.Background(), "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected 42, got %d", id)
	}
	if len(m.listAppsCalls) != 0 {
		t.Fatalf("ListApps should not be called for numeric ID, calls: %d", len(m.listAppsCalls))
	}
}

func TestResolveApp_DirectID_NegativeFallsBack_R3(t *testing.T) {
	// "-1" は数値変換成功するが id <= 0 なので code 経路へ fallback。
	// code 経路で 0 件、name 経路でも 0 件 → NotFoundError。
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: nil}, nil
		},
	}
	r := New(m)
	_, err := r.ResolveApp(context.Background(), "-1")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	// step 2 (codes) と step 3 (name) で 2 回呼ばれる
	if len(m.listAppsCalls) != 2 {
		t.Fatalf("expected 2 ListApps calls (code + name), got %d", len(m.listAppsCalls))
	}
}

func TestResolveApp_DirectID_ZeroFallsBack_R3b(t *testing.T) {
	// "0" も id<=0 なので fallback
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: nil}, nil
		},
	}
	r := New(m)
	_, err := r.ResolveApp(context.Background(), "0")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
}

func TestResolveApp_CodeExact_R4(t *testing.T) {
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) == 1 && req.Codes[0] == "sales" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{
					{AppID: "42", Code: "sales", Name: "営業案件"},
				}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	r := New(m)
	id, err := r.ResolveApp(context.Background(), "sales")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected 42, got %d", id)
	}
	if len(m.listAppsCalls) != 1 {
		t.Fatalf("expected 1 ListApps call (code only), got %d", len(m.listAppsCalls))
	}
}

func TestResolveApp_CodeMissNameExact_R5(t *testing.T) {
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) > 0 {
				return &kintoneapi.ListAppsResponse{Apps: nil}, nil
			}
			if req.Name == "営業案件" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{
					{AppID: "42", Code: "sales", Name: "営業案件"},
				}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	r := New(m)
	id, err := r.ResolveApp(context.Background(), "営業案件")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected 42, got %d", id)
	}
}

func TestResolveApp_CodeAmbiguous_R6(t *testing.T) {
	// codes は完全一致 API なので普通は 1 件だが、
	// 想定外の REST 挙動（同一 code 重複等）を防御的にテスト。
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) > 0 {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{
					{AppID: "42", Code: "dup", Name: "A"},
					{AppID: "55", Code: "dup", Name: "B"},
				}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	r := New(m)
	_, err := r.ResolveApp(context.Background(), "dup")
	var ae *AmbiguousError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AmbiguousError, got %v", err)
	}
	if ae.Kind != "app" || len(ae.Candidates) != 2 {
		t.Fatalf("expected 2 app candidates, got kind=%q candidates=%d", ae.Kind, len(ae.Candidates))
	}
}

func TestResolveApp_NameExactAmbiguous_R7(t *testing.T) {
	// name 部分一致で複数返り、すべて Name == ref → 完全一致 ambiguous
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) > 0 {
				return &kintoneapi.ListAppsResponse{Apps: nil}, nil
			}
			if req.Name == "営業" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{
					{AppID: "42", Code: "a1", Name: "営業"},
					{AppID: "55", Code: "a2", Name: "営業"},
				}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	r := New(m)
	_, err := r.ResolveApp(context.Background(), "営業")
	var ae *AmbiguousError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AmbiguousError, got %v", err)
	}
	if !errors.Is(err, ErrAppAmbiguous) {
		t.Fatalf("errors.Is should match ErrAppAmbiguous")
	}
	if len(ae.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(ae.Candidates))
	}
}

func TestResolveApp_NamePartialSingle_R8(t *testing.T) {
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) > 0 {
				return &kintoneapi.ListAppsResponse{Apps: nil}, nil
			}
			if req.Name == "営業" {
				// 完全一致 0 件、部分一致 1 件
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{
					{AppID: "42", Code: "sales", Name: "営業案件"},
				}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	r := New(m)
	id, err := r.ResolveApp(context.Background(), "営業")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected 42, got %d", id)
	}
}

func TestResolveApp_NamePartialAmbiguous_R9(t *testing.T) {
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) > 0 {
				return &kintoneapi.ListAppsResponse{Apps: nil}, nil
			}
			if req.Name == "営業" {
				return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{
					{AppID: "42", Code: "sales", Name: "営業案件"},
					{AppID: "55", Code: "pipe", Name: "営業パイプライン"},
					{AppID: "60", Code: "sales24", Name: "営業 2024"},
				}}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	r := New(m)
	_, err := r.ResolveApp(context.Background(), "営業")
	var ae *AmbiguousError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AmbiguousError, got %v", err)
	}
	if len(ae.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(ae.Candidates))
	}
}

func TestResolveApp_NotFound_R10(t *testing.T) {
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: nil}, nil
		},
	}
	r := New(m)
	_, err := r.ResolveApp(context.Background(), "ないアプリ")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("errors.Is should match ErrAppNotFound")
	}
	if len(nfe.Candidates) != 0 {
		t.Fatalf("expected 0 candidates, got %d", len(nfe.Candidates))
	}
}

func TestResolveApp_NamePagination_R11(t *testing.T) {
	calls := 0
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) > 0 {
				return &kintoneapi.ListAppsResponse{Apps: nil}, nil
			}
			calls++
			// 100 → 100 → 50
			switch calls {
			case 1:
				apps := make([]kintoneapi.AppListEntry, 100)
				for i := range apps {
					apps[i] = kintoneapi.AppListEntry{AppID: "x", Code: "x", Name: "他"}
				}
				return &kintoneapi.ListAppsResponse{Apps: apps}, nil
			case 2:
				apps := make([]kintoneapi.AppListEntry, 100)
				for i := range apps {
					apps[i] = kintoneapi.AppListEntry{AppID: "x", Code: "x", Name: "他"}
				}
				// 真ん中に 1 件 hit を入れる
				apps[50] = kintoneapi.AppListEntry{AppID: "777", Code: "hit", Name: "営業案件"}
				return &kintoneapi.ListAppsResponse{Apps: apps}, nil
			case 3:
				apps := make([]kintoneapi.AppListEntry, 50)
				for i := range apps {
					apps[i] = kintoneapi.AppListEntry{AppID: "x", Code: "x", Name: "他"}
				}
				return &kintoneapi.ListAppsResponse{Apps: apps}, nil
			}
			return &kintoneapi.ListAppsResponse{}, nil
		},
	}
	r := New(m)
	id, err := r.ResolveApp(context.Background(), "営業案件")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if id != 777 {
		t.Fatalf("expected 777, got %d", id)
	}
	// codes 1 + name 3 = 4 calls
	if len(m.listAppsCalls) != 4 {
		t.Fatalf("expected 4 ListApps calls, got %d", len(m.listAppsCalls))
	}
}

func TestResolveApp_NamePaginationOverflow_R12(t *testing.T) {
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			if len(req.Codes) > 0 {
				return &kintoneapi.ListAppsResponse{Apps: nil}, nil
			}
			// 常に 100 件返す → ページング上限到達
			apps := make([]kintoneapi.AppListEntry, 100)
			for i := range apps {
				apps[i] = kintoneapi.AppListEntry{AppID: "x", Code: "x", Name: "他"}
			}
			return &kintoneapi.ListAppsResponse{Apps: apps}, nil
		},
	}
	r := New(m)
	_, err := r.ResolveApp(context.Background(), "営業")
	if !errors.Is(err, ErrAppListTooLarge) {
		t.Fatalf("expected ErrAppListTooLarge, got %v", err)
	}
}

func TestResolveApp_APIErrorPropagated_R19(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 500, Message: "server error"}
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return nil, apiErr
		},
	}
	r := New(m)
	_, err := r.ResolveApp(context.Background(), "営業")
	var got *kintoneapi.APIError
	if !errors.As(err, &got) {
		t.Fatalf("expected APIError to be propagated, got %v", err)
	}
}
