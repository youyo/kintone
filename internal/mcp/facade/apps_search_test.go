package facade_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/mcp/facade"
)

// callTool は handler を直接呼び出して結果テキストを取り出すヘルパ。
func callTool(t *testing.T, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) string {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = "test"
	req.Params.Arguments = args
	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("nil result")
	}
	tc, ok := mcp.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("not text content: %#v", res.Content[0])
	}
	return tc.Text
}

// envelope は出力 JSON envelope の確認用 struct。
type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
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

// FA-1: apps_search 成功（空 args）
func TestAppsSearch_Empty(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return &kintoneapi.ListAppsResponse{Apps: []kintoneapi.AppListEntry{{AppID: "1", Code: "hr", Name: "人事"}}}, nil
		},
	}
	h := facade.AppsSearchHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
	var data struct {
		Apps []kintoneapi.AppListEntry `json:"apps"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("data: %v", err)
	}
	if len(data.Apps) != 1 || data.Apps[0].AppID != "1" {
		t.Fatalf("apps=%v", data.Apps)
	}
}

// FA-2: apps_search の引数が ListAppsRequest にマップされる
func TestAppsSearch_AllArgs(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.AppsSearchHandler(facade.ToolDeps{API: m})
	_ = callTool(t, h, map[string]any{
		"ids":       []any{1.0, 2.0},
		"codes":     []any{"A", "B"},
		"name":      "hr",
		"space_ids": []any{10.0},
		"limit":     50.0,
		"offset":    5.0,
	})
	if m.gotListApps == nil {
		t.Fatal("ListApps not called")
	}
	if !reflect.DeepEqual(m.gotListApps.IDs, []int64{1, 2}) {
		t.Errorf("IDs=%v", m.gotListApps.IDs)
	}
	if !reflect.DeepEqual(m.gotListApps.Codes, []string{"A", "B"}) {
		t.Errorf("Codes=%v", m.gotListApps.Codes)
	}
	if m.gotListApps.Name != "hr" {
		t.Errorf("Name=%q", m.gotListApps.Name)
	}
	if !reflect.DeepEqual(m.gotListApps.SpaceIDs, []int64{10}) {
		t.Errorf("SpaceIDs=%v", m.gotListApps.SpaceIDs)
	}
	if m.gotListApps.Limit != 50 || m.gotListApps.Offset != 5 {
		t.Errorf("limit=%d offset=%d", m.gotListApps.Limit, m.gotListApps.Offset)
	}
}

// FA-3: apps_search 失敗（API 500）
func TestAppsSearch_APIError(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		listAppsFn: func(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
			return nil, &kintoneapi.APIError{HTTPStatus: 500, Category: kintoneapi.CategoryServerError}
		},
	}
	h := facade.AppsSearchHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{})
	e := parseEnvelope(t, got)
	if e.OK {
		t.Fatalf("ok=true: %s", got)
	}
	if e.Error.Code != "KINTONE_INTERNAL" {
		t.Errorf("code=%s", e.Error.Code)
	}
}

// FA-4: 不正な ids 型 → INVALID_PARAMS
func TestAppsSearch_BadIDs(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.AppsSearchHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"ids": "not-an-array",
	})
	e := parseEnvelope(t, got)
	if e.OK {
		t.Fatalf("ok=true: %s", got)
	}
	if e.Error.Code != "INVALID_PARAMS" {
		t.Errorf("code=%s", e.Error.Code)
	}
}
