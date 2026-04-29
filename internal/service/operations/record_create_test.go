package operations_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/service/operations"
)

// OC-1: 単件（Record 指定）
func TestRecordCreate_SingleRecord(t *testing.T) {
	s := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"10"}, Revisions: []string{"1"}}, nil
		},
	}
	in := operations.RecordCreateInput{
		App:    42,
		Record: map[string]any{"name": map[string]any{"value": "x"}},
	}
	out, err := operations.RecordCreate(context.Background(), s, nil, in)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotInsertReq == nil {
		t.Fatal("Insert not called")
	}
	if len(s.gotInsertReq.Records) != 1 {
		t.Errorf("records len=%d", len(s.gotInsertReq.Records))
	}
	if !reflect.DeepEqual(out.IDs, []int64{10}) {
		t.Errorf("ids=%v", out.IDs)
	}
	if !reflect.DeepEqual(out.Revisions, []int64{1}) {
		t.Errorf("revisions=%v", out.Revisions)
	}
}

// OC-2: 複数件（Records 指定）
func TestRecordCreate_MultipleRecords(t *testing.T) {
	s := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"10", "11"}, Revisions: []string{"1", "1"}}, nil
		},
	}
	in := operations.RecordCreateInput{
		App: 42,
		Records: []map[string]any{
			{"a": map[string]any{"value": "1"}},
			{"b": map[string]any{"value": "2"}},
		},
	}
	out, err := operations.RecordCreate(context.Background(), s, nil, in)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(s.gotInsertReq.Records) != 2 {
		t.Errorf("records len=%d", len(s.gotInsertReq.Records))
	}
	if !reflect.DeepEqual(out.IDs, []int64{10, 11}) {
		t.Errorf("ids=%v", out.IDs)
	}
}

// OC-3: 両方指定 → ErrConflictingRecords
func TestRecordCreate_Conflicting(t *testing.T) {
	s := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			t.Fatal("should not be called")
			return nil, nil
		},
	}
	_, err := operations.RecordCreate(context.Background(), s, nil, operations.RecordCreateInput{
		App: 1, Record: map[string]any{"x": 1}, Records: []map[string]any{{"y": 2}},
	})
	if !errors.Is(err, operations.ErrConflictingRecords) {
		t.Errorf("err=%v", err)
	}
}

// OC-4: 両方未指定 → ErrEmptyRecords
func TestRecordCreate_Empty(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordCreate(context.Background(), s, nil, operations.RecordCreateInput{App: 1})
	if !errors.Is(err, operations.ErrEmptyRecords) {
		t.Errorf("err=%v", err)
	}
}

// OC-5: App=0 → ErrInvalidApp
func TestRecordCreate_InvalidApp(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordCreate(context.Background(), s, nil, operations.RecordCreateInput{
		App: 0, Record: map[string]any{"x": 1},
	})
	if !errors.Is(err, operations.ErrInvalidApp) {
		t.Errorf("err=%v", err)
	}
}

// OC-6: API エラー透過
func TestRecordCreate_APIErrorPassThrough(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 401, Category: kintoneapi.CategoryUnauthorized}
	s := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			return nil, apiErr
		},
	}
	_, err := operations.RecordCreate(context.Background(), s, nil, operations.RecordCreateInput{
		App: 1, Record: map[string]any{"x": 1},
	})
	var got *kintoneapi.APIError
	if !errors.As(err, &got) {
		t.Fatalf("expected APIError, got %v", err)
	}
}

// OC-7: id パース不能 → エラー
func TestRecordCreate_IDParseError(t *testing.T) {
	s := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"abc"}, Revisions: []string{"1"}}, nil
		},
	}
	_, err := operations.RecordCreate(context.Background(), s, nil, operations.RecordCreateInput{
		App: 1, Record: map[string]any{"x": 1},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
