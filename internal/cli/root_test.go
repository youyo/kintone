package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
)

// R-1: 未知サブコマンドでエラーが返る
func TestRootCmd_UnknownSubcommand(t *testing.T) {
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"foo"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for unknown subcommand, got nil")
	}
}

// R-2: サブコマンド無しで cobra デフォルト動作（ヘルプ表示）
func TestRootCmd_NoSubcommand(t *testing.T) {
	cmd := cli.NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Usage") {
		t.Errorf("expected help output to contain 'Usage', got: %q", out.String())
	}
}

// R-4: --profile / --config / --no-color が PersistentFlags に登録されている
func TestRootCmd_PersistentFlagsRegistered(t *testing.T) {
	cmd := cli.NewRootCmd()
	for _, name := range []string{"profile", "config", "no-color"} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("PersistentFlag %q not registered", name)
		}
	}
}

// R-5: PersistentFlags は子コマンドからも参照可能
func TestRootCmd_PersistentFlagsVisibleToSubcommands(t *testing.T) {
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"version", "--profile", "x"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// R-6: Execute() が成功時に nil を返す
func TestExecute_Success(t *testing.T) {
	// os.Args は使わず、ExecuteWith を直接呼ぶ統合確認
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"version"}, &out, &errOut)
	if err != nil {
		t.Errorf("Execute success path returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"ok":true`) {
		t.Errorf("expected ok:true in output, got: %q", out.String())
	}
}

// R-3: executeWith() 経由の失敗パス（統合テスト）
func TestExecuteWith_UnknownSubcommandJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"foo"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}

	// stdout に失敗 JSON が出力されていること
	var result struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if jsonErr := json.Unmarshal(out.Bytes(), &result); jsonErr != nil {
		t.Fatalf("failed to parse stdout as JSON: %v, output: %q", jsonErr, out.String())
	}
	if result.OK {
		t.Error("expected ok=false")
	}
	if result.Error.Code != "USAGE" {
		t.Errorf("expected error.code=USAGE, got %q", result.Error.Code)
	}
	if result.Error.Message == "" {
		t.Error("expected non-empty error.message")
	}
}
