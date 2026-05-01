package ops

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
	"github.com/youyo/kintone/internal/store"
)

// LoaderInput は NewAPIBuilder hook へ渡される情報。
type LoaderInput struct {
	CLI config.CLIConfig
	Ctx context.Context
}

// NewAPIBuilder は CLI コマンドが service/api.API を取得するための hook。
//
// 本番では config.Load → kintoneapi.NewFromResolved → service/api.NewFromKintone を実行する。
// テスト時は stub 実装を返すよう差し替える。
//
// 並列テスト禁止: グローバル var の差し替えは goroutine 安全でないため、
// cli/ops 配下のテストでは t.Parallel() を使わない。
var NewAPIBuilder = defaultNewAPI

// defaultNewAPI は本番用ローダー。
//
// KINTONE_STORE_CACHE_BYPASS=1 でない限り、CachingAPI で upstream をラップする。
// CachingAPI の cacheProvider は ctx に注入された Container.CacheForDecorator を使う
// （per-request lazy resolution）。
func defaultNewAPI(in LoaderInput) (serviceapi.API, error) {
	r, err := config.Load(config.LoadOptions{CLI: in.CLI})
	if err != nil {
		return nil, err
	}
	kc, err := kintoneapi.NewFromResolved(r)
	if err != nil {
		return nil, err
	}
	upstream, err := serviceapi.NewFromKintone(kc)
	if err != nil {
		return nil, err
	}
	env := store.LoadFromEnv()
	if env.CacheBypass {
		return upstream, nil
	}
	provider := newCacheProvider(in.Ctx)
	return serviceapi.NewCachingAPI(upstream, provider, r.Domain), nil
}

// newCacheProvider は ctx に注入された Container から CacheForDecorator を引く CacheProvider を返す。
func newCacheProvider(ctx context.Context) serviceapi.CacheProvider {
	return func() (store.CacheStore, error) {
		container := store.ContainerFromContext(ctx)
		if container == nil {
			return nil, nil
		}
		return container.CacheForDecorator()
	}
}

// readCLIConfig は cobra 親コマンドの PersistentFlags から CLIConfig を構築する。
func readCLIConfig(cmd *cobra.Command) config.CLIConfig {
	profile, _ := cmd.Flags().GetString("profile")
	configPath, _ := cmd.Flags().GetString("config")
	return config.CLIConfig{Profile: profile, ConfigPath: configPath}
}

// buildAPI は cobra cmd から service/api.API を構築する。
func buildAPI(cmd *cobra.Command) (serviceapi.API, error) {
	cli := readCLIConfig(cmd)
	return NewAPIBuilder(LoaderInput{CLI: cli, Ctx: cmd.Context()})
}

// newUsageError は internal/cli/clierr.NewUsageError への薄いエイリアス。
//
// cli/ops 内では新形式の usage エラーを統一して生成する。
// 配置を共通パッケージに移したことで cli/api / cli/cache 等からも同じ仕組みを利用できる
// （advisor 指摘 #3 反映）。
func newUsageError(format string, args ...any) *clierr.UsageError {
	return clierr.NewUsageError(format, args...)
}

// parseRecordJSON は単件レコード JSON 文字列を map にパースする。
// 不正 JSON は USAGE エラーで返す。
func parseRecordJSON(s string) (map[string]any, error) {
	if s == "" {
		return nil, newUsageError("--record-json must not be empty")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, newUsageError("--record-json: invalid JSON: %v", err)
	}
	return m, nil
}

// parseRecordsJSON は複数件レコード JSON 配列文字列をパースする。
// 不正 JSON / 空配列は USAGE エラーで返す。
func parseRecordsJSON(s string) ([]map[string]any, error) {
	if s == "" {
		return nil, newUsageError("--records-json must not be empty")
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil, newUsageError("--records-json: invalid JSON array: %v", err)
	}
	if len(arr) == 0 {
		return nil, newUsageError("--records-json must contain at least one record")
	}
	return arr, nil
}
