package facade_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/kintone/internal/mcp/facade"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// stubResolver は facade.APIResolver のテスト用実装。
type stubResolver struct {
	api serviceapi.API
	err error
}

func (s *stubResolver) ForContext(_ context.Context) (serviceapi.API, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.api, nil
}

// F1: Factory=nil なら deps.API が使われる
// （既存の AppsSearch_OK 等が暗黙的に確認しているが、明示する）
func TestResolveAPI_FactoryNilUsesAPI(t *testing.T) {
	t.Parallel()

	m := &mockAPI{}
	h := facade.AppsSearchHandler(facade.ToolDeps{API: m, Factory: nil})

	req := mcp.CallToolRequest{}
	req.Params.Name = "apps_search"
	req.Params.Arguments = map[string]any{}

	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if m.gotListApps == nil {
		t.Error("expected mockAPI.ListApps to be called")
	}
}

// F2 / F3 / F4: Factory が ErrAuthRequired を返すとき AUTH_REQUIRED envelope が返る
func TestResolveAPI_AuthRequired(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{err: serviceapi.ErrAuthRequired}
	h := facade.AppsSearchHandler(facade.ToolDeps{API: nil, Factory: resolver})

	req := mcp.CallToolRequest{}
	req.Params.Name = "apps_search"
	req.Params.Arguments = map[string]any{}

	res, err := h(context.Background(), req)
	if err != nil {
		t.Fatalf("handler should not return protocol-level error, got: %v", err)
	}
	body := extractText(t, res)
	if !strings.Contains(body, `"AUTH_REQUIRED"`) {
		t.Errorf("expected AUTH_REQUIRED envelope, got: %s", body)
	}
	if !strings.Contains(body, `"ok":false`) {
		t.Errorf("expected ok:false, got: %s", body)
	}
}

// F5: Factory が API を返したらそちらが使われる（fallback API は無視）
func TestResolveAPI_FactoryReturnsAPI(t *testing.T) {
	t.Parallel()

	primary := &mockAPI{}
	fallback := &mockAPI{}
	resolver := &stubResolver{api: primary}
	h := facade.AppsSearchHandler(facade.ToolDeps{API: fallback, Factory: resolver})

	req := mcp.CallToolRequest{}
	req.Params.Name = "apps_search"
	req.Params.Arguments = map[string]any{}

	if _, err := h(context.Background(), req); err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if primary.gotListApps == nil {
		t.Error("expected primary API to receive ListApps")
	}
	if fallback.gotListApps != nil {
		t.Error("fallback API must not be called when Factory returns API")
	}
}

// extractText は CallToolResult から TextContent.Text を取り出す。
func extractText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("empty content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("not TextContent: %T", res.Content[0])
	}
	return tc.Text
}
