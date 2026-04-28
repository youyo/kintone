# タスク完了時のチェックリスト

実装タスク（マイルストーン or サブタスク）を完了する前に必ず以下を実行する。

## 1. テスト
```bash
go test ./...                      # 全テスト pass
go test -race ./...                # レース検出 pass
```
- TDD サイクル（Red → Green → Refactor）を完遂しているか
- 新規追加機能の `_test.go` が存在するか
- カバレッジが極端に下がっていないか

## 2. 静的解析
```bash
go vet ./...
gofmt -l . | grep -v vendor        # 出力なしであること
golangci-lint run
```

## 3. ビルド確認
```bash
go build ./...                     # ビルドエラーなし
go run ./cmd/kintone version       # 動作確認（M01 以降）
```

## 4. ドキュメント更新
- 新規 CLI コマンド/フラグ追加 → `README.md` 反映
- 新規環境変数追加 → `README.md` + `docs/specs/kintone_spec.md` 反映
- アーキテクチャ変更 → `CLAUDE.md` の関連節を更新
- 関連マイルストーン詳細計画ファイルのチェックボックスを `[x]` に更新

## 5. ロードマップ更新
- マイルストーン完了時は `plans/kintone-roadmap.md` の以下を更新:
  - Progress セクションのチェックボックス `[ ]` → `[x]`
  - Current Focus（次のマイルストーン名・直近の完了・次のアクション）
  - 最終更新日時
  - Changelog（日付・種別・内容、**理由も書く**）

## 6. JSON 出力規約の遵守確認
- 成功: `{"ok":true,"data":{...}}`
- 失敗: `{"ok":false,"error":{"code":"...","message":"...","details":{...}}}`
- 規約から外れる場合は明示的なフラグ + ドキュメント記載

## 7. Git
- ブランチ名規則確認（単一文字の前にハイフン禁止）
- コミットメッセージ: Conventional Commits 日本語
- `git add <files>` で明示指定（`-A` や `.` を使わない）
- シークレットを含めていないか確認（.env, credentials.json 等）
- pre-commit hook が落ちたら **新規コミットで修正**（amend は使わない）
- `--no-verify` は使わない

## 8. セルフレビュー
- 「シニア開発者の基準でこの差分を承認するか？」
- Simplicity First / No Laziness / Minimal Impact / Scope Management に反していないか
- 一時的な修正・ハック的な迂回がないか（根本原因にアプローチしているか）

## 9. devflow:cycle 内での挙動
- Phase 4 の implementer が `git commit` を実行する（push は不要）
- code-reviewer の指摘事項に対応
- CYCLE_RESULT.handoff に次マイルストーンが必要な情報を構造化して残す
