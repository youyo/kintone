package api_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/cache"
	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// --- mock upstream ---

type mockAPI struct {
	getAppCalls        int
	getFormFieldsCalls int
	listAppsCalls      int
	getRecordsCalls    int
	getRecordCalls     int
	insertRecordsCalls int
	updateRecordCalls  int
	deleteRecordsCalls int

	getAppFn        func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error)
	getFormFieldsFn func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)
	listAppsFn      func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error)
}

func (m *mockAPI) GetApp(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
	m.getAppCalls++
	if m.getAppFn != nil {
		return m.getAppFn(ctx, req)
	}
	return &kintoneapi.GetAppResponse{AppID: fmt.Sprintf("%d", req.ID), Name: "TestApp"}, nil
}

func (m *mockAPI) GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
	m.getFormFieldsCalls++
	if m.getFormFieldsFn != nil {
		return m.getFormFieldsFn(ctx, req)
	}
	return &kintoneapi.GetFormFieldsResponse{
		Properties: map[string]map[string]any{"field1": {"type": "SINGLE_LINE_TEXT"}},
		Revision:   "1",
	}, nil
}

func (m *mockAPI) ListApps(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
	m.listAppsCalls++
	if m.listAppsFn != nil {
		return m.listAppsFn(ctx, req)
	}
	return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "1", Name: "App1"}}}, nil
}

func (m *mockAPI) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	m.getRecordsCalls++
	return &kintoneapi.GetRecordsResponse{Records: []map[string]any{}}, nil
}

func (m *mockAPI) GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
	m.getRecordCalls++
	return &kintoneapi.GetRecordResponse{Record: map[string]any{}}, nil
}

func (m *mockAPI) InsertRecords(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
	m.insertRecordsCalls++
	return &kintoneapi.InsertRecordsResponse{}, nil
}

func (m *mockAPI) UpdateRecord(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
	m.updateRecordCalls++
	return &kintoneapi.UpdateRecordResponse{}, nil
}

func (m *mockAPI) DeleteRecords(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
	m.deleteRecordsCalls++
	return nil
}

// openCacheStore はテスト用キャッシュストアを TempDir に作る。
func openCacheStore(t *testing.T) cache.Store {
	t.Helper()
	s, err := cache.Open(t.TempDir() + "/cache.db")
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// CA-0: 全 cached メソッドのキーが必ず v1: で始まること
// （テスト観察: Put が呼ばれた後 key を store から Stats で確認するのではなく
//  2 回目呼出しで upstream が呼ばれないことで "キャッシュが書かれた" を証明）

// CA-1: GetApp ミス → upstream 呼ばれ cache に Put
func TestCachingAPI_GetApp_Miss(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")

	resp, err := api.GetApp(context.Background(), kintoneapi.GetAppRequest{ID: 42})
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if resp == nil {
		t.Fatal("response is nil")
	}
	if mock.getAppCalls != 1 {
		t.Errorf("upstream calls: want 1, got %d", mock.getAppCalls)
	}
}

// CA-2: GetApp ヒット → upstream が呼ばれない
func TestCachingAPI_GetApp_Hit(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	ctx := context.Background()
	req := kintoneapi.GetAppRequest{ID: 42}

	// 1 回目: miss → upstream
	if _, err := api.GetApp(ctx, req); err != nil {
		t.Fatalf("first GetApp: %v", err)
	}
	// 2 回目: hit → upstream 呼ばれない
	if _, err := api.GetApp(ctx, req); err != nil {
		t.Fatalf("second GetApp: %v", err)
	}
	if mock.getAppCalls != 1 {
		t.Errorf("upstream calls: want 1 (hit), got %d", mock.getAppCalls)
	}
}

// CA-3: GetApp 期限切れ → upstream が再呼出しされる
func TestCachingAPI_GetApp_Expired(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	ctx := context.Background()

	// TTL=1ns で期限切れエントリを直接 Put
	key := "v1:app:example.cybozu.com:42"
	if err := store.Put(ctx, key, []byte(`{"appId":"42","name":"cached"}`), 1*time.Nanosecond); err != nil {
		t.Fatalf("Put expired: %v", err)
	}
	// 1ns wait
	time.Sleep(2 * time.Nanosecond)

	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	if _, err := api.GetApp(ctx, kintoneapi.GetAppRequest{ID: 42}); err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if mock.getAppCalls != 1 {
		t.Errorf("expected upstream re-call on expiry, got %d calls", mock.getAppCalls)
	}
}

// CA-4: 異なる domain は別エントリ
func TestCachingAPI_GetApp_DomainIsolation(t *testing.T) {
	mockA := &mockAPI{}
	mockB := &mockAPI{}
	store := openCacheStore(t)

	apiA := serviceapi.NewCachingAPI(mockA, store, "domainA.cybozu.com")
	apiB := serviceapi.NewCachingAPI(mockB, store, "domainB.cybozu.com")
	ctx := context.Background()
	req := kintoneapi.GetAppRequest{ID: 1}

	if _, err := apiA.GetApp(ctx, req); err != nil {
		t.Fatalf("apiA GetApp: %v", err)
	}
	// domainB のキャッシュは domainA とは別 → upstream が呼ばれる
	if _, err := apiB.GetApp(ctx, req); err != nil {
		t.Fatalf("apiB GetApp: %v", err)
	}
	if mockB.getAppCalls != 1 {
		t.Errorf("domainB should call upstream, got %d", mockB.getAppCalls)
	}
}

// CA-5: GetFormFields ヒット/ミス
func TestCachingAPI_GetFormFields_HitMiss(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	ctx := context.Background()
	req := kintoneapi.GetFormFieldsRequest{App: 10, Lang: "ja"}

	// miss
	if _, err := api.GetFormFields(ctx, req); err != nil {
		t.Fatalf("first GetFormFields: %v", err)
	}
	// hit
	if _, err := api.GetFormFields(ctx, req); err != nil {
		t.Fatalf("second GetFormFields: %v", err)
	}
	if mock.getFormFieldsCalls != 1 {
		t.Errorf("upstream calls: want 1, got %d", mock.getFormFieldsCalls)
	}
}

// CA-6: ListApps は offset/limit 違いで別キャッシュエントリ
func TestCachingAPI_ListApps_DifferentArgs(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	ctx := context.Background()

	if _, err := api.ListApps(ctx, kintoneapi.ListAppsRequest{Limit: 10, Offset: 0}); err != nil {
		t.Fatalf("ListApps first: %v", err)
	}
	if _, err := api.ListApps(ctx, kintoneapi.ListAppsRequest{Limit: 10, Offset: 10}); err != nil {
		t.Fatalf("ListApps second: %v", err)
	}
	// 別キャッシュキー → upstream 2 回呼ばれる
	if mock.listAppsCalls != 2 {
		t.Errorf("upstream calls: want 2 (different args), got %d", mock.listAppsCalls)
	}
}

// CA-6b: listAppsCacheKey 正規化 — IDs=[2,1] と IDs=[1,2] は同一キー
func TestCachingAPI_ListApps_SliceNormalization(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	ctx := context.Background()

	if _, err := api.ListApps(ctx, kintoneapi.ListAppsRequest{IDs: []int64{2, 1}}); err != nil {
		t.Fatalf("ListApps [2,1]: %v", err)
	}
	// ソート後同一キー → hit
	if _, err := api.ListApps(ctx, kintoneapi.ListAppsRequest{IDs: []int64{1, 2}}); err != nil {
		t.Fatalf("ListApps [1,2]: %v", err)
	}
	if mock.listAppsCalls != 1 {
		t.Errorf("upstream calls: want 1 (normalized), got %d", mock.listAppsCalls)
	}
}

// CA-6c: nil と 空配列は同一キー
func TestCachingAPI_ListApps_NilVsEmpty(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	ctx := context.Background()

	if _, err := api.ListApps(ctx, kintoneapi.ListAppsRequest{IDs: nil}); err != nil {
		t.Fatalf("ListApps nil: %v", err)
	}
	// 空スライス → same key → hit
	if _, err := api.ListApps(ctx, kintoneapi.ListAppsRequest{IDs: []int64{}}); err != nil {
		t.Fatalf("ListApps empty: %v", err)
	}
	if mock.listAppsCalls != 1 {
		t.Errorf("upstream calls: want 1 (nil vs empty), got %d", mock.listAppsCalls)
	}
}

// CA-7: GetRecords はキャッシュを介さず必ず upstream に行く
func TestCachingAPI_GetRecords_Passthrough(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	ctx := context.Background()

	for range 3 {
		if _, err := api.GetRecords(ctx, kintoneapi.GetRecordsRequest{App: 1}); err != nil {
			t.Fatalf("GetRecords: %v", err)
		}
	}
	if mock.getRecordsCalls != 3 {
		t.Errorf("upstream calls: want 3 (no cache), got %d", mock.getRecordsCalls)
	}
}

// CA-8: InsertRecords はキャッシュを介さず upstream に行く
func TestCachingAPI_InsertRecords_Passthrough(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	ctx := context.Background()

	for range 2 {
		if _, err := api.InsertRecords(ctx, kintoneapi.InsertRecordsRequest{App: 1, Records: []map[string]any{{"f": map[string]any{"value": "v"}}}}); err != nil {
			t.Fatalf("InsertRecords: %v", err)
		}
	}
	if mock.insertRecordsCalls != 2 {
		t.Errorf("upstream calls: want 2, got %d", mock.insertRecordsCalls)
	}
}

// CA-9: upstream エラー時はエラーを伝播し cache に Put しない
func TestCachingAPI_GetApp_UpstreamError(t *testing.T) {
	upstreamErr := errors.New("upstream error")
	mock := &mockAPI{
		getAppFn: func(_ context.Context, _ kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return nil, upstreamErr
		},
	}
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	ctx := context.Background()

	_, err := api.GetApp(ctx, kintoneapi.GetAppRequest{ID: 99})
	if err == nil {
		t.Fatal("expected error")
	}
	// 2 回目: cache に保存されていないため再び upstream
	_, err2 := api.GetApp(ctx, kintoneapi.GetAppRequest{ID: 99})
	if err2 == nil {
		t.Fatal("expected error on second call")
	}
	if mock.getAppCalls != 2 {
		t.Errorf("upstream calls: want 2 (no cache on error), got %d", mock.getAppCalls)
	}
}

// CA-10: cache.Get が IO エラー（miss 以外）→ upstream にフォールバック（fail-open）
func TestCachingAPI_GetApp_CacheGetError_FailOpen(t *testing.T) {
	mock := &mockAPI{}
	// 閉じた store を使うと IO エラーになる
	store := openCacheStore(t)
	api := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")
	// store を Close してから呼ぶ
	_ = store.Close()

	// fail-open: エラーにならず upstream を呼ぶ
	_, err := api.GetApp(context.Background(), kintoneapi.GetAppRequest{ID: 1})
	// upstream 自体は動くのでエラーにならない
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.getAppCalls != 1 {
		t.Errorf("upstream calls: want 1 (fail-open), got %d", mock.getAppCalls)
	}
}

// CA-11: cache.Put エラーでも upstream レスポンスは返す（fail-silent on Put）
func TestCachingAPI_GetApp_CachePutError_FailSilent(t *testing.T) {
	mock := &mockAPI{}
	store := openCacheStore(t)
	ctx := context.Background()
	// store を閉じて Put が失敗する状態にする
	_ = store.Close()
	// 閉じた store を持つ api
	api2 := serviceapi.NewCachingAPI(mock, store, "example.cybozu.com")

	resp, err := api2.GetApp(ctx, kintoneapi.GetAppRequest{ID: 5})
	// Put が失敗しても upstream レスポンスを返す
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("response should not be nil even if Put fails")
	}
}

// CA-12: nil store → upstream をそのまま返す
func TestCachingAPI_NilStore(t *testing.T) {
	mock := &mockAPI{}
	api := serviceapi.NewCachingAPI(mock, nil, "example.cybozu.com")

	// API 型が返るため、呼び出し側は区別不要
	if _, err := api.GetApp(context.Background(), kintoneapi.GetAppRequest{ID: 1}); err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if mock.getAppCalls != 1 {
		t.Errorf("upstream calls: want 1, got %d", mock.getAppCalls)
	}
}
