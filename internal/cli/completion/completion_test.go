package completion_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	clicompletion "github.com/youyo/kintone/internal/cli/completion"
)

// newRootStub は GenXxxCompletion を呼び出せる最小の root を返す。
func newRootStub() *cobra.Command {
	root := &cobra.Command{Use: "kintone"}
	root.AddCommand(&cobra.Command{Use: "version", Run: func(*cobra.Command, []string) {}})
	return root
}

func runCompletion(t *testing.T, shell string) string {
	t.Helper()
	root := newRootStub()
	root.AddCommand(clicompletion.NewCmd(root))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"completion", shell})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute %s: %v", shell, err)
	}
	return buf.String()
}

// C1: bash
func TestCompletion_Bash(t *testing.T) {
	out := runCompletion(t, "bash")
	if !strings.Contains(out, "bash completion") && !strings.Contains(out, "__start_kintone") {
		t.Errorf("expected bash completion script, got: %q", truncate(out))
	}
}

// C2: zsh
func TestCompletion_Zsh(t *testing.T) {
	out := runCompletion(t, "zsh")
	if !strings.Contains(out, "compdef") {
		t.Errorf("expected compdef in zsh script, got: %q", truncate(out))
	}
}

// C3: fish
func TestCompletion_Fish(t *testing.T) {
	out := runCompletion(t, "fish")
	if !strings.Contains(out, "complete -c kintone") {
		t.Errorf("expected fish complete -c kintone, got: %q", truncate(out))
	}
}

// C4: powershell
func TestCompletion_PowerShell(t *testing.T) {
	out := runCompletion(t, "powershell")
	if !strings.Contains(out, "Register-ArgumentCompleter") {
		t.Errorf("expected Register-ArgumentCompleter, got: %q", truncate(out))
	}
}

// C5: 不正引数
func TestCompletion_InvalidShell(t *testing.T) {
	root := newRootStub()
	root.AddCommand(clicompletion.NewCmd(root))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"completion", "invalid-shell"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for invalid shell")
	}
}

// C6: 引数なし
func TestCompletion_NoArg(t *testing.T) {
	root := newRootStub()
	root.AddCommand(clicompletion.NewCmd(root))
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"completion"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when shell arg missing")
	}
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
