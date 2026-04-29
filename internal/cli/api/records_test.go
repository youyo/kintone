package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
	cliapi "github.com/youyo/kintone/internal/cli/api"
	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// stubAPI は serviceapi.API の最小スタブ。テスト hook で差し替える。
type stubAPI struct {
	getRecordsFn    func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error)
	getRecordFn     func(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error)
	getAppFn        func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error)
	getFormFieldsFn func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)

	gotRecords *kintoneapi.GetRecordsRequest
}

func (s *stubAPI) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	s.gotRecords = &req
	if s.getRecordsFn != nil {
		return s.getRecordsFn(ctx, req)
	}
	return &kintoneapi.GetRecordsResponse{Records: []map[string]any{}}, nil
}
func (s *stubAPI) GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
	if s.getRecordFn != nil {
		return s.getRecordFn(ctx, req)
	}
	return &kintoneapi.GetRecordResponse{}, nil
}
func (s *stubAPI) GetApp(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
	if s.getAppFn != nil {
		return s.getAppFn(ctx, req)
	}
	return &kintoneapi.GetAppResponse{}, nil
}
func (s *stubAPI) GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
	if s.getFormFieldsFn != nil {
		return s.getFormFieldsFn(ctx, req)
	}
	return &kintoneapi.GetFormFieldsResponse{}, nil
}

// withStubAPI は cliapi.NewAPIBuilder hook を stub 実装に差し替えて test 終了時に元に戻す。
func withStubAPI(t *testing.T, s serviceapi.API) {
	t.Helper()
	old := cliapi.NewAPIBuilder
	cliapi.NewAPIBuilder = func(_ cliapi.LoaderInput) (serviceapi.API, error) {
		return s, nil
	}
	t.Cleanup(func() { cliapi.NewAPIBuilder = old })
}

// CR-1: records get 正常
func TestCmd_RecordsGet_OK(t *testing.T) {
	stub := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			tc := "3"
			return &kintoneapi.GetRecordsResponse{
				Records:    []map[string]any{{"name": map[string]any{"value": "foo"}}},
				TotalCount: &tc,
			}, nil
		},
	}
	withStubAPI(t, stub)

	root := cli.NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"api", "records", "get",
		"--app", "42",
		"--query", `name = "foo"`,
		"--field", "name", "--field", "age",
		"--total-count",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v / out=%s", err, out.String())
	}
	if stub.gotRecords.App != 42 || stub.gotRecords.Query != `name = "foo"` ||
		!stub.gotRecords.TotalCount ||
		!reflect.DeepEqual(stub.gotRecords.Fields, []string{"name", "age"}) {
		t.Errorf("req=%+v", stub.gotRecords)
	}

	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Records    []map[string]any `json:"records"`
			TotalCount *int64           `json:"total_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v / %s", err, out.String())
	}
	if !got.OK {
		t.Errorf("ok=%v out=%s", got.OK, out.String())
	}
	if got.Data.TotalCount == nil || *got.Data.TotalCount != 3 {
		t.Errorf("total_count=%v", got.Data.TotalCount)
	}
	if len(got.Data.Records) != 1 {
		t.Errorf("records=%v", got.Data.Records)
	}
}

// CR-2: --app 必須（cobra の MarkFlagRequired 経由 → USAGE）
func TestCmd_RecordsGet_MissingApp(t *testing.T) {
	withStubAPI(t, &stubAPI{}) // 念のため呼ばれてもいいよう設定

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"api", "records", "get"}, &out, &errOut)
	if err == nil {
		t.Fatalf("expected error, out=%s", out.String())
	}
	var got struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal(out.Bytes(), &got); jerr != nil {
		t.Fatalf("json: %v / %s", jerr, out.String())
	}
	if got.OK || got.Error.Code != "USAGE" {
		t.Errorf("expected USAGE, got %+v", got)
	}
}

// CR-3: API エラー透過 → KINTONE_UNAUTHORIZED
func TestCmd_RecordsGet_APIError(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 401, Category: kintoneapi.CategoryUnauthorized, Message: "auth"}
	stub := &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return nil, apiErr
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"api", "records", "get", "--app", "1"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"KINTONE_UNAUTHORIZED"`) {
		t.Errorf("unexpected output: %s", out.String())
	}
}
