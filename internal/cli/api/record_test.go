package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
	"github.com/youyo/kintone/internal/kintoneapi"
)

// CRec-1: record get 正常
func TestCmd_RecordGet_OK(t *testing.T) {
	stub := &stubAPI{
		getRecordFn: func(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
			if req.App != 42 || req.ID != 7 {
				t.Errorf("req=%+v", req)
			}
			return &kintoneapi.GetRecordResponse{
				Record: map[string]any{"name": map[string]any{"value": "foo"}},
			}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"api", "record", "get", "--app", "42", "--id", "7"}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / out=%s", err, out.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Record map[string]any `json:"record"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v / %s", err, out.String())
	}
	if !got.OK || got.Data.Record == nil {
		t.Errorf("got=%+v / out=%s", got, out.String())
	}
}

// CRec-2: --id 必須（USAGE）
func TestCmd_RecordGet_MissingID(t *testing.T) {
	withStubAPI(t, &stubAPI{})

	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"api", "record", "get", "--app", "42"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE, got: %s", out.String())
	}
}
