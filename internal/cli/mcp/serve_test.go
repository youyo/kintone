package mcp_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	climcp "github.com/youyo/kintone/internal/cli/mcp"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// CM-1: NewCmd で mcp + serve サブコマンドが返る
func TestMcpCmd_Structure(t *testing.T) {
	cmd := climcp.NewCmd()
	if cmd.Use != "mcp" {
		t.Errorf("Use=%q", cmd.Use)
	}
	if !hasSubcommand(cmd, "serve") {
		t.Errorf("missing serve subcommand")
	}
}

func hasSubcommand(cmd *cobra.Command, name string) bool {
	for _, c := range cmd.Commands() {
		if c.Use == name || strings.HasPrefix(c.Use, name+" ") {
			return true
		}
	}
	return false
}

// CM-2: --help は exit 0 で description 出力
func TestServeCmd_Help(t *testing.T) {
	cmd := climcp.NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"serve", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(buf.String(), "stdio") {
		t.Errorf("help output: %s", buf.String())
	}
}

// CM-3: NewAPIBuilder hook で stub が注入できる
func TestNewAPIBuilder_Hook(t *testing.T) {
	// stub 実装で API を返す（実 API は呼ばない）
	called := false
	old := climcp.NewAPIBuilder
	climcp.NewAPIBuilder = func(in climcp.LoaderInput) (serviceapi.API, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { climcp.NewAPIBuilder = old })

	// hook 経由で呼び出されるか直接確認
	_, _ = climcp.NewAPIBuilder(climcp.LoaderInput{})
	if !called {
		t.Error("hook not called")
	}
}

// CM-4: serve RunE で API ローダーが失敗するとエラーが伝播する（stdio を起動しない）
func TestServeCmd_BuildAPIError(t *testing.T) {
	wantErr := errStub("loader failed")
	old := climcp.NewAPIBuilder
	climcp.NewAPIBuilder = func(in climcp.LoaderInput) (serviceapi.API, error) {
		return nil, wantErr
	}
	t.Cleanup(func() { climcp.NewAPIBuilder = old })

	cmd := climcp.NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"serve"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "loader failed") {
		t.Fatalf("err=%v", err)
	}
}

// errStub は RunE のエラーパスを通すための簡易 error 型。
type errStub string

func (e errStub) Error() string { return string(e) }
