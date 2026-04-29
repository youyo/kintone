package resolver

import (
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// maxAppPagesForResolver は ListApps（name 部分一致）の最大ページ数。
//
// 1 ページ 100 件 × 100 ページ = 10,000 アプリで打ち切る。
// resolver 用途で 10,000 件を超える場合は ref の絞り込みが弱すぎると判断し、
// ErrAppListTooLarge で LLM に「絞り込みを強くしてください」と伝える。
const maxAppPagesForResolver = 100

// Resolver は kintone の app / field 名前解決を行う。
//
// 依存する service/api.API は CachingAPI でラップされた状態で渡されることを想定する。
// 1 度引いた ListApps / GetFormFields は CachingAPI 側の SQLite キャッシュ（TTL=1 年）で
// 再利用されるため、Resolver 内部に専用キャッシュは持たない（依存最小化 / M08 計画 #5）。
//
// stateless なため複数 goroutine から安全に共有可能。
type Resolver struct {
	api serviceapi.API
}

// New は Resolver を構築する。api は CachingAPI でラップされていることが望ましい。
func New(api serviceapi.API) *Resolver {
	return &Resolver{api: api}
}
