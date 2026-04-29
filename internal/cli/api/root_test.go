// Package api_test は cli/api パッケージの cobra コマンドツリーをテストする。
//
// 並列実行ポリシー: cli/api 配下のテストは t.Parallel() を使わない。
// グローバル var (newAPI) を差し替えるパターンが goroutine 安全でないため、
// サブテスト並列化は行わない。
package api_test

import (
	"testing"

	"github.com/spf13/cobra"
	cliapi "github.com/youyo/kintone/internal/cli/api"
)

// AR-1: NewCmd 構築とサブコマンド登録
func TestNewCmd_HasSubcommands(t *testing.T) {
	cmd := cliapi.NewCmd()
	if cmd == nil {
		t.Fatal("NewCmd returned nil")
	}
	if cmd.Use != "api" {
		t.Errorf("Use=%q want api", cmd.Use)
	}

	want := map[string]bool{"records": false, "record": false, "app": false}
	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("subcommand %q not registered", name)
		}
	}
}

// AR-2: 各サブコマンドのフラグ登録
func TestNewCmd_FlagsRegistered(t *testing.T) {
	cmd := cliapi.NewCmd()
	cases := []struct {
		path  []string
		flags []string
	}{
		{[]string{"records", "get"}, []string{"app", "query", "field", "total-count"}},
		{[]string{"record", "get"}, []string{"app", "id"}},
		{[]string{"app", "get"}, []string{"app"}},
		{[]string{"app", "fields"}, []string{"app", "lang"}},
		{[]string{"app", "describe"}, []string{"app", "lang"}},
	}
	for _, c := range cases {
		sub := findCmd(t, cmd, c.path)
		for _, f := range c.flags {
			if sub.Flags().Lookup(f) == nil {
				t.Errorf("%v: flag --%s not registered", c.path, f)
			}
		}
	}
}

func findCmd(t *testing.T, root *cobra.Command, path []string) *cobra.Command {
	t.Helper()
	cur := root
	for _, p := range path {
		var next *cobra.Command
		for _, sub := range cur.Commands() {
			if sub.Name() == p {
				next = sub
				break
			}
		}
		if next == nil {
			t.Fatalf("subcommand %q not found under %q", p, cur.Name())
		}
		cur = next
	}
	return cur
}
