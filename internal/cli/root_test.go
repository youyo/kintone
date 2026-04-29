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
