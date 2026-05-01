package sqlite

import (
	"fmt"

	idproxy "github.com/youyo/idproxy"
	idproxysqlite "github.com/youyo/idproxy/store/sqlite"
)

// newIDProxyStore は idproxy が要求する Store interface を満たす SQLite 実装を返す。
//
// idproxy v0.4.2 の `store/sqlite.New(path)` を thin wrap。kintone.db とは別ファイル
// (idproxy.db) を使うのは、idproxy の WAL ライフサイクル / 鍵スキーマと kintone データを
// 衝突させないため。
func newIDProxyStore(path string) (idproxy.Store, error) {
	s, err := idproxysqlite.New(path)
	if err != nil {
		return nil, fmt.Errorf("store/sqlite: idproxy open %s: %w", path, err)
	}
	return s, nil
}
