package memory

import (
	"time"

	"github.com/youyo/kintone/internal/store"
)

// DefaultCleanupInterval は memory backend の TTL クリーンアップ周期の既定値。
// テストなど意図的に短い周期で動かしたい場合は New(interval) を直接使う。
const DefaultCleanupInterval = 5 * time.Minute

// Open は store.Opener として呼び出される memory backend のエントリーポイント。
// cfg の memory 固有パラメータは現状なし（cleanup 周期は固定）。
func Open(_ *store.Config) (store.Container, error) {
	return New(DefaultCleanupInterval), nil
}

func init() {
	store.RegisterOpener(store.BackendMemory, Open)
}
