package redis

import "github.com/youyo/kintone/internal/store"

// init は store.RegisterOpener で Redis backend を登録する。
// 利用側は `_ "github.com/youyo/kintone/internal/store/redis"` の blank import で有効化する。
func init() {
	store.RegisterOpener(store.BackendRedis, Open)
}
