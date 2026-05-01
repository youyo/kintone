// Package cache は kintone CLI の `cache` サブコマンドツリーを提供する。
//
//	kintone cache stats    キャッシュ統計を JSON で出力（backend 中立 schema）
//	kintone cache clear    キャッシュエントリを削除（--scope または --key）
//
// 設計方針 (M12 Phase 6b):
//   - Storage backend は internal/store 経由で抽象化。memory / sqlite / redis / dynamodb
//     のいずれでも同一 schema で動作する。
//   - Container 取得順序: ctx 経由 > hook > store.OpenFromEnv() フォールバック
//   - 旧 KINTONE_CACHE_DISABLE / KINTONE_CACHE_PATH は internal/store の env に統合済み
//   - Stats は backend 中立 schema (`backend` / `location` / `reachable` / `entry_count`
//     / `expired_count` / `backend_specific`)。旧 db_path / db_exists / total は撤廃。
package cache

import (
	"context"
	"fmt"

	"github.com/youyo/kintone/internal/store"
)

// NewContainerBuilder はテスト hook。本番では store.OpenFromEnv を使う。
//
// SetNewContainerBuilder で差し替え可能。差し替えた場合は test 経路として扱い、
// 戻り値の Container は呼び出し側（test）が Close する責任を負う。
var NewContainerBuilder = defaultNewContainerBuilder

func defaultNewContainerBuilder() (store.Container, error) {
	c, err := store.OpenFromEnv()
	if err != nil {
		return nil, fmt.Errorf("cache: open store: %w", err)
	}
	return c, nil
}

// SetNewContainerBuilder は NewContainerBuilder を差し替える（テスト専用）。
// 戻り値の restore を defer で呼ぶことで元の hook に戻せる。
func SetNewContainerBuilder(fn func() (store.Container, error)) (restore func()) {
	prev := NewContainerBuilder
	NewContainerBuilder = fn
	return func() { NewContainerBuilder = prev }
}

// getContainer は Container を取得する（ctx > hook > env の優先順）。
//
// cleanup ライフサイクル:
//   - ctx 経由: Container は cli.ExecuteWith が close するため cleanup は no-op
//   - hook 経由（test 経路）: test 側に Close 責任、cleanup は no-op
//   - env 経由（lib 直接呼び出し）: cleanup が Close する
func getContainer(ctx context.Context) (c store.Container, cleanup func(), err error) {
	if cc := store.ContainerFromContext(ctx); cc != nil {
		return cc, func() {}, nil
	}
	hookOverridden := !isDefaultBuilder()
	cc, err := NewContainerBuilder()
	if err != nil {
		return nil, nil, err
	}
	if hookOverridden {
		return cc, func() {}, nil
	}
	return cc, func() { _ = cc.Close(ctx) }, nil
}

// isDefaultBuilder は NewContainerBuilder が defaultNewContainerBuilder のまま判定する。
func isDefaultBuilder() bool {
	return fmt.Sprintf("%p", NewContainerBuilder) == fmt.Sprintf("%p", defaultNewContainerBuilder)
}
