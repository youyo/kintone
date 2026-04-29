package facade_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/mcp/facade"
)

// FQ-1: records_query 成功（totalCount 含む）
func TestRecordsQuery_OK(t *testing.T) {
	t.Parallel()
	tc := "5"
	m := &mockAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return &kintoneapi.GetRecordsResponse{
				Records:    []map[string]any{{"name": map[string]any{"value": "foo"}}},
				TotalCount: &tc,
			}, nil
		},
	}
	h := facade.RecordsQueryHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":         42.0,
		"query":       "name = \"foo\"",
		"fields":      []any{"name"},
		"total_count": true,
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
	var data struct {
		Records    []map[string]any `json:"records"`
		TotalCount *int64           `json:"total_count"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.Records) != 1 {
		t.Errorf("records=%v", data.Records)
	}
	if data.TotalCount == nil || *data.TotalCount != 5 {
		t.Errorf("total_count=%v", data.TotalCount)
	}
	// API.GetRecords への引数確認
	if m.gotGetRecords == nil || m.gotGetRecords.App != 42 || m.gotGetRecords.Query != `name = "foo"` || !m.gotGetRecords.TotalCount {
		t.Errorf("got=%+v", m.gotGetRecords)
	}
}

// FQ-2: app=0
func TestRecordsQuery_AppZero(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordsQueryHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app": 0.0})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("got=%s", got)
	}
}

// FQ-3: API 401
func TestRecordsQuery_Unauthorized(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		getRecordsFn: func(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
			return nil, &kintoneapi.APIError{HTTPStatus: 401, Category: kintoneapi.CategoryUnauthorized}
		},
	}
	h := facade.RecordsQueryHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app": 1.0})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "KINTONE_UNAUTHORIZED" {
		t.Fatalf("got=%s", got)
	}
}
