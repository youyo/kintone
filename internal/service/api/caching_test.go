package api_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
	"github.com/youyo/kintone/internal/store"
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

// --- fake CacheStore ---

// memCacheEntry は memCache のエントリ（value + 期限）。
type memCacheEntry struct {
	value     []byte
	expiresAt time.Time // zero なら無期限
}

// memCache は in-memory CacheStore 実装。test 専用。
//
// store.CacheStore interface を満たす。closed 時は IO エラーを返し、
// CA-10/CA-11 fail-open 経路を検証可能にする。
type memCache struct {
	mu     sync.Mutex
	data   map[string]memCacheEntry
	closed bool
	getErr error
	putErr error
	now    func() time.Time
}

func newMemCache() *memCache {
	return &memCache{data: map[string]memCacheEntry{}, now: time.Now}
}

func (m *memCache) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("memcache: closed")
	}
	if m.getErr != nil {
		return nil, m.getErr
	}
	e, ok := m.data[key]
	if !ok {
		return nil, store.ErrCacheMiss
	}
	if !e.expiresAt.IsZero() && !m.now().Before(e.expiresAt) {
		return nil, store.ErrCacheMiss
	}
	cp := make([]byte, len(e.value))
	copy(cp, e.value)
	return cp, nil
}

func (m *memCache) Put(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("memcache: closed")
	}
	if m.putErr != nil {
		return m.putErr
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	var exp time.Time
	if ttl > 0 {
		exp = m.now().Add(ttl)
	}
	m.data[key] = memCacheEntry{value: cp, expiresAt: exp}
	return nil
}

func (m *memCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *memCache) DeleteByPrefix(_ context.Context, prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			delete(m.data, k)
			n++
		}
	}
	return n, nil
}

func (m *memCache) Stats(_ context.Context) (store.Stats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return store.Stats{
		Backend:    "memory-test",
		Location:   "memory://",
		Reachable:  !m.closed,
		EntryCount: int64(len(m.data)),
	}, nil
}

func (m *memCache) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// providerOf は memCache を返す CacheProvider を構築する。
func providerOf(mc store.CacheStore) serviceapi.CacheProvider {
	return func() (store.CacheStore, error) { return mc, nil }
}

// CA-1: GetApp ミス → upstream 呼ばれ cache に Put
func TestCachingAPI_GetApp_Miss(t *testing.T) {
	mock := &mockAPI{}
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")

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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()
	ctx := context.Background()

	// TTL=1ns で期限切れエントリを直接 Put
	key := "v1:app:example.cybozu.com:42"
	if err := mc.Put(ctx, key, []byte(`{"appId":"42","name":"cached"}`), 1*time.Nanosecond); err != nil {
		t.Fatalf("Put expired: %v", err)
	}
	// 1ns wait
	time.Sleep(2 * time.Nanosecond)

	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()

	apiA := serviceapi.NewCachingAPI(mockA, providerOf(mc), "domainA.cybozu.com")
	apiB := serviceapi.NewCachingAPI(mockB, providerOf(mc), "domainB.cybozu.com")
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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
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
	mc := newMemCache()
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")
	// closed にすると Get/Put が IO エラーになる
	_ = mc.Close()

	// fail-open: エラーにならず upstream を呼ぶ
	_, err := api.GetApp(context.Background(), kintoneapi.GetAppRequest{ID: 1})
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
	mc := newMemCache()
	ctx := context.Background()
	// Put のみ失敗するように設定
	mc.putErr = errors.New("put failed")
	api := serviceapi.NewCachingAPI(mock, providerOf(mc), "example.cybozu.com")

	resp, err := api.GetApp(ctx, kintoneapi.GetAppRequest{ID: 5})
	// Put が失敗しても upstream レスポンスを返す
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("response should not be nil even if Put fails")
	}
}

// CA-12: nil cacheProvider → upstream をそのまま返す
func TestCachingAPI_NilProvider(t *testing.T) {
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

// CA-13: cacheProvider が (nil, err) を返す → fail-open
func TestCachingAPI_CacheProviderError_FailOpen(t *testing.T) {
	mock := &mockAPI{}
	provider := serviceapi.CacheProvider(func() (store.CacheStore, error) {
		return nil, errors.New("redis unreachable")
	})
	api := serviceapi.NewCachingAPI(mock, provider, "example.cybozu.com")

	if _, err := api.GetApp(context.Background(), kintoneapi.GetAppRequest{ID: 1}); err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if mock.getAppCalls != 1 {
		t.Errorf("upstream calls: want 1 (provider error → fail-open), got %d", mock.getAppCalls)
	}
}

// CA-14: per-request lazy resolution — provider が呼ばれるたびに評価される
func TestCachingAPI_PerRequestLazy(t *testing.T) {
	mock := &mockAPI{}
	mc := newMemCache()
	calls := 0
	provider := serviceapi.CacheProvider(func() (store.CacheStore, error) {
		calls++
		return mc, nil
	})
	api := serviceapi.NewCachingAPI(mock, provider, "example.cybozu.com")

	for range 3 {
		if _, err := api.GetApp(context.Background(), kintoneapi.GetAppRequest{ID: 1}); err != nil {
			t.Fatalf("GetApp: %v", err)
		}
	}
	if calls != 3 {
		t.Errorf("provider calls: want 3, got %d", calls)
	}
}
