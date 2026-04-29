package oauth_test

import (
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/auth/oauth"
)

// BR-1: DefaultOpenBrowser は OS 別の exec.Command を構築する（darwin/linux/windows で命令切替）。
// 実際にブラウザを起動しないよう、hook 経由でコマンドをモックする。
func TestDefaultOpenBrowser_CommandBuilt(t *testing.T) {
	t.Parallel()
	var gotCmd string
	var gotArgs []string

	// commandFactory フィールドを使って exec.Command をモック
	openBrowserFn := oauth.NewOpenBrowserWithCommand(func(name string, args ...string) error {
		gotCmd = name
		gotArgs = args
		return nil
	})

	err := openBrowserFn("https://example.com/oauth2/authorization?client_id=test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// OS によりコマンドが異なるが、何らかのコマンドが使われることを確認
	if gotCmd == "" {
		t.Error("expected a command to be built, got empty string")
	}
	// URL が引数として渡されること
	found := false
	for _, arg := range gotArgs {
		if arg == "https://example.com/oauth2/authorization?client_id=test" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("URL not found in args: %v", gotArgs)
	}
}

// BR-2: exec エラー → 戻り値 error
func TestDefaultOpenBrowser_ExecError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("exec failed")
	openBrowserFn := oauth.NewOpenBrowserWithCommand(func(_ string, _ ...string) error {
		return wantErr
	})
	err := openBrowserFn("https://example.com")
	if err == nil {
		t.Error("expected error, got nil")
	}
}
