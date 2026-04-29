// Package mcp は kintone CLI の `mcp` サブコマンドツリーを提供する。
//
//	kintone mcp serve   stdio MCP サーバーを起動
//
// 設計判断:
//   - kintoneapi を直接 import せず、必ず service/api を経由する
//   - テスト hook（NewAPIBuilder）でグローバル var を差し替え可能（並列テスト禁止）
package mcp

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/youyo/kintone/internal/cache"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// LoaderInput は NewAPIBuilder hook へ渡される情報。
type LoaderInput struct {
	CLI config.CLIConfig
}

// NewAPIBuilder は CLI コマンドが service/api.API を取得するための hook。
//
// 本番では config.Load → kintoneapi.NewFromResolved → service/api.NewFromKintone を実行する。
// テスト時は stub 実装を返すよう差し替える。
//
// 並列テスト禁止: グローバル var の差し替えは goroutine 安全でないため、
// cli/mcp 配下のテストでは t.Parallel() を使わない。
var NewAPIBuilder = defaultNewAPI

// defaultNewAPI は本番用ローダー。
// CLIConfig → config.Load → kintoneapi.NewFromResolved → service/api.NewFromKintone。
// KINTONE_CACHE_DISABLE=1 でない限り、CachingAPI で upstream をラップする。
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
	if os.Getenv("KINTONE_CACHE_DISABLE") == "1" {
		return upstream, nil
	}
	cachePath, err := cache.DefaultCachePath(nil, nil)
	if err != nil {
		return upstream, nil
	}
	store, err := cache.Open(cachePath)
	if err != nil {
		return upstream, nil
	}
	return serviceapi.NewCachingAPI(upstream, store, r.Domain), nil
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
	return NewAPIBuilder(LoaderInput{CLI: cli})
}
