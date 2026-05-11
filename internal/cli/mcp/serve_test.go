package mcp_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/youyo/kintone/internal/cli/clierr"
	climcp "github.com/youyo/kintone/internal/cli/mcp"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// isolateMCPEnv は CI / 開発者シェルの MCP 関連環境変数を空に固定し、テスト間の
// 副作用を防ぐ。M15 で新規追加するテストの前提条件。
func isolateMCPEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"KINTONE_MCP_LISTEN_ADDR",
		"KINTONE_MCP_AUTH_MODE",
		"KINTONE_MCP_AUTHZ_MODE",
		"KINTONE_OAUTH_CLIENT_ID",
		"KINTONE_OAUTH_CLIENT_SECRET",
		"KINTONE_OAUTH_REDIRECT_URL",
		"KINTONE_MCP_EXTERNAL_URL",
	} {
		t.Setenv(k, "")
	}
}

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

// M15-1: stdio + authz=oauth は起動時に clierr.UsageError で fail-fast し、
// buildAPI は呼ばれないことを確認する。
func TestServeCmd_StdioOAuth_RejectedAsUsageError(t *testing.T) {
	isolateMCPEnv(t)

	called := false
	old := climcp.NewAPIBuilder
	climcp.NewAPIBuilder = func(in climcp.LoaderInput) (serviceapi.API, error) {
		called = true
		return nil, errStub("must not be called")
	}
	t.Cleanup(func() { climcp.NewAPIBuilder = old })

	cmd := climcp.NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"serve", "--authz=oauth"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected UsageError, got nil")
	}
	var ue *clierr.UsageError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *clierr.UsageError, got %T: %v", err, err)
	}
	if !strings.Contains(ue.Error(), "stdio") || !strings.Contains(ue.Error(), "HTTP") {
		t.Errorf("UsageError message should reference stdio and HTTP recovery hint, got: %q", ue.Error())
	}
	if !strings.Contains(ue.Error(), "--listen") {
		t.Errorf("UsageError message should mention --listen recovery flag, got: %q", ue.Error())
	}
	if called {
		t.Error("buildAPI must NOT be called for stdio+oauth (fail-fast before buildAPI)")
	}
}

// M15-2: HTTP + authz=oauth では buildAPI が skip され、OAuth setup の env 検証で
// 失敗する経路に到達することを確認する。
func TestServeCmd_HTTPOAuth_SkipsBuildAPI(t *testing.T) {
	isolateMCPEnv(t)

	called := false
	old := climcp.NewAPIBuilder
	climcp.NewAPIBuilder = func(in climcp.LoaderInput) (serviceapi.API, error) {
		called = true
		return nil, errStub("buildAPI must not be called for HTTP+OAuth")
	}
	t.Cleanup(func() { climcp.NewAPIBuilder = old })

	cmd := climcp.NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"serve", "--listen=127.0.0.1:0", "--auth=oidc", "--authz=oauth"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error from OAuth setup (missing env), got nil")
	}
	if called {
		t.Errorf("buildAPI must NOT be called for HTTP+OAuth, but was called (err=%v)", err)
	}
	// OAuth env 検証 or auth=oidc env 検証に到達することを確認（buildAPI 由来でないこと）
	msg := err.Error()
	if !strings.Contains(msg, "OAUTH") && !strings.Contains(msg, "OIDC") && !strings.Contains(msg, "Storage") && !strings.Contains(msg, "principal") {
		t.Errorf("error should originate from OAuth/OIDC setup, got: %q", msg)
	}
}

// M15-3: HTTP + authz=api-token は従来通り buildAPI を呼ぶ（後方互換）。
func TestServeCmd_HTTPAPIToken_CallsBuildAPI(t *testing.T) {
	isolateMCPEnv(t)

	called := false
	old := climcp.NewAPIBuilder
	climcp.NewAPIBuilder = func(in climcp.LoaderInput) (serviceapi.API, error) {
		called = true
		return nil, errStub("loader-error-marker")
	}
	t.Cleanup(func() { climcp.NewAPIBuilder = old })

	cmd := climcp.NewCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"serve", "--listen=127.0.0.1:0"})

	err := cmd.Execute()
	if !called {
		t.Error("buildAPI must be called for HTTP+api-token (backward compatibility)")
	}
	if err == nil || !strings.Contains(err.Error(), "loader-error-marker") {
		t.Fatalf("expected stub error from buildAPI, got %v", err)
	}
}
