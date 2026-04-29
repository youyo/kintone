package ops_test

import (
	"context"
	"testing"

	cliops "github.com/youyo/kintone/internal/cli/ops"
	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// stubAPI は serviceapi.API のスタブ。cli/ops 配下で共有する。
//
// 並列テスト禁止: NewAPIBuilder のグローバル var 差し替えが goroutine 安全でないため、
// cli/ops 配下のテストでは t.Parallel() を使わない。
type stubAPI struct {
	getRecordsFn    func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error)
	getRecordFn     func(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error)
	getAppFn        func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error)
	getFormFieldsFn func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)
	insertRecordsFn func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error)
	updateRecordFn  func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error)
	deleteRecordsFn func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error

	gotInsert *kintoneapi.InsertRecordsRequest
	gotUpdate *kintoneapi.UpdateRecordRequest
	gotDelete *kintoneapi.DeleteRecordsRequest
}

func (s *stubAPI) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	if s.getRecordsFn != nil {
		return s.getRecordsFn(ctx, req)
	}
	return &kintoneapi.GetRecordsResponse{}, nil
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
func (s *stubAPI) InsertRecords(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
	s.gotInsert = &req
	if s.insertRecordsFn != nil {
		return s.insertRecordsFn(ctx, req)
	}
	return &kintoneapi.InsertRecordsResponse{}, nil
}
func (s *stubAPI) UpdateRecord(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
	s.gotUpdate = &req
	if s.updateRecordFn != nil {
		return s.updateRecordFn(ctx, req)
	}
	return &kintoneapi.UpdateRecordResponse{}, nil
}
func (s *stubAPI) DeleteRecords(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
	s.gotDelete = &req
	if s.deleteRecordsFn != nil {
		return s.deleteRecordsFn(ctx, req)
	}
	return nil
}

// withStubAPI は cliops.NewAPIBuilder hook を stub 実装に差し替えて test 終了時に元に戻す。
func withStubAPI(t *testing.T, s serviceapi.API) {
	t.Helper()
	old := cliops.NewAPIBuilder
	cliops.NewAPIBuilder = func(_ cliops.LoaderInput) (serviceapi.API, error) {
		return s, nil
	}
	t.Cleanup(func() { cliops.NewAPIBuilder = old })
}

// OR-1 / OR-2: NewCmd 構築・必要なサブコマンドが登録されている
func TestOpsCmd_Structure(t *testing.T) {
	cmd := cliops.NewCmd()
	if cmd.Use != "ops" {
		t.Errorf("Use=%q", cmd.Use)
	}
	subs := map[string]bool{}
	for _, c := range cmd.Commands() {
		subs[c.Use] = true
	}
	if !subs["record"] {
		t.Errorf("record subcommand missing")
	}
	if !subs["app"] {
		t.Errorf("app subcommand missing")
	}
}
