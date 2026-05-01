// Package auth は kintone CLI の認証情報管理コマンド（auth status/logout）を提供する。
//
// M12 Phase 6b では旧 internal/tokenstore に依存していた経路を internal/store の
// 統合 Container 経由に切り替えた。Container の取得順序は次の通り:
//
//  1. ctx に注入された Container（cli.ExecuteWith が PersistentPreRunE で設定）
//  2. テスト用 hook openStoreFn（SetOpenStoreFn で差し替え）
//  3. store.OpenFromEnv()（lib として直接呼ぶフォールバック）
package auth

import (
	"context"
	"fmt"

	"github.com/youyo/kintone/internal/store"
)

// openStoreFn は Container を Open するテスト用 hook。
//
// 本番経路では cli.ExecuteWith が事前に Container を ctx へ注入するため
// この hook は呼ばれない。SetOpenStoreFn でのみ test 経路で差し替えられる。
//
// IMPORTANT: 並列テストで状態を共有しないよう、t.TempDir() ベースの sqlite backend
// または KINTONE_STORE_BACKEND=memory を使うこと。
var openStoreFn = defaultOpenStore

// defaultOpenStore は本番フォールバック用の Container ビルダ。
func defaultOpenStore() (store.Container, error) {
	c, err := store.OpenFromEnv()
	if err != nil {
		return nil, fmt.Errorf("auth: open store: %w", err)
	}
	return c, nil
}

// SetOpenStoreFn は openStoreFn を差し替える（テスト専用）。
//
// 戻り値の restore を defer で呼ぶことで元の hook に戻せる。
// IMPORTANT: 並列テストで同じパスを共有しないよう、t.TempDir() ベースの
// Container を返す mock を使うこと。
func SetOpenStoreFn(fn func() (store.Container, error)) (restore func()) {
	prev := openStoreFn
	openStoreFn = fn
	return func() { openStoreFn = prev }
}

// ResetOpenStoreFn は openStoreFn をデフォルトに戻す（テスト専用）。
func ResetOpenStoreFn() {
	openStoreFn = defaultOpenStore
}

// getTokenStore は Container > hook > direct open の順序で TokenStore を取得する。
//
// 戻り値の cleanup は呼び出し側で必ず実行する。
//
// ライフサイクル:
//   - context 経由: Container は cli.ExecuteWith が close するため cleanup は no-op
//   - hook 経由（test 経路）: Container のライフサイクルは test 側（t.Cleanup）に委ねるため
//     cleanup は no-op。これにより同一 Container を複数 sub-test で再利用できる
//   - env 直接呼び出し（lib 経路）: 呼び出し側に閉じ責任があるため cleanup が Close する
func getTokenStore(ctx context.Context) (ts store.TokenStore, cleanup func(), err error) {
	if c := store.ContainerFromContext(ctx); c != nil {
		ts, err := c.Tokens()
		if err != nil {
			return nil, nil, fmt.Errorf("auth: tokens: %w", err)
		}
		return ts, func() {}, nil
	}
	// hook が差し替えられている場合は test 経路として扱い、cleanup を no-op にする。
	// 差し替えがない（=defaultOpenStore）場合のみ env 直接経路として cleanup で Close する。
	hookOverridden := !isDefaultOpenStore()
	c, err := openStoreFn()
	if err != nil {
		return nil, nil, err
	}
	ts2, err := c.Tokens()
	if err != nil {
		if !hookOverridden {
			_ = c.Close(ctx)
		}
		return nil, nil, fmt.Errorf("auth: tokens: %w", err)
	}
	if hookOverridden {
		return ts2, func() {}, nil
	}
	return ts2, func() { _ = c.Close(ctx) }, nil
}

// isDefaultOpenStore は openStoreFn が defaultOpenStore のままか判定する。
// SetOpenStoreFn による差し替えを検知するための単純な関数ポインタ比較。
func isDefaultOpenStore() bool {
	// reflect は不要。fmt.Sprintf("%p", ...) で関数ポインタの一意性を比較する。
	return fmt.Sprintf("%p", openStoreFn) == fmt.Sprintf("%p", defaultOpenStore)
}
