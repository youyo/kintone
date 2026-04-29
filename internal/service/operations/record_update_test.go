package operations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/service/operations"
)

// OU-1: ID 指定正常
func TestRecordUpdate_ByID(t *testing.T) {
	s := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "3"}, nil
		},
	}
	out, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 42, ID: 7, Record: map[string]any{"x": 1},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotUpdateReq.ID != 7 {
		t.Errorf("got ID=%d", s.gotUpdateReq.ID)
	}
	if s.gotUpdateReq.UpdateKey != nil {
		t.Errorf("UpdateKey should be nil")
	}
	if out.Revision != 3 {
		t.Errorf("revision=%d", out.Revision)
	}
}

// OU-2: ID + revision 指定
func TestRecordUpdate_WithRevision(t *testing.T) {
	s := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "6"}, nil
		},
	}
	rev := int64(5)
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 42, ID: 7, Revision: &rev,
		Record: map[string]any{"x": 1},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotUpdateReq.Revision == nil || *s.gotUpdateReq.Revision != 5 {
		t.Errorf("revision=%v", s.gotUpdateReq.Revision)
	}
}

// OU-3: updateKey 指定
func TestRecordUpdate_ByUpdateKey(t *testing.T) {
	s := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "4"}, nil
		},
	}
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 42, UpdateKeyField: "code", UpdateKeyValue: "A1",
		Record: map[string]any{"x": 1},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotUpdateReq.UpdateKey == nil {
		t.Fatal("UpdateKey nil")
	}
	if s.gotUpdateReq.UpdateKey.Field != "code" || s.gotUpdateReq.UpdateKey.Value != "A1" {
		t.Errorf("UpdateKey=%+v", s.gotUpdateReq.UpdateKey)
	}
	if s.gotUpdateReq.ID != 0 {
		t.Errorf("ID should be zero, got %d", s.gotUpdateReq.ID)
	}
}

// OU-4: ID + UpdateKey 両方 → ErrConflictingUpdateKey
func TestRecordUpdate_Conflicting(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 1, ID: 1, UpdateKeyField: "c", UpdateKeyValue: "v",
		Record: map[string]any{"x": 1},
	})
	if !errors.Is(err, operations.ErrConflictingUpdateKey) {
		t.Errorf("err=%v", err)
	}
}

// OU-5: ID なし & updateKey 半分（Field のみ） → ErrMissingUpdateKey
func TestRecordUpdate_MissingUpdateKeyValue(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 1, UpdateKeyField: "code", Record: map[string]any{"x": 1},
	})
	if !errors.Is(err, operations.ErrMissingUpdateKey) {
		t.Errorf("err=%v", err)
	}
}

// OU-6: ID なし & updateKey 完全 OK
func TestRecordUpdate_OnlyUpdateKey(t *testing.T) {
	s := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "1"}, nil
		},
	}
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 1, UpdateKeyField: "code", UpdateKeyValue: "A",
		Record: map[string]any{"x": 1},
	})
	if err != nil {
		t.Errorf("err=%v", err)
	}
}

// OU-7: App=0
func TestRecordUpdate_InvalidApp(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		ID: 1, Record: map[string]any{"x": 1},
	})
	if !errors.Is(err, operations.ErrInvalidApp) {
		t.Errorf("err=%v", err)
	}
}

// OU-8: Record 空
func TestRecordUpdate_EmptyRecord(t *testing.T) {
	s := &stubAPI{}
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 1, ID: 1,
	})
	if !errors.Is(err, operations.ErrEmptyRecord) {
		t.Errorf("err=%v", err)
	}
}

// OU-9: API エラー透過
func TestRecordUpdate_APIErrorPassThrough(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 422, Category: kintoneapi.CategoryValidation}
	s := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return nil, apiErr
		},
	}
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 1, ID: 1, Record: map[string]any{"x": 1},
	})
	var got *kintoneapi.APIError
	if !errors.As(err, &got) {
		t.Fatalf("expected APIError, got %v", err)
	}
}

// OU-10: revision パース不能
func TestRecordUpdate_RevisionParseError(t *testing.T) {
	s := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "abc"}, nil
		},
	}
	_, err := operations.RecordUpdate(context.Background(), s, operations.RecordUpdateInput{
		App: 1, ID: 1, Record: map[string]any{"x": 1},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
