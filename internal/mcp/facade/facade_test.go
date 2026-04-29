package facade_test

import (
	"context"

	"github.com/youyo/kintone/internal/kintoneapi"
)

// mockAPI は service/api.API のモック実装。各テストで個別に hook を設定する。
type mockAPI struct {
	listAppsFn      func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error)
	getRecordsFn    func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error)
	getRecordFn     func(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error)
	getAppFn        func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error)
	getFormFieldsFn func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)
	insertRecordsFn func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error)
	updateRecordFn  func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error)
	deleteRecordsFn func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error

	gotListApps     *kintoneapi.ListAppsRequest
	gotGetRecords   *kintoneapi.GetRecordsRequest
	gotGetApp       *kintoneapi.GetAppRequest
	gotInsert       *kintoneapi.InsertRecordsRequest
	gotUpdate       *kintoneapi.UpdateRecordRequest
	gotDelete       *kintoneapi.DeleteRecordsRequest
	gotGetFormField *kintoneapi.GetFormFieldsRequest
}

func (m *mockAPI) ListApps(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
	m.gotListApps = &req
	if m.listAppsFn != nil {
		return m.listAppsFn(ctx, req)
	}
	return &kintoneapi.ListAppsResponse{}, nil
}
func (m *mockAPI) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	m.gotGetRecords = &req
	if m.getRecordsFn != nil {
		return m.getRecordsFn(ctx, req)
	}
	return &kintoneapi.GetRecordsResponse{}, nil
}
func (m *mockAPI) GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
	if m.getRecordFn != nil {
		return m.getRecordFn(ctx, req)
	}
	return &kintoneapi.GetRecordResponse{}, nil
}
func (m *mockAPI) GetApp(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
	m.gotGetApp = &req
	if m.getAppFn != nil {
		return m.getAppFn(ctx, req)
	}
	return &kintoneapi.GetAppResponse{}, nil
}
func (m *mockAPI) GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
	m.gotGetFormField = &req
	if m.getFormFieldsFn != nil {
		return m.getFormFieldsFn(ctx, req)
	}
	return &kintoneapi.GetFormFieldsResponse{}, nil
}
func (m *mockAPI) InsertRecords(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
	m.gotInsert = &req
	if m.insertRecordsFn != nil {
		return m.insertRecordsFn(ctx, req)
	}
	return &kintoneapi.InsertRecordsResponse{}, nil
}
func (m *mockAPI) UpdateRecord(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
	m.gotUpdate = &req
	if m.updateRecordFn != nil {
		return m.updateRecordFn(ctx, req)
	}
	return &kintoneapi.UpdateRecordResponse{}, nil
}
func (m *mockAPI) DeleteRecords(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
	m.gotDelete = &req
	if m.deleteRecordsFn != nil {
		return m.deleteRecordsFn(ctx, req)
	}
	return nil
}
