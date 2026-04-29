package facade_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/youyo/kintone/internal/mcp/facade"
)

// FX-1: record_delete IDs=[1,2]
func TestRecordDelete_OK(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordDeleteHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":       1.0,
		"ids":       []any{1.0, 2.0},
		"revisions": []any{1.0, 1.0},
	})
	e := parseEnvelope(t, got)
	if !e.OK {
		t.Fatalf("ok=false: %s", got)
	}
	var data struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if data.Deleted != 2 {
		t.Errorf("deleted=%d", data.Deleted)
	}
	if m.gotDelete == nil || !reflect.DeepEqual(m.gotDelete.IDs, []int64{1, 2}) ||
		!reflect.DeepEqual(m.gotDelete.Revisions, []int64{1, 1}) {
		t.Errorf("got=%+v", m.gotDelete)
	}
}

// FX-2: ids 空
func TestRecordDelete_Empty(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordDeleteHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app": 1.0,
		"ids": []any{},
	})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("got=%s", got)
	}
}

// FX-3: revisions 長さ不一致
func TestRecordDelete_RevisionLengthMismatch(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordDeleteHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app":       1.0,
		"ids":       []any{1.0, 2.0},
		"revisions": []any{1.0},
	})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("got=%s", got)
	}
}

// FX-4: ids に <= 0 が含まれる
func TestRecordDelete_InvalidID(t *testing.T) {
	t.Parallel()
	m := &mockAPI{}
	h := facade.RecordDeleteHandler(facade.ToolDeps{API: m})
	got := callTool(t, h, map[string]any{
		"app": 1.0,
		"ids": []any{1.0, 0.0},
	})
	e := parseEnvelope(t, got)
	if e.OK || e.Error.Code != "INVALID_PARAMS" {
		t.Fatalf("got=%s", got)
	}
}
