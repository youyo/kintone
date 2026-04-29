// Package api は kintone REST API の薄い透過層を提供する。
//
// 設計原則:
//   - kintoneapi.Client の各 GetX メソッドを 1:1 で透過する
//   - 型変換・名前解決・キャッシュは行わない（M07 cache / M08 resolver で別途実装）
//   - operations / facade / cli から **必ずこの層を経由** して kintoneapi にアクセスする
//
// 利用例:
//
//	apiClient, _ := api.NewFromKintone(kclient)
//	resp, err := apiClient.GetRecords(ctx, kintoneapi.GetRecordsRequest{App: 1})
//
// 依存方向:
//
//	cli / operations / facade  →  service/api  →  kintoneapi  →  auth
//
// この層を必ず経由することで、M07 のキャッシュ挿入時に CLI や operations を
// 修正せずに済む（service/api.Client の各メソッドを差し替えるだけで済む）。
package api
