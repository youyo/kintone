# 進捗状況

最終更新: 2026-04-29 09:15

## ロードマップ進捗

| マイルストーン | ステータス | ブランチ / 詳細 |
|--------------|-----------|----------------|
| M01: プロジェクト雛形 + JSON 出力規約 | ✅ 完了 | `feat/m01-project-skeleton`（main 未 merge） |
| M02: config 層（toml + env + profile） | 🟡 次 | 詳細計画未作成（着手時 `/devflow:plan`） |
| M03: kintoneapi クライアント + API Token 認証 | ⏸ 未着手 | — |
| M04: service 層 + CLI api コマンド | ⏸ 未着手 | — |
| M05: CLI ops コマンド | ⏸ 未着手 | — |
| M06: MCP サーバー雛形 + Facade 層 | ⏸ 未着手 | — |
| M07: SQLite キャッシュ + TokenStore | ⏸ 未着手 | — |
| M08: Resolver（名前解決） | ⏸ 未着手 | — |
| M09: OAuth 認証 + 自動更新 | ⏸ 未着手 | — |
| M10: idproxy + multi-user MCP | ⏸ 未着手 | — |
| M11: completion + Docker + GoReleaser | ⏸ 未着手 | — |

## M01 成果物（feat/m01-project-skeleton, 10 コミット）

```
8478c72 ci: GitHub Actions workflow を追加し M01 完了をロードマップに反映
9d1d93c chore: golangci-lint 設定を追加し lint エラーを修正
ff9dde4 docs: README と CLAUDE.md を更新し LICENSE (MIT) を追加
66fe980 feat: cmd/kintone エントリポイントを追加
e48171d feat(cli): cobra root と version コマンドを実装
69c52c6 test(cli): cobra root / version / errors のテストを追加（V-1〜V-4 / R-1〜R-3 / E-1〜E-3）
854ecf6 chore: cobra v1.8.1 依存を追加
bb4047a feat(output): JSON 出力規約パッケージを実装
47c1cd9 test(output): JSON 出力規約パッケージのテストを追加（O-1〜O-10）
1fb34ef chore: go.mod を初期化し .gitignore を追加
```

### 実装したパッケージ

- `internal/output/`: `Success(data any) ([]byte, error)`, `Failure(*Error) ([]byte, error)`, `Write(io.Writer, []byte) error`, `Error{Code, Message, Details}`. HTML エスケープ無効化 / 末尾改行統一 / フィールド順 ok→data/error 保証。
- `internal/cli/`: `NewRootCmd()`, `Execute()`, `executeWith(args, out, errOut)`, `newVersionCmd()`, `MapToOutputError(err)`, `Version`/`Commit`/`Date` var, `versionPayload struct{Version,Commit,Date}` (omitempty)。
- `cmd/kintone/main.go`: `cli.Execute()` のみの最小化。

### 検証結果（実測）

- `go test -race -cover ./...` ✅ 全 pass
  - `internal/output` カバレッジ 85.0%
  - `internal/cli` カバレッジ 90.9%
- `go vet ./...` ✅ クリーン
- `gofmt -l .` ✅ 差分なし
- `go build ./...` ✅ 成功
- `kintone version` → `{"ok":true,"data":{"version":"0.1.0"}}`
- `kintone version --short` → `0.1.0`
- `kintone foo` → 失敗 JSON + exit 1
- `kintone --help` → cobra ヘルプ

## 残タスク（M01 周辺）

- [ ] Phase 4.2 code-reviewer によるレビュー（任意・main merge 前推奨）
- [ ] `feat/m01-project-skeleton` を main に merge（`git merge --no-ff feat/m01-project-skeleton`）

## devflow:cycle 実行で得られた知見

- **Agent ネスト不可**: サブエージェントから更に Agent tool で spawn できない。`devflow:cycle` の milestone-executor 設計はオーケストレーター（Claude Code 本体）が直接各専門エージェント（planning-agent / devils-advocate / advocate / implementer / code-reviewer）を呼ぶ形でしか成立しない。
- **バックグラウンド agent の Bash 権限プロンプト**: UI に表示されないため詰む。foreground 実行（`run_in_background: false`）が必須。
- **planning-agent は Edit/Write 権限なし**: 計画書本体の保存はオーケストレーターまたは light-implementer が行う必要あり。

## 次回再開コマンド

- M02 着手: `/devflow:plan`（詳細計画作成）→ `/devflow:implement` または `/devflow:cycle`
- M01 を main に merge してから進めるのが推奨
