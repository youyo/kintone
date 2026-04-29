// Package ops は kintone CLI の `ops` サブコマンドツリーを提供する。
//
// LLM 向けの意味付け書き込み + describe コマンド群:
//
//	kintone ops record   create    レコード新規登録（複数件可）
//	kintone ops record   update    レコード単件更新（id / updateKey）
//	kintone ops record   delete    レコード複数件削除
//	kintone ops app      describe  app + fields 合成（M04 と同 operations を呼ぶ）
//
// 設計判断:
//   - kintoneapi を直接 import せず、必ず service/api または service/operations を経由する
//   - テスト hook（NewAPIBuilder）でグローバル var を差し替え可能（並列テストは禁止）
//   - cli/api 配下とは独立した hook を持つ（同名・同シグネチャ）。両者の独立を保つ
//
// USAGE エラー戦略（advisor 指摘 #1, #3）:
//   - 入力ミスを表す型付き sentinel `*clierr.UsageError`（internal/cli/clierr）を使う。
//   - internal/cli/errors.go の MapToOutputError が errors.As でこの型を検出し、
//     output.Error{Code:"USAGE"} に変換する。
//   - 文字列 prefix マッチ（isUsageError）に依存せず、堅牢な分類を実現する。
//   - 配置: cli ←→ cli/ops の循環を避けるため、共通パッケージ internal/cli/clierr に
//     置いて両方から参照できるようにしている。
package ops

import "github.com/spf13/cobra"

// NewCmd は `kintone ops` サブコマンドツリーを構築する。
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ops",
		Short: "kintone レコードの CRUD と app 記述（LLM 向け抽象化）",
		Long: `LLM / スクリプトから JSON 入出力で意味付け操作を実行するコマンド群です。

サブコマンド:
  record  create    レコード新規登録（複数件可・dry-run 対応）
  record  update    レコード単件更新（id / updateKey 排他）
  record  delete    レコード複数件削除（dry-run 対応）
  app     describe  app + fields 合成（M04 の api app describe と同等）

書き込み系（create/update/delete）は --dry-run で送信予定 body を JSON 出力できます。`,
	}
	cmd.AddCommand(newRecordCmd())
	cmd.AddCommand(newAppCmd())
	return cmd
}
