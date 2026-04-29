package resolver

import (
	"context"
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
)

func formFieldsResp(props map[string]map[string]any) *kintoneapi.GetFormFieldsResponse {
	return &kintoneapi.GetFormFieldsResponse{Properties: props, Revision: "1"}
}

func TestResolveField_EmptyRef_R17(t *testing.T) {
	r := New(&mockAPI{})
	_, err := r.ResolveField(context.Background(), 42, "")
	if !errors.Is(err, ErrEmptyRef) {
		t.Fatalf("expected ErrEmptyRef, got %v", err)
	}
}

func TestResolveField_InvalidAppID_R18(t *testing.T) {
	r := New(&mockAPI{})
	_, err := r.ResolveField(context.Background(), 0, "name")
	if !errors.Is(err, ErrInvalidAppID) {
		t.Fatalf("expected ErrInvalidAppID, got %v", err)
	}
}

func TestResolveField_CodeExact_R13(t *testing.T) {
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return formFieldsResp(map[string]map[string]any{
				"name": {"label": "氏名", "type": "SINGLE_LINE_TEXT"},
			}), nil
		},
	}
	r := New(m)
	code, err := r.ResolveField(context.Background(), 42, "name")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if code != "name" {
		t.Fatalf("expected name, got %q", code)
	}
}

func TestResolveField_LabelExact_R14(t *testing.T) {
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return formFieldsResp(map[string]map[string]any{
				"customer_name":  {"label": "顧客名", "type": "SINGLE_LINE_TEXT"},
				"company":        {"label": "会社名", "type": "SINGLE_LINE_TEXT"},
				"customer_phone": {"label": "顧客電話", "type": "SINGLE_LINE_TEXT"},
			}), nil
		},
	}
	r := New(m)
	code, err := r.ResolveField(context.Background(), 42, "顧客名")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if code != "customer_name" {
		t.Fatalf("expected customer_name, got %q", code)
	}
}

func TestResolveField_LabelExactAmbiguous(t *testing.T) {
	// 複数 label が同名（ありえないが防御）
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return formFieldsResp(map[string]map[string]any{
				"a": {"label": "名前", "type": "SINGLE_LINE_TEXT"},
				"b": {"label": "名前", "type": "SINGLE_LINE_TEXT"},
			}), nil
		},
	}
	r := New(m)
	_, err := r.ResolveField(context.Background(), 42, "名前")
	var ae *AmbiguousError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AmbiguousError, got %v", err)
	}
	if len(ae.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(ae.Candidates))
	}
}

func TestResolveField_LabelPartialAmbiguous_R15(t *testing.T) {
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return formFieldsResp(map[string]map[string]any{
				"a": {"label": "氏名（フル名前）", "type": "SINGLE_LINE_TEXT"},
				"b": {"label": "名前カナ", "type": "SINGLE_LINE_TEXT"},
				"c": {"label": "ユーザー名前", "type": "SINGLE_LINE_TEXT"},
			}), nil
		},
	}
	r := New(m)
	_, err := r.ResolveField(context.Background(), 42, "名前")
	var ae *AmbiguousError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AmbiguousError, got %v", err)
	}
	if !errors.Is(err, ErrFieldAmbiguous) {
		t.Fatalf("errors.Is should match ErrFieldAmbiguous")
	}
	if len(ae.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(ae.Candidates))
	}
}

func TestResolveField_LabelPartialSingle(t *testing.T) {
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return formFieldsResp(map[string]map[string]any{
				"a": {"label": "氏名（フル名前）", "type": "SINGLE_LINE_TEXT"},
				"b": {"label": "電話番号", "type": "SINGLE_LINE_TEXT"},
			}), nil
		},
	}
	r := New(m)
	code, err := r.ResolveField(context.Background(), 42, "名前")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if code != "a" {
		t.Fatalf("expected a, got %q", code)
	}
}

func TestResolveField_NotFound_R16(t *testing.T) {
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return formFieldsResp(map[string]map[string]any{
				"a": {"label": "電話", "type": "SINGLE_LINE_TEXT"},
			}), nil
		},
	}
	r := New(m)
	_, err := r.ResolveField(context.Background(), 42, "氏名")
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected NotFoundError, got %v", err)
	}
	if !errors.Is(err, ErrFieldNotFound) {
		t.Fatalf("errors.Is should match ErrFieldNotFound")
	}
}

func TestResolveField_LabelMissingOrNonString(t *testing.T) {
	// label が無い / 非 string のフィールドはスキップされる
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return formFieldsResp(map[string]map[string]any{
				"a": {"label": 123, "type": "NUMBER"}, // 非 string
				"b": {"type": "SINGLE_LINE_TEXT"},     // label なし
				"c": {"label": "氏名", "type": "TEXT"},  // 正常一致
			}), nil
		},
	}
	r := New(m)
	code, err := r.ResolveField(context.Background(), 42, "氏名")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if code != "c" {
		t.Fatalf("expected c, got %q", code)
	}
}

func TestResolveField_APIErrorPropagated(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 403, Message: "forbidden"}
	m := &mockAPI{
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return nil, apiErr
		},
	}
	r := New(m)
	_, err := r.ResolveField(context.Background(), 42, "顧客名")
	var got *kintoneapi.APIError
	if !errors.As(err, &got) {
		t.Fatalf("expected APIError, got %v", err)
	}
}
