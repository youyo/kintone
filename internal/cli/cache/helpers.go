// Package cache は kintone CLI の `cache` サブコマンドツリーを提供する。
//
//	kintone cache stats    キャッシュ統計を JSON で出力
//	kintone cache clear    キャッシュを削除
//
// 設計方針:
//   - KINTONE_CACHE_DISABLE env は無視する（advisor 加筆 B: cache サブコマンドの目的が Store 操作そのもの）
//   - DB ファイルが存在しない場合は auto-create しない（advisor 指摘 #5）
//   - OpenIfExists を使い、不在時は合成した Stats を返す
package cache

import (
	"github.com/youyo/kintone/internal/cache"
)

// NewStoreBuilder はテスト hook。本番では DefaultCachePath + OpenIfExists を使う。
//
// 戻り値: (store, exists, error)
// store == nil かつ exists == false かつ error == nil はファイル不在を意味する。
var NewStoreBuilder = defaultNewStoreBuilder

func defaultNewStoreBuilder() (cache.Store, bool, error) {
	path, err := cache.DefaultCachePath(nil, nil)
	if err != nil {
		return nil, false, err
	}
	s, exists, err := cache.OpenIfExists(path)
	if err != nil {
		return nil, false, err
	}
	return s, exists, nil
}

// cachePath は stats の db_path フィールドに使う（DB 不在時でも表示する）。
func cachePath() string {
	p, _ := cache.DefaultCachePath(nil, nil)
	return p
}
