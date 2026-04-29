package operations_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/service/operations"
)

// stubAPI は service/api.API のスタブ実装。
// 各メソッドに対応するハンドラを設定し、呼び出し記録を取れる。
type stubAPI struct {
	getRecordsFn    func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error)
	getRecordFn     func(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error)
	getAppFn        func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error)
	getFormFieldsFn func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)

	// 記録
	gotRecordsReq    *kintoneapi.GetRecordsRequest
	gotAppReq        *kintoneapi.GetAppRequest
	gotFormFieldsReq *kintoneapi.GetFormFieldsRequest
}

func (s *stubAPI) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	s.gotRecordsReq = &req
	if s.getRecordsFn == nil {
		return &kintoneapi.GetRecordsResponse{}, nil
	}
	return s.getRecordsFn(ctx, req)
}
func (s *stubAPI) GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
	if s.getRecordFn == nil {
		return &kintoneapi.GetRecordResponse{}, nil
	}
	return s.getRecordFn(ctx, req)
}
func (s *stubAPI) GetApp(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
	s.gotAppReq = &req
	if s.getAppFn == nil {
		return &kintoneapi.GetAppResponse{}, nil
	}
	return s.getAppFn(ctx, req)
}
func (s *stubAPI) GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
	s.gotFormFieldsReq = &req
	if s.getFormFieldsFn == nil {
		return &kintoneapi.GetFormFieldsResponse{}, nil
	}
	return s.getFormFieldsFn(ctx, req)
}

// strPtr は文字列ポインタを生成する。
func strPtr(s string) *string { return &s }

// OQ-1: 全引数を渡し、TotalCount が int64 化される
func TestRecordsQuery_AllParams(t *testing.T) {
	s := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return &kintoneapi.GetRecordsResponse{
				Records:    []map[string]any{{"name": map[string]any{"value": "foo"}}},
				TotalCount: strPtr("5"),
			}, nil
		},
	}
	in := operations.RecordsQueryInput{
		App: 42, Query: `name = "foo"`, Fields: []string{"name", "age"}, TotalCount: true,
	}
	out, err := operations.RecordsQuery(context.Background(), s, in)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(s.gotRecordsReq.Fields, []string{"name", "age"}) {
		t.Errorf("Fields=%v", s.gotRecordsReq.Fields)
	}
	if s.gotRecordsReq.App != 42 || s.gotRecordsReq.Query != `name = "foo"` || !s.gotRecordsReq.TotalCount {
		t.Errorf("req=%+v", s.gotRecordsReq)
	}
	if len(out.Records) != 1 {
		t.Errorf("records=%v", out.Records)
	}
	if out.TotalCount == nil || *out.TotalCount != 5 {
		t.Errorf("total_count=%v", out.TotalCount)
	}
}

// OQ-2: 最小（App のみ）→ TotalCount=nil
func TestRecordsQuery_Minimal(t *testing.T) {
	s := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return &kintoneapi.GetRecordsResponse{Records: []map[string]any{}}, nil
		},
	}
	out, err := operations.RecordsQuery(context.Background(), s, operations.RecordsQueryInput{App: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.TotalCount != nil {
		t.Errorf("total_count=%v want nil", out.TotalCount)
	}
	if len(out.Records) != 0 {
		t.Errorf("records=%v", out.Records)
	}
}

// OQ-3: App=0 → ErrInvalidApp（API 呼ばれない）
func TestRecordsQuery_InvalidApp(t *testing.T) {
	s := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			t.Fatal("should not be called")
			return nil, nil
		},
	}
	_, err := operations.RecordsQuery(context.Background(), s, operations.RecordsQueryInput{App: 0})
	if !errors.Is(err, operations.ErrInvalidApp) {
		t.Errorf("err=%v want ErrInvalidApp", err)
	}
}

// OQ-4: totalCount="0" の場合
func TestRecordsQuery_TotalCountZero(t *testing.T) {
	s := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return &kintoneapi.GetRecordsResponse{Records: []map[string]any{}, TotalCount: strPtr("0")}, nil
		},
	}
	out, err := operations.RecordsQuery(context.Background(), s, operations.RecordsQueryInput{App: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.TotalCount == nil || *out.TotalCount != 0 {
		t.Errorf("total_count=%v want *0", out.TotalCount)
	}
}

// OQ-5: totalCount が不正文字列 → エラー
func TestRecordsQuery_InvalidTotalCount(t *testing.T) {
	s := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return &kintoneapi.GetRecordsResponse{Records: []map[string]any{}, TotalCount: strPtr("abc")}, nil
		},
	}
	_, err := operations.RecordsQuery(context.Background(), s, operations.RecordsQueryInput{App: 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

// OQ-6: api 層がエラー透過
func TestRecordsQuery_APIErrorPassThrough(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 401, Category: kintoneapi.CategoryUnauthorized}
	s := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return nil, apiErr
		},
	}
	_, err := operations.RecordsQuery(context.Background(), s, operations.RecordsQueryInput{App: 1})
	var got *kintoneapi.APIError
	if !errors.As(err, &got) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if got.HTTPStatus != 401 {
		t.Errorf("status=%d", got.HTTPStatus)
	}
}

// OQ-7: ctx cancel 透過
func TestRecordsQuery_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return nil, ctx.Err()
		},
	}
	_, err := operations.RecordsQuery(ctx, s, operations.RecordsQueryInput{App: 1})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v want context.Canceled", err)
	}
}
