package auth

import (
	"fmt"
	"os"

	"github.com/youyo/kintone/internal/cache"
	"github.com/youyo/kintone/internal/tokenstore"
)

// openTokenStore は tokens.db を開く。
// テスト時は openTokenStoreFn を差し替えることで SQLite への依存を回避できる。
// IMPORTANT: 並列テストで同じパスを共有しないよう t.TempDir() を使うこと。
var openTokenStoreFn = defaultOpenTokenStore

// defaultOpenTokenStore は本番用の tokenstore.Open を呼ぶ。
func defaultOpenTokenStore() (tokenstore.Store, error) {
	tokPath, err := cache.DefaultTokensPath(os.Getenv, os.UserHomeDir)
	if err != nil {
		return nil, fmt.Errorf("auth: resolve tokens path: %w", err)
	}
	store, err := tokenstore.Open(tokPath)
	if err != nil {
		return nil, fmt.Errorf("auth: open tokenstore: %w", err)
	}
	return store, nil
}

// SetOpenTokenStoreFn は openTokenStoreFn を差し替える（テスト専用）。
// IMPORTANT: 並列テストで同じパスを共有しないよう t.TempDir() ベースの Store を使うこと。
func SetOpenTokenStoreFn(fn func() (tokenstore.Store, error)) {
	openTokenStoreFn = fn
}

// ResetOpenTokenStoreFn は openTokenStoreFn をデフォルトに戻す（テスト専用）。
func ResetOpenTokenStoreFn() {
	openTokenStoreFn = defaultOpenTokenStore
}
