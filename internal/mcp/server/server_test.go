package server_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/kintoneapi"
	mcpserver "github.com/youyo/kintone/internal/mcp/server"
)

// stubAPI は service/api.API 互換の最小スタブ。
// 各 hook を nil 残置するとデフォルト空レスポンスを返す。
type stubAPI struct {
	listAppsFn      func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error)
	getRecordsFn    func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error)
	getAppFn        func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error)
	getFormFieldsFn func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)
	insertRecordsFn func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error)
	updateRecordFn  func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error)
	deleteRecordsFn func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error
}

func (s *stubAPI) ListApps(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
	if s.listAppsFn != nil {
		return s.listAppsFn(ctx, req)
	}
	return &kintoneapi.ListAppsResponse{}, nil
}
func (s *stubAPI) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	if s.getRecordsFn != nil {
		return s.getRecordsFn(ctx, req)
	}
	return &kintoneapi.GetRecordsResponse{}, nil
}
func (s *stubAPI) GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
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
	if s.insertRecordsFn != nil {
		return s.insertRecordsFn(ctx, req)
	}
	return &kintoneapi.InsertRecordsResponse{}, nil
}
func (s *stubAPI) UpdateRecord(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
	if s.updateRecordFn != nil {
		return s.updateRecordFn(ctx, req)
	}
	return &kintoneapi.UpdateRecordResponse{}, nil
}
func (s *stubAPI) DeleteRecords(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
	if s.deleteRecordsFn != nil {
		return s.deleteRecordsFn(ctx, req)
	}
	return nil
}

// newClient は in-process client を初期化して返す。
func newClient(t *testing.T, api *stubAPI) *client.Client {
	t.Helper()
	srv := mcpserver.New(api)
	c, err := client.NewInProcessClient(srv)
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = "2024-11-05"
	initReq.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "1.0.0"}
	if _, err := c.Initialize(context.Background(), initReq); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return c
}

// callTool はサーバーへ tools/call を投げ、Content[0] のテキストを返す。
func callTool(t *testing.T, c *client.Client, name string, args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := c.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("nil result for %s", name)
	}
	tc, ok := mcp.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("not text content: %#v", res.Content[0])
	}
	return tc.Text
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func parseEnvelope(t *testing.T, s string) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		t.Fatalf("unmarshal envelope %q: %v", s, err)
	}
	return e
}

// MS-1: 6 tools が ListTools で返される
func TestServer_ListTools(t *testing.T) {
	t.Parallel()
	c := newClient(t, &stubAPI{})

	res, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	want := map[string]bool{
		"apps_search":   true,
		"app_describe":  true,
		"records_query": true,
		"record_create": true,
		"record_update": true,
		"record_delete": true,
	}
	for _, tool := range res.Tools {
		delete(want, tool.Name)
	}
	if len(want) > 0 {
		var missing []string
		for k := range want {
			missing = append(missing, k)
		}
		t.Errorf("missing tools: %v (got %d tools)", missing, len(res.Tools))
	}
}

// MS-2: apps_search 呼び出し
func TestServer_AppsSearch(t *testing.T) {
	t.Parallel()
	c := newClient(t, &stubAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "1", Code: "x", Name: "X"}}}, nil
		},
	})
	got := callTool(t, c, "apps_search", map[string]any{"name": "X"})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
	if !strings.Contains(string(e.Data), `"X"`) {
		t.Errorf("data=%s", string(e.Data))
	}
}

// MS-3: app_describe
func TestServer_AppDescribe(t *testing.T) {
	t.Parallel()
	c := newClient(t, &stubAPI{
		getAppFn: func(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
			return &kintoneapi.GetAppResponse{AppID: "42", Code: "hr", Name: "人事"}, nil
		},
		getFormFieldsFn: func(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
			return &kintoneapi.GetFormFieldsResponse{Properties: map[string]map[string]any{"name": {"type": "SINGLE_LINE_TEXT"}}, Revision: "3"}, nil
		},
	})
	got := callTool(t, c, "app_describe", map[string]any{"app": 42.0})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
}

// MS-4: records_query
func TestServer_RecordsQuery(t *testing.T) {
	t.Parallel()
	c := newClient(t, &stubAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return &kintoneapi.GetRecordsResponse{Records: []map[string]any{{"name": map[string]any{"value": "foo"}}}}, nil
		},
	})
	got := callTool(t, c, "records_query", map[string]any{"app": 1.0})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
}

// MS-5: record_create
func TestServer_RecordCreate(t *testing.T) {
	t.Parallel()
	c := newClient(t, &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"100"}, Revisions: []string{"1"}}, nil
		},
	})
	got := callTool(t, c, "record_create", map[string]any{
		"app":    1.0,
		"record": map[string]any{"name": map[string]any{"value": "foo"}},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
}

// MS-6: record_update
func TestServer_RecordUpdate(t *testing.T) {
	t.Parallel()
	c := newClient(t, &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "5"}, nil
		},
	})
	got := callTool(t, c, "record_update", map[string]any{
		"app":    1.0,
		"id":     7.0,
		"record": map[string]any{"name": map[string]any{"value": "foo"}},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
}

// MS-7: record_delete
func TestServer_RecordDelete(t *testing.T) {
	t.Parallel()
	c := newClient(t, &stubAPI{})
	got := callTool(t, c, "record_delete", map[string]any{
		"app": 1.0,
		"ids": []any{1.0, 2.0},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
}

// MS-8: 不正引数（app="abc"）でも JSON envelope を返す（panic しない）
func TestServer_BadArguments(t *testing.T) {
	t.Parallel()
	c := newClient(t, &stubAPI{})
	got := callTool(t, c, "app_describe", map[string]any{"app": "abc"})
	e := parseEnvelope(t, got)
	if e.OK {
		t.Fatalf("ok=true: %s", got)
	}
	if e.Error == nil || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("err=%+v", e.Error)
	}
}
