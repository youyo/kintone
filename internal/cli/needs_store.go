package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/store"
)

// needsStore は cmd / store env から Container を Open すべきか判定する単一関数。
//
// 計画 §Section 3 の決定マトリクス全行に対応する：
//   - read-only コマンド (version/completion/config/help/store init) → false
//   - auth/cache/mcp serve など Container が mandatory → true
//   - api/ops は api-token + cache_bypass=1 のファストパスのみ false、それ以外は true
//   - その他は false（保守的に）
//
// auth mode 解決は Phase 6c で完全対応。Phase 6a 時点では KINTONE_AUTH 環境変数のみを
// 参照する簡易判定（CLI flag / config.toml の解決は呼び出し側で完了済みであることを前提）。
func needsStore(cmd *cobra.Command, env *store.Config) bool {
	if isStoreSkippedCommand(cmd) {
		return false
	}
	if isStoreRequiredCommand(cmd) {
		return true
	}
	// api/ops 経路: api-token + cache_bypass=1 はファストパス（完全 stateless）
	if isAPIOrOpsCommand(cmd) {
		if env != nil && env.CacheBypass && isAPITokenAuth() {
			return false
		}
		return true
	}
	// それ以外は保守的に false
	return false
}

// isStoreSkippedCommand は store を絶対 Open しないコマンドを判定する。
// version / completion / config show|init / store init / help が対象。
func isStoreSkippedCommand(cmd *cobra.Command) bool {
	name := commandPath(cmd)
	prefixes := []string{
		"kintone version",
		"kintone completion",
		"kintone config",
		"kintone help",
		"kintone store init", // store init dynamodb 等のブートストラップ用途
	}
	for _, p := range prefixes {
		if hasPathPrefix(name, p) {
			return true
		}
	}
	return false
}

// isStoreRequiredCommand は store が mandatory なコマンドを判定する。
// auth status/logout, cache stats/clear, mcp serve が対象。
func isStoreRequiredCommand(cmd *cobra.Command) bool {
	name := commandPath(cmd)
	prefixes := []string{
		"kintone auth",
		"kintone cache",
		"kintone mcp serve",
	}
	for _, p := range prefixes {
		if hasPathPrefix(name, p) {
			return true
		}
	}
	return false
}

// isAPIOrOpsCommand は api / ops 系サブコマンドを判定する。
func isAPIOrOpsCommand(cmd *cobra.Command) bool {
	name := commandPath(cmd)
	return hasPathPrefix(name, "kintone api") || hasPathPrefix(name, "kintone ops")
}

// isAPITokenAuth は KINTONE_AUTH 環境変数の値から auth=api-token を判定する。
// 未設定（空文字）はデフォルトの api-token として扱う（M02 既定挙動と整合）。
// CLI flag / config.toml の解決は Phase 6c で完全対応。
func isAPITokenAuth() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("KINTONE_AUTH")))
	return v == "" || v == "api-token"
}

// commandPath は cmd.CommandPath() のラッパ。テスト互換のため切り出し。
func commandPath(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	return cmd.CommandPath()
}

// hasPathPrefix は CommandPath の prefix マッチを行う。
// 完全一致 もしくは prefix の直後がスペース（次トークン境界）の場合に true。
func hasPathPrefix(name, prefix string) bool {
	if name == prefix {
		return true
	}
	if len(name) > len(prefix) && strings.HasPrefix(name, prefix) && name[len(prefix)] == ' ' {
		return true
	}
	return false
}
