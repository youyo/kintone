package dynamodb

import "github.com/youyo/kintone/internal/store"

// init は store.RegisterOpener で dynamodb backend を登録する。
// Phase 5 完了後、CLI 起動時に internal/store/dynamodb を blank import すれば
// `KINTONE_STORE_BACKEND=dynamodb` で本 backend を選択できる。
func init() {
	store.RegisterOpener(store.BackendDynamoDB, Open)
}
