package operations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/service/operations"
)

// OD-1: 正常合成
func TestAppDescribe_OK(t *testing.T) {
	s := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return &kintoneapi.GetAppResponse{
				AppID:       "42",
				Code:        "myapp",
				Name:        "テスト",
				Description: "desc",
				CreatedAt:   "2026-01-01T00:00:00Z",
				Creator:     map[string]any{"code": "u1"},
				ModifiedAt:  "2026-04-29T00:00:00Z",
				Modifier:    map[string]any{"code": "u2"},
			}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{
				Properties: map[string]map[string]any{
					"name": {"type": "SINGLE_LINE_TEXT"},
				},
				Revision: "5",
			}, nil
		},
	}
	out, err := operations.AppDescribe(context.Background(), s, operations.AppDescribeInput{App: 42, Lang: "ja"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.App.AppID != "42" || out.App.Name != "テスト" || out.App.Code != "myapp" {
		t.Errorf("app=%+v", out.App)
	}
	if out.Revision != "5" {
		t.Errorf("revision=%q", out.Revision)
	}
	if _, ok := out.Fields["name"]; !ok {
		t.Errorf("fields=%v", out.Fields)
	}
	// stub の req に App=42 / Lang=ja が伝播
	if s.gotAppReq.ID != 42 {
		t.Errorf("app req=%+v", s.gotAppReq)
	}
	if s.gotFormFieldsReq.App != 42 || s.gotFormFieldsReq.Lang != "ja" {
		t.Errorf("fields req=%+v", s.gotFormFieldsReq)
	}
}

// OD-2: App=0 → ErrInvalidApp
func TestAppDescribe_InvalidApp(t *testing.T) {
	s := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			t.Fatal("should not be called")
			return nil, nil
		},
	}
	_, err := operations.AppDescribe(context.Background(), s, operations.AppDescribeInput{App: 0})
	if !errors.Is(err, operations.ErrInvalidApp) {
		t.Errorf("err=%v", err)
	}
}

// OD-3: GetApp 失敗 → GetFormFields は呼ばれない
func TestAppDescribe_GetAppError(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 404, Category: kintoneapi.CategoryNotFound}
	s := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return nil, apiErr
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			t.Fatal("should not be called")
			return nil, nil
		},
	}
	_, err := operations.AppDescribe(context.Background(), s, operations.AppDescribeInput{App: 42})
	var got *kintoneapi.APIError
	if !errors.As(err, &got) {
		t.Fatalf("expected APIError, got %v", err)
	}
}

// OD-4: GetFormFields 失敗（GetApp は成功）
func TestAppDescribe_GetFormFieldsError(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 403, Category: kintoneapi.CategoryForbidden}
	s := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return &kintoneapi.GetAppResponse{AppID: "42"}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return nil, apiErr
		},
	}
	_, err := operations.AppDescribe(context.Background(), s, operations.AppDescribeInput{App: 42})
	var got *kintoneapi.APIError
	if !errors.As(err, &got) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if got.Category != kintoneapi.CategoryForbidden {
		t.Errorf("category=%v", got.Category)
	}
}

// OD-5: Lang 指定が下層に伝播
func TestAppDescribe_LangPropagation(t *testing.T) {
	s := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return &kintoneapi.GetAppResponse{AppID: "1"}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{}, nil
		},
	}
	_, err := operations.AppDescribe(context.Background(), s, operations.AppDescribeInput{App: 1, Lang: "en"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotFormFieldsReq.Lang != "en" {
		t.Errorf("lang=%q want en", s.gotFormFieldsReq.Lang)
	}
}

// OD-6: Lang 省略は空文字伝播
func TestAppDescribe_LangOmitted(t *testing.T) {
	s := &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return &kintoneapi.GetAppResponse{AppID: "1"}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{}, nil
		},
	}
	_, err := operations.AppDescribe(context.Background(), s, operations.AppDescribeInput{App: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.gotFormFieldsReq.Lang != "" {
		t.Errorf("lang=%q want empty", s.gotFormFieldsReq.Lang)
	}
}
