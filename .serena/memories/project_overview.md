# kintone プロジェクト概要

## 目的
kintone REST API を操作する統合ツール。CLI と MCP サーバー（remote / multi-user 対応）の両方を提供し、LLM フレンドリーな JSON 固定出力で利用しやすくする。

## 現状（2026-04-29 時点）
**計画フェーズ**。ソースコードはまだ存在しない。
- `docs/specs/kintone_spec.md` — 超詳細仕様書（確定済み）
- `plans/kintone-roadmap.md` — M01〜M11 のマイルストーン（確定済み）
- `plans/kintone-m01-project-skeleton.md` — M01 詳細計画（確定済み）
- `CLAUDE.md` — 全体ガイダンス（確定済み）
- `mise.toml` — Go 1.26 固定
- `go.mod` 未作成。M01 で `go mod init github.com/youyo/kintone` 予定

## 提供予定機能
- CLI（Cobra ベース）
- MCP サーバー（remote 対応・multi-user 対応、mark3labs/mcp-go）
- 認証: API Token / OAuth / idproxy（OIDC）
- SQLite ベースのキャッシュ / TokenStore
- LLM フレンドリーな名前解決（Resolver）

## ロードマップ要約（垂直スライス進行）
- M01: プロジェクト雛形 + JSON 出力規約 ← 着手対象
- M02: config 層（toml + env + profile）
- M03: kintoneapi クライアント + API Token 認証
- M04: service 層 + CLI api コマンド（読み系）
- M05: CLI ops コマンド（書き系・describe）
- M06: MCP サーバー雛形（mark3labs/mcp-go）+ Facade 層
- M07: SQLite キャッシュ + TokenStore
- M08: Resolver（名前解決）
- M09: OAuth 認証 + 自動更新
- M10: idproxy + multi-user MCP（remote/oidc）
- M11: completion + Docker + GoReleaser リリース
