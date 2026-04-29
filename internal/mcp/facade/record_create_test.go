package facade_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/mcp/facade"
)

// FC-1: record_create 単件
func TestRecordCreate_Single(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"100"}, Revisions: []string{"1"}}, nil
		},
	}
	h := facade.RecordCreateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":    1.0,
		"record": map[string]any{"name": map[string]any{"value": "foo"}},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
	var data struct {
		IDs       []int64 `json:"ids"`
		Revisions []int64 `json:"revisions"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.IDs) != 1 || data.IDs[0] != 100 {
		t.Errorf("ids=%v", data.IDs)
	}
}

// FC-2: record と records 両指定
func TestRecordCreate_Conflict(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordCreateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":     1.0,
		"record":  map[string]any{"x": 1},
		"records": []any{map[string]any{"x": 2}},
	})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("got=%s", got)
	}
}

// FC-3: どちらも未指定
func TestRecordCreate_Empty(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordCreateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{"app": 1.0})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("got=%s", got)
	}
}
