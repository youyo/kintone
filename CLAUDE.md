# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト現状

**計画フェーズ**：ソースコードはまだ存在しない。`docs/specs/kintone_spec.md`（超詳細仕様書）と `plans/kintone-roadmap.md`（M01〜M11 のマイルストーン分割）のみ確定済み。実装は M01 から順次行う。

- Go 1.26（`mise.toml` で固定。`mise install` で導入）
- `go.mod` 未作成。M01 で `go mod init github.com/youyo/kintone` 予定

## 開発ワークフロー

このリポジトリは devflow スキル群によるロードマップ駆動開発を採用する。

| 状況 | 使うスキル |
|------|-----------|
| 単一マイルストーンの詳細計画を作成 | `/devflow:plan` |
| 単一マイルストーンを実装 | `/devflow:implement` |
| 未完了マイルストーンを連続自律実行 | `/devflow:cycle` |
| ロードマップを更新・追加 | `/devflow:roadmap` |

**重要**: マイルストーンの依存関係は `plans/kintone-roadmap.md` の Progress セクションに記載。先頭から順に着手する（垂直スライス進行）。完了時は roadmap のチェックボックス `[ ]` → `[x]` を更新する。

## アーキテクチャの全体像

仕様書 `docs/specs/kintone_spec.md` で定義された層構造：

```
CLI / MCP
   ↓
facade        ← MCP 公開層（mcp/facade）
   ↓
operations    ← LLM 向け抽象化（service/operations）
   ↓
api           ← 薄い API 透過層（service/api）
   ↓
client        ← net/http 自作の REST クライアント（kintoneapi）
   ↓
auth          ← API Token / OAuth / idproxy（auth, idproxy）
   ↓
cache         ← SQLite キャッシュ・TokenStore（cache, tokenstore）
```

予定ディレクトリ：`cmd/kintone`, `internal/{cli,config,auth,idproxy,tokenstore,cache,resolver,kintoneapi,service/{api,operations},mcp/{server,facade},output}`

### 横断的な設計原則

- **JSON 固定出力**: 成功 `{"ok":true,"data":{...}}` / 失敗 `{"ok":false,"error":{"code":"...","message":"...","details":{...}}}`。`internal/output` パッケージに統一。`completion` や `version --short` など人間向けのプレーン出力は規約の例外として明示する
- **設定優先順位**: CLI フラグ > 環境変数 (`KINTONE_*`) > `~/.config/kintone/config.toml`。profile + env override 構造
- **multi-user 対応**: TokenStore のキーは `Domain + PrincipalID + AuthType`、`principal_id = provider:sub`
- **キャッシュパス**: ホスト `~/.cache/kintone/cache.db` / コンテナ `/data/kintone/cache.db`。TTL は apps/fields/resolver=1 年
- **名前解決**（Resolver）: App は `ID → code → name → partial`、Field は `code → label → partial` の順
- **MCP 認証モデル**: Auth=`none|oidc`、AuthZ=`oauth|api-token` の組み合わせ

### 採用済み技術選定

- MCP SDK: `github.com/mark3labs/mcp-go`（公式 Go MCP SDK）
- kintone REST クライアント: `net/http` 薄ラッパーを自作（外部 SDK は使わない）
- CLI フレームワーク: Cobra
- 配布: GoReleaser + GitHub Releases + Homebrew Tap + ghcr.io Docker

## 開発コマンド（M01 完了後に有効）

```bash
# テスト
go test ./...                      # 全テスト
go test ./internal/output -run T1  # 単一テスト
go test -race ./...                # レース検出

# 静的解析
go vet ./...
golangci-lint run

# ビルド・実行
go build ./...
go run ./cmd/kintone version       # JSON 出力で動作確認
```

## コーディング規約

- **TDD 必須**: Red → Green → Refactor。テストを先に書く
- **ブランチ命名**: 単一文字の前にハイフンを置かない（❌ `fix-f-encoding` / ✅ `fix-japanese-filename-encoding`）
- **コミットメッセージ**: Conventional Commits 形式・日本語（例: `feat: JSON 出力規約を実装`）
- **会話・PR 本文**: 日本語

## 重要ファイル参照

- 仕様書: `docs/specs/kintone_spec.md`
- ロードマップ: `plans/kintone-roadmap.md`
- M01 詳細計画: `plans/kintone-m01-project-skeleton.md`
- M02 以降の詳細計画は着手時に `/devflow:plan` で生成する
