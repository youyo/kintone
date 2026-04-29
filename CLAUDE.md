# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト現状

**M05 完了済み**。M06（MCP サーバー雛形 + Facade 層）が次のマイルストーン。

- Go 1.26、module: `github.com/youyo/kintone`
- 動作する CLI: `version` / `config show|init` / `api {records,record,app} ...` / **`ops {record create|update|delete, app describe}`**（M05）
- 実装済みパッケージ:
  - `internal/output` — JSON 出力規約
  - `internal/cli` + `internal/cli/api` + **`internal/cli/ops`** + **`internal/cli/clierr`**（共通 UsageError） — cobra コマンドツリー
  - `internal/config` — CLI > ENV > toml の優先解決
  - `internal/auth/apitoken` — `X-Cybozu-API-Token` ヘッダ付与
  - `internal/kintoneapi` — net/http 薄ラッパー REST クライアント（read 系 + write 系 POST/PUT/DELETE 完備、書き込みは MaxAttempts=1 デフォルト）
  - `internal/service/api` — `kintoneapi` の薄い透過層（interface でモック容易化、M07 cache 挿入点）
  - `internal/service/operations` — LLM 向け抽象化: `RecordsQuery` / `AppDescribe` / **`RecordCreate` / `RecordUpdate` / `RecordDelete`**
- 依存方向: `cli/{api,ops} → service/operations → service/api → kintoneapi → auth`
- **設計原則**: CLI から `internal/kintoneapi` 直 import 禁止。必ず `service/api` または `service/operations` を経由する
- **設計判断（M05）**:
  - `clierr.UsageError` 型 sentinel + `MapToOutputError` `errors.As` 分岐で USAGE 分類を堅牢化（文字列 prefix 依存を排除）。配置は中立パッケージ `internal/cli/clierr` で循環なし
  - `--dry-run` フラグで送信予定 body を JSON 出力（実 API 送信時と byte 完全一致を担保するため、`kintoneapi.BuildXxxBody` を共通化。テストで equivalence を検証）
  - 書き込み系（POST/PUT/DELETE）は `doJSONWithBody` 内部で `MaxAttempts=1` 強制（多重作成リスク回避）
- `go test -race -cover ./...` 全 pass（service/api 100% / service/operations 98.8% / cli/ops 87.5% / cli/api 82.0% / cli 87.2% / config 92.7% / kintoneapi 85.5% / auth 100% / output 85.0%）
- ブランチ: `feat/m05-cli-ops-write`（main への merge 待ち）

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
- M02 詳細計画: `plans/kintone-m02-config-layer.md`
- M03 詳細計画: `plans/kintone-m03-kintoneapi-client.md`
- M04 詳細計画: `plans/kintone-m04-service-read-cli-api.md`
- M05 詳細計画: `plans/kintone-m05-cli-ops-write.md`
- M06 以降の詳細計画は着手時に `/devflow:plan` で生成する
