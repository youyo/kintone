package operations_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/service/operations"
)

// OD-1: 正常 ids のみ
func TestRecordDelete_IDsOnly(t *testing.T) {
	s := &stubAPI{
		deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
			return nil
		},
	}
	out, err := operations.RecordDelete(context.Background(), s, nil, operations.RecordDeleteInput{
		App: 42, IDs: []int64{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(s.gotDeleteReq.IDs, []int64{1, 2, 3}) {
		t.Errorf("ids=%v", s.gotDeleteReq.IDs)
	}
	if len(s.gotDeleteReq.Revisions) != 0 {
		t.Errorf("revisions=%v want empty", s.gotDeleteReq.Revisions)
	}
	if out.Deleted != 3 {
		t.Errorf("deleted=%d", out.Deleted)
	}
}

// OD-2: 正常 ids + revisions
func TestRecordDelete_WithRevisions(t *testing.T) {
	s := &stubAPI{
		deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
			return nil
		},
	}
	_, err := operations.RecordDelete(context.Background(), s, nil, operations.RecordDeleteInput{
		App: 42, IDs: []int64{1, 2}, Revisions: []int64{10, 11},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(s.gotDeleteReq.Revisions, []int64{10, 11}) {
		t.Errorf("revisions=%v", s.gotDeleteReq.Revisions)
	}
}

// OD-3: App=0
func TestRecordDelete_InvalidApp(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordDelete(context.Background(), s, nil, operations.RecordDeleteInput{
		IDs: []int64{1},
	})
	if !errors.Is(err, operations.ErrInvalidApp) {
		t.Errorf("err=%v", err)
	}
}

// OD-4: IDs 空
func TestRecordDelete_EmptyIDs(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordDelete(context.Background(), s, nil, operations.RecordDeleteInput{
		App: 1, IDs: nil,
	})
	if !errors.Is(err, operations.ErrEmptyIDs) {
		t.Errorf("err=%v", err)
	}
}

// OD-5: IDs に 0 含む
func TestRecordDelete_InvalidID(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordDelete(context.Background(), s, nil, operations.RecordDeleteInput{
		App: 1, IDs: []int64{1, 0, 2},
	})
	if !errors.Is(err, operations.ErrInvalidID) {
		t.Errorf("err=%v", err)
	}
}

// OD-6: revisions 長さ不一致
func TestRecordDelete_RevisionsLengthMismatch(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordDelete(context.Background(), s, nil, operations.RecordDeleteInput{
		App: 1, IDs: []int64{1, 2}, Revisions: []int64{10},
	})
	if !errors.Is(err, operations.ErrRevisionsLengthMismatch) {
		t.Errorf("err=%v", err)
	}
}

// OD-7: API エラー透過
func TestRecordDelete_APIErrorPassThrough(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 403, Category: kintoneapi.CategoryForbidden}
	s := &stubAPI{
		deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
			return apiErr
		},
	}
	_, err := operations.RecordDelete(context.Background(), s, nil, operations.RecordDeleteInput{
		App: 1, IDs: []int64{1},
	})
	var got *kintoneapi.APIError
	if !errors.As(err, &got) {
		t.Fatalf("expected APIError, got %v", err)
	}
}
