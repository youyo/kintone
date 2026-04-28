# コード規約・スタイル

## 全般
- 言語: 日本語（会話・コミットメッセージ・PR 本文・ドキュメント）。ただしコード内の identifier は英語
- Go 標準のフォーマット: `gofmt` / `goimports`
- 静的解析: `go vet` + `golangci-lint`
- テスト: 標準 `testing` パッケージ + `httptest`（外部 mock ライブラリは原則使わない）

## TDD 必須
**Red → Green → Refactor** サイクルを厳守する。
- Red: 失敗するテストを先に書く
- Green: テストを通す最小限の実装
- Refactor: テストが通る状態でコードを整理

各マイルストーンは `_test.go` を必ず先に書く。

## ブランチ命名
- 単一文字の前にハイフンを置かない
- ❌ `fix-f-encoding`
- ✅ `fix-japanese-filename-encoding`

## コミットメッセージ
- Conventional Commits 形式・**日本語**
- 例:
  - `feat: JSON 出力規約を実装`
  - `fix: API Token 認証ヘッダの誤りを修正`
  - `chore: golangci-lint 設定を追加`
- 種別: feat / fix / chore / docs / refactor / test / build / ci

## 設計原則（CLAUDE.md より）
- **Simplicity First**: 変更を可能な限りシンプルに、影響範囲を最小化
- **No Laziness**: 根本原因を追求し、一時的な修正はしない
- **Minimal Impact**: 必要なコードのみを変更
- **Scope Management**: 機能クリープを防ぐ。既存インターフェースは制約として保護

## パッケージ構造規約
- `cmd/kintone/main.go` は最小: `cli.Execute()` 呼び出しのみ
- `internal/` 配下は他リポジトリから import 禁止（Go 標準の internal セマンティクス）
- パッケージ名は単数形・小文字
- テストファイルは同パッケージ（`package foo` + `foo_test.go`）

## エラーハンドリング
- 構造化エラー型 `output.Error{Code, Message, Details}` を JSON 出力経路で使用
- 内部では `errors.Is` / `errors.As` を活用
- `panic` は禁止（main の最上位以外）

## セキュリティ
- シークレット（API Token / OAuth Client Secret）はハードコード禁止
- TokenStore に保存し環境変数経由で読み込む
- XSS / SQLi / コマンドインジェクション等を作り込まない

## ドキュメント更新
- 実装と同時に `README.md` / `docs/` を更新する
- 新しい CLI コマンド / 環境変数追加時は必ず文書化
