// Package operations は LLM / CLI 向けの意味付けされた操作（オペレーション）を提供する。
//
// 設計原則:
//   - 1 オペレーションは「ユーザーがやりたいこと」の単位（複数 API 呼び出しを内包しても良い）
//   - 戻り値は LLM が消費しやすい構造を返す（snake_case JSON タグ統一・薄いマッピング）
//   - 名前解決（resolver）は M08 でこの層に挿入する
//   - キャッシュ（cache）は M07 で service/api 層に挿入する（operations は意識しない）
//
// 提供オペレーション:
//   - RecordsQuery : GET /k/v1/records.json の薄いラッパ（TotalCount を int64 に正規化）
//   - AppDescribe  : GetApp + GetFormFields の合成（LLM 向けのアプリ記述）
package operations
