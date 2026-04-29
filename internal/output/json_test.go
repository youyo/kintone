package output_test

import (
	"bytes"
	"testing"

	"github.com/youyo/kintone/internal/output"
)

// O-1: Success: シンプルな data
func TestSuccess_Simple(t *testing.T) {
	type payload struct {
		Version string `json:"version"`
	}
	got, err := output.Success(payload{Version: "0.1.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"ok":true,"data":{"version":"0.1.0"}}`
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

// O-2: Success: ネストした data
func TestSuccess_Nested(t *testing.T) {
	type inner struct {
		A int      `json:"a"`
		B []string `json:"b"`
	}
	type payload struct {
		A int      `json:"a"`
		B []string `json:"b"`
	}
	got, err := output.Success(payload{A: 1, B: []string{"x"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"ok":true,"data":{"a":1,"b":["x"]}}`
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

// O-3: Success: 空オブジェクト
func TestSuccess_EmptyObject(t *testing.T) {
	got, err := output.Success(struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"ok":true,"data":{}}`
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

// O-4: Failure: 標準エラー（details 無し）
func TestFailure_NoDetails(t *testing.T) {
	got, err := output.Failure(&output.Error{Code: "CONFIG_NOT_FOUND", Message: "config not found"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"ok":false,"error":{"code":"CONFIG_NOT_FOUND","message":"config not found"}}`
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

// O-5: Failure: details 付き
func TestFailure_WithDetails(t *testing.T) {
	got, err := output.Failure(&output.Error{
		Code:    "X",
		Message: "y",
		Details: map[string]any{"path": "/a"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"ok":false,"error":{"code":"X","message":"y","details":{"path":"/a"}}}`
	if string(got) != want {
		t.Errorf("got %q, want %q", string(got), want)
	}
}

// O-6: Failure: nil Error → error 戻り値が non-nil
func TestFailure_NilError(t *testing.T) {
	_, err := output.Failure(nil)
	if err == nil {
		t.Error("expected error for nil *Error, got nil")
	}
}

// O-7: Write: 末尾改行 1 つ
func TestWrite_AppendNewline(t *testing.T) {
	var buf bytes.Buffer
	if err := output.Write(&buf, []byte("{}")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := buf.String(); got != "{}\n" {
		t.Errorf("got %q, want %q", got, "{}\n")
	}
}

// O-8: Write 契約: 改行なしの payload を渡しても Write が \n を 1 つ追加する
func TestWrite_AlwaysOneNewline(t *testing.T) {
	payload := []byte(`{"ok":true,"data":{}}`)
	var buf bytes.Buffer
	if err := output.Write(&buf, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result := buf.String()
	if result[len(result)-1] != '\n' {
		t.Errorf("output must end with newline, got %q", result)
	}
	// 改行は 1 つだけ
	if bytes.Count([]byte(result), []byte{'\n'}) != 1 {
		t.Errorf("output must contain exactly one newline, got %q", result)
	}
}

// O-9: エンコード安定性: 同じ input で 2 回呼んでも同一 byte 列
func TestSuccess_Stable(t *testing.T) {
	type payload struct {
		Version string `json:"version"`
	}
	a, err := output.Success(payload{Version: "0.1.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := output.Success(payload{Version: "0.1.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("successive calls returned different bytes: %q vs %q", a, b)
	}
}

// O-10: HTML エスケープ無効化: & < > がそのまま含まれる（& 等にならない）
func TestSuccess_NoHTMLEscape(t *testing.T) {
	type payload struct {
		Q string `json:"q"`
	}
	got, err := output.Success(payload{Q: "a&b<c>"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// HTML エスケープシーケンスが含まれていないこと
	if bytes.Contains(got, []byte("\\u0026")) || bytes.Contains(got, []byte("\\u003c")) || bytes.Contains(got, []byte("\\u003e")) {
		t.Errorf("output contains HTML escape sequences: %q", string(got))
	}
	// 元の文字列がそのまま含まれること
	if !bytes.Contains(got, []byte("a&b<c>")) {
		t.Errorf("output does not contain original string: %q", string(got))
	}
}
