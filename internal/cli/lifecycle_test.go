package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/youyo/kintone/internal/cli"
)

// TestExecuteWith_NoStoreOpen_ForReadOnlyCommands は read-only コマンド
// (version / completion / config) で SQLite DB ファイルが作成されないことを確認する。
// Phase 6a の不変条件: needsStore=false の経路で Storage 副作用ゼロ。
func TestExecuteWith_NoStoreOpen_ForReadOnlyCommands(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("KINTONE_STORE_BACKEND", "sqlite")
	t.Setenv("KINTONE_STORE_SQLITE_DIR", tmpDir)

	cases := [][]string{
		{"version"},
		{"version", "--short"},
		{"completion", "bash"},
		{"config", "show"},
	}
	for _, args := range cases {
		t.Run(args[0], func(t *testing.T) {
			var out, errOut bytes.Buffer
			_ = cli.ExecuteWith(args, &out, &errOut)
			// out の内容は問わない（既存テストでカバー済み）。
			// ここでは tmpDir に SQLite DB ファイルが作成されていないことを検証する。
			entries, err := os.ReadDir(tmpDir)
			if err != nil {
				t.Fatalf("ReadDir(%q): %v", tmpDir, err)
			}
			for _, e := range entries {
				name := filepath.Base(e.Name())
				t.Errorf("read-only command %v created unexpected file: %s",
					args, name)
			}
		})
	}
}

// TestExecuteWith_VersionStillWorks: 既存挙動が ExecuteWith 拡張後も維持される
// ことを smoke レベルで再確認する。
func TestExecuteWith_VersionStillWorks(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{"version"}, &out, &errOut); err != nil {
		t.Fatalf("ExecuteWith version: %v", err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"ok":true`)) {
		t.Errorf("expected ok:true, got: %q", out.String())
	}
}
