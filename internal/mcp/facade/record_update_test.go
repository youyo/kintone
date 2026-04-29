package facade_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/mcp/facade"
)

// FU-1: record_update id 経路
func TestRecordUpdate_ByID(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "5"}, nil
		},
	}
	h := facade.RecordUpdateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":    1.0,
		"id":     7.0,
		"record": map[string]any{"name": map[string]any{"value": "x"}},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
	var data struct {
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.Revision != 5 {
		t.Errorf("revision=%d", data.Revision)
	}
	if m.gotUpdate == nil || m.gotUpdate.ID != 7 || m.gotUpdate.UpdateKey != nil {
		t.Errorf("got=%+v", m.gotUpdate)
	}
}

// FU-2: record_update updateKey 経路
func TestRecordUpdate_ByUpdateKey(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "1"}, nil
		},
	}
	h := facade.RecordUpdateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":              1.0,
		"update_key_field": "code",
		"update_key_value": "X",
		"record":           map[string]any{"name": map[string]any{"value": "y"}},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
	if m.gotUpdate == nil || m.gotUpdate.UpdateKey == nil ||
		m.gotUpdate.UpdateKey.Field != "code" || m.gotUpdate.UpdateKey.Value != "X" {
		t.Errorf("got=%+v", m.gotUpdate)
	}
}

// FU-3: id と update_key 両指定
func TestRecordUpdate_Conflict(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordUpdateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":              1.0,
		"id":               7.0,
		"update_key_field": "code",
		"update_key_value": "X",
		"record":           map[string]any{"x": 1},
	})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("got=%s", got)
	}
}

// FU-4: record 空
func TestRecordUpdate_EmptyRecord(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordUpdateHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":    1.0,
		"id":     7.0,
		"record": map[string]any{},
	})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("got=%s", got)
	}
}

// FU-5: revision 指定
func TestRecordUpdate_Revision(t *testing.T) {
	t.Parallel()
	m := &mockAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "10"}, nil
		},
	}
	h := facade.RecordUpdateHandler(facade.ToolDeps{API: m})
	_ = callTool(t, h, map[string]any{
		"app":      1.0,
		"id":       7.0,
		"revision": 9.0,
		"record":   map[string]any{"x": map[string]any{"value": "v"}},
	})
	if m.gotUpdate == nil || m.gotUpdate.Revision == nil || *m.gotUpdate.Revision != 9 {
		t.Errorf("revision=%v", m.gotUpdate)
	}
}
