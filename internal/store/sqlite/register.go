package sqlite

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/youyo/kintone/internal/store"
)

// Open は store.Opener として呼び出される SQLite backend のエントリーポイント。
//
// cfg.SQLiteDir が空のときは `~/.local/state/kintone/` を既定として採用する。
// dir 内に kintone.db / idproxy.db を作成する。
func Open(cfg *store.Config) (store.Container, error) {
	dir := cfg.SQLiteDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("store/sqlite: resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".local", "state", "kintone")
	}
	return NewContainer(dir)
}

func init() {
	store.RegisterOpener(store.BackendSQLite, Open)
}
