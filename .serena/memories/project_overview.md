# kintone プロジェクト概要

## 目的
kintone REST API を操作する統合ツール。CLI と MCP サーバー（remote / multi-user 対応）の両方を提供し、LLM フレンドリーな JSON 固定出力で利用しやすくする。

## 現状（2026-04-29 09:15 時点）
**M01 完了 / M02 着手前**。

- Go 1.26、module: `github.com/youyo/kintone`
- ブランチ `feat/m01-project-skeleton`（10 コミット、main 未 merge）
- 実装済み: `cmd/kintone`, `internal/cli`, `internal/output`, `.github/workflows/ci.yml`, `LICENSE` (MIT), `README.md`, `.golangci.yml`, `.gitignore`
- 動作: `kintone version` → `{"ok":true,"data":{"version":"0.1.0"}}`、`version --short` → `0.1.0`、未知コマンド → 失敗 JSON + exit 1
- テスト: output 85.0% / cli 90.9% カバレッジ、`go test -race ./...` 全 pass

進捗詳細: serena memory `progress`

## 提供予定機能
- CLI（Cobra ベース）
- MCP サーバー（remote 対応・multi-user 対応、mark3labs/mcp-go）
- 認証: API Token / OAuth / idproxy（OIDC）
- SQLite ベースのキャッシュ / TokenStore
- LLM フレンドリーな名前解決（Resolver）

## ロードマップ要約（垂直スライス進行）
- M01: プロジェクト雛形 + JSON 出力規約 ✅
- M02: config 層（toml + env + profile）← 次
- M03: kintoneapi クライアント + API Token 認証
- M04: service 層 + CLI api コマンド（読み系）
- M05: CLI ops コマンド（書き系・describe）
- M06: MCP サーバー雛形（mark3labs/mcp-go）+ Facade 層
- M07: SQLite キャッシュ + TokenStore
- M08: Resolver（名前解決）
- M09: OAuth 認証 + 自動更新
- M10: idproxy + multi-user MCP（remote/oidc）
- M11: completion + Docker + GoReleaser リリース
