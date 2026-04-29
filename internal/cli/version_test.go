package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
)

// V-1: version サブコマンドが JSON 出力を返す
func TestVersionCmd_JSON(t *testing.T) {
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"version"}, &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"ok":true,"data":{"version":"0.1.0"}}` + "\n"
	if got := out.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// V-2: version JSON 構造妥当性
func TestVersionCmd_JSONStructure(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"version"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result struct {
		OK   bool                       `json:"ok"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json.Unmarshal failed: %v, output: %q", err, out.String())
	}
	if !result.OK {
		t.Error("expected ok=true")
	}
	// data のキー集合が {"version"} のみであること
	if len(result.Data) != 1 {
		t.Errorf("expected data to have exactly 1 key, got %d: %v", len(result.Data), result.Data)
	}
	versionVal, ok := result.Data["version"]
	if !ok {
		t.Fatal("expected data.version to exist")
	}
	var version string
	if err := json.Unmarshal(versionVal, &version); err != nil {
		t.Fatalf("failed to unmarshal version: %v", err)
	}
	if version != "0.1.0" {
		t.Errorf("expected version=0.1.0, got %q", version)
	}
	// commit と date のキーが含まれないこと
	for _, key := range []string{"commit", "date"} {
		if _, exists := result.Data[key]; exists {
			t.Errorf("unexpected key %q in data", key)
		}
	}
}

// V-3: version --short がプレーン出力を返す
func TestVersionCmd_Short(t *testing.T) {
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"version", "--short"}, &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "0.1.0\n"
	if got := out.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// V-4: version --help が cobra ヘルプを返す
func TestVersionCmd_Help(t *testing.T) {
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"version", "--help"}, &out, &errOut)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("expected help output to contain 'Usage:', got: %q", out.String())
	}
}
