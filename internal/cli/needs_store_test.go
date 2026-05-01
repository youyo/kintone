package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/store"
)

// makeCmdAtPath は path（例: "kintone version"）に等しい CommandPath を持つ
// cobra.Command を組み立てる小ヘルパ。root の Use="kintone" 配下にサブコマンドを連結する。
func makeCmdAtPath(t *testing.T, path string) *cobra.Command {
	t.Helper()
	parts := splitPath(path)
	if len(parts) == 0 || parts[0] != "kintone" {
		t.Fatalf("makeCmdAtPath: invalid path %q", path)
	}
	root := &cobra.Command{Use: "kintone"}
	cur := root
	for _, p := range parts[1:] {
		next := &cobra.Command{Use: p}
		cur.AddCommand(next)
		cur = next
	}
	return cur
}

func splitPath(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func TestIsStoreSkippedCommand(t *testing.T) {
	cases := []struct {
		path    string
		skipped bool
	}{
		{"kintone version", true},
		{"kintone completion", true},
		{"kintone completion bash", true},
		{"kintone config", true},
		{"kintone config show", true},
		{"kintone config init", true},
		{"kintone help", true},
		{"kintone store init", true},
		{"kintone store init dynamodb", true},
		// store 必要系
		{"kintone auth", false},
		{"kintone auth login", false},
		{"kintone cache", false},
		{"kintone cache stats", false},
		{"kintone mcp serve", false},
		{"kintone api records", false},
		{"kintone ops record create", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			c := makeCmdAtPath(t, tc.path)
			if got := isStoreSkippedCommand(c); got != tc.skipped {
				t.Errorf("isStoreSkippedCommand(%q) = %v, want %v", tc.path, got, tc.skipped)
			}
		})
	}
}

func TestIsStoreRequiredCommand(t *testing.T) {
	cases := []struct {
		path     string
		required bool
	}{
		{"kintone auth", true},
		{"kintone auth login", true},
		{"kintone auth status", true},
		{"kintone auth logout", true},
		{"kintone cache", true},
		{"kintone cache stats", true},
		{"kintone cache clear", true},
		{"kintone mcp serve", true},
		// 違うもの
		{"kintone version", false},
		{"kintone api records", false},
		{"kintone ops record create", false},
		{"kintone completion", false},
		{"kintone config show", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			c := makeCmdAtPath(t, tc.path)
			if got := isStoreRequiredCommand(c); got != tc.required {
				t.Errorf("isStoreRequiredCommand(%q) = %v, want %v", tc.path, got, tc.required)
			}
		})
	}
}

func TestIsAPIOrOpsCommand(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"kintone api", true},
		{"kintone api records", true},
		{"kintone api record get", true},
		{"kintone ops", true},
		{"kintone ops record create", true},
		// 違うもの
		{"kintone version", false},
		{"kintone auth", false},
		{"kintone mcp serve", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			c := makeCmdAtPath(t, tc.path)
			if got := isAPIOrOpsCommand(c); got != tc.want {
				t.Errorf("isAPIOrOpsCommand(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// TestNeedsStore_Matrix は計画 §Section 3 の決定マトリクス全行をカバーする。
func TestNeedsStore_Matrix(t *testing.T) {
	cases := []struct {
		name        string
		path        string
		auth        string // KINTONE_AUTH
		cacheBypass bool
		want        bool
	}{
		// read-only コマンド: auth/cache_bypass によらず false
		{"version", "kintone version", "", false, false},
		{"version+api-token+bypass", "kintone version", "api-token", true, false},
		{"completion", "kintone completion bash", "", false, false},
		{"config show", "kintone config show", "", false, false},
		{"config init", "kintone config init", "", false, false},
		{"store init", "kintone store init dynamodb", "", false, false},
		{"help", "kintone help", "", false, false},

		// 必須コマンド
		{"auth login", "kintone auth login", "", false, true},
		{"auth status", "kintone auth status", "", false, true},
		{"auth logout", "kintone auth logout", "", false, true},
		{"cache stats", "kintone cache stats", "", false, true},
		{"cache clear", "kintone cache clear", "", false, true},
		{"mcp serve", "kintone mcp serve", "", false, true},

		// api/ops: api-token + cache_bypass=1 はファストパス（false）
		{"api+api-token+bypass", "kintone api records", "api-token", true, false},
		{"ops+api-token+bypass", "kintone ops record create", "api-token", true, false},
		// api/ops: api-token + cache_bypass=0 → true（cache が必要）
		{"api+api-token+nobypass", "kintone api records", "api-token", false, true},
		{"ops+api-token+nobypass", "kintone ops record create", "api-token", false, true},
		// api/ops: oauth → 常に true（TokenStore + cache）
		{"api+oauth+bypass", "kintone api records", "oauth", true, true},
		{"api+oauth+nobypass", "kintone api records", "oauth", false, true},
		{"ops+oauth+nobypass", "kintone ops record create", "oauth", false, true},
		// api/ops: auth 未設定はデフォルト（api-token として扱う）+ bypass で false
		{"api+empty+bypass", "kintone api records", "", true, false},
		// api/ops: auth 未設定 + bypass=0 → true
		{"api+empty+nobypass", "kintone api records", "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KINTONE_AUTH", tc.auth)
			c := makeCmdAtPath(t, tc.path)
			env := &store.Config{CacheBypass: tc.cacheBypass}
			if got := needsStore(c, env); got != tc.want {
				t.Errorf("needsStore(%q, auth=%q, bypass=%v) = %v, want %v",
					tc.path, tc.auth, tc.cacheBypass, got, tc.want)
			}
		})
	}
}
