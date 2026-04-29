# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト現状

**全 11 マイルストーン完了済み**。リリース準備完了（タグ push で GoReleaser ワークフロー起動）。

- Go 1.26、module: `github.com/youyo/kintone`
- 動作する CLI: `version` / `config show|init` / `api {records,record,app} ...` / `ops {record create|update|delete, app describe}` / `mcp serve`（M06、M10 で HTTP/Streamable + OIDC + multi-user）/ `cache clear|stats`（M07）/ 全コマンドに `--app-ref` / `--update-key-field-ref` で名前解決（M08）/ `auth login --oauth` / `auth status` / `auth logout`（M09）/ **`completion {bash|zsh|fish|powershell}`（M11）**
- 実装済みパッケージ:
  - `internal/output` — JSON 出力規約（CLI / MCP 共通）
  - `internal/cli` + `internal/cli/api` + `internal/cli/ops` + `internal/cli/mcp` + `internal/cli/clierr`（共通 UsageError）+ `internal/cli/cache`（M07）+ **`internal/cli/auth`（M09）**
  - `internal/config` — CLI > ENV > toml の優先解決（`KINTONE_OAUTH_*` 環境変数追加 M09）
  - `internal/auth/apitoken` — `X-Cybozu-API-Token` ヘッダ付与
  - **`internal/auth/oauth`** — OAuth 2.0 Authorization Code + PKCE フロー / loopback callback サーバ / refresh_token 自動更新 / Authenticator（M09）
  - `internal/kintoneapi` — net/http 薄ラッパー REST クライアント + **`NewFromResolvedWithAuth`**（M09）
  - `internal/service/api` — `kintoneapi` の薄い透過層 + `CachingAPI` decorator（M07）
  - `internal/service/operations` — LLM 向け抽象化（M08 で `*Ref` フィールド + `resolver` 引数追加）
  - `internal/mcp/server` — mark3labs/mcp-go v0.49.0 の薄いラッパー（stdio + **HTTP/Streamable transport** M10 / Auth/AuthZ モード判定 M10）
  - **`internal/idproxy`** — `github.com/youyo/idproxy` v0.4.2 の thin wrapper（OIDC リクエスト認証 + `Principal` context 注入、M10）
  - **`internal/service/api/PrincipalAPIFactory`** — per-request にユーザー別 TokenStore からトークンを引いて API を構築（M10）
  - `internal/mcp/facade` — 6 つの MCP tools ハンドラと `MapError`
  - `internal/cache` — SQLite ベースのキャッシュ層（modernc.org/sqlite v1.50.0 / TTL 管理）（M07）
  - `internal/tokenstore` — OAuth アクセストークン保存（Key=Domain+PrincipalID+AuthType）（M07 / M09 で本格利用）
  - `internal/resolver` — App / Field 名前解決（M08）
- 依存方向: `cli/{api,ops,mcp} → service/api(CachingAPI) → kintoneapi → auth` / `cli/auth → auth/oauth → tokenstore` / **`cli/auth/helpers → oauth.Authenticator → kintoneapi`（OAuth 用 NewFromResolvedWithAuth 経由）**（M09）
- **設計原則**: CLI / MCP から `internal/kintoneapi` 直 import 禁止。必ず `service/api` または `service/operations` を経由する
- **設計判断（M05）**:
  - `clierr.UsageError` 型 sentinel + `MapToOutputError` `errors.As` 分岐で USAGE 分類を堅牢化（文字列 prefix 依存を排除）。配置は中立パッケージ `internal/cli/clierr` で循環なし
  - `--dry-run` フラグで送信予定 body を JSON 出力（実 API 送信時と byte 完全一致を担保するため、`kintoneapi.BuildXxxBody` を共通化。テストで equivalence を検証）
  - 書き込み系（POST/PUT/DELETE）は `doJSONWithBody` 内部で `MaxAttempts=1` 強制（多重作成リスク回避）
- **設計判断（M06）**:
  - MCP の出力は CLI と同じ `output.Success` / `output.Failure` envelope を `CallToolResult.Content[0].Text` に格納する。CLI と MCP で JSON 契約を共有
  - `facade.MapError` は `errors.Is`（operations の Err\*）→ `errors.As`（`*kintoneapi.APIError`、`*url.Error`）→ context error の優先順で分類し、`INVALID_PARAMS` / `KINTONE_*` / `KINTONE_NETWORK` / `INTERNAL` に振り分ける
  - dry-run は MCP には露出しない（LLM ツール選択のセマンティクスに不適）
  - `internal/cli/mcp/helpers.go` は `cli/api` と同型の `NewAPIBuilder` hook を持ち、テストで stub を注入可能（並列テスト禁止）
- **設計判断（M07）**:
  - `CachingAPI` は `service/api.API` interface の decorator パターン。upstream を wrap し、`GetApp` / `GetAppFormFields` / `ListApps` にキャッシュ TTL（1 年）を適用
  - `KINTONE_CACHE_DISABLE=1` で CachingAPI をスキップし、upstream を直接使用する
  - `cache.OpenIfExists` で DB ファイル不在時は auto-create しない（`cache clear/stats` サブコマンドは `cache.Store` がなければ空統計を返す）
  - キャッシュパス: ホスト `~/.cache/kintone/cache.db` / コンテナ `KINTONE_CACHE_PATH` 環境変数で上書き
  - `tokenstore` は `cache` パッケージとは独立した DB ファイル（将来の multi-user OAuth 対応のため `TokenStore` を分離）
- **設計判断（M08）**:
  - `internal/resolver` は stateless な struct（`api` 依存のみ）。専用キャッシュは持たず、CachingAPI 経由で apps/fields のキャッシュを共有（依存最小化）
  - operations 層に **ハイブリッド**フィールド `AppRef string` / `UpdateKeyFieldRef string` を追加。既存 `App int64` 直指定経路は **完全後方互換**（テスト・スクリプト無修正）
  - operations 関数のシグネチャに `r *resolver.Resolver` 引数を追加（nil 許容）。AppRef / UpdateKeyFieldRef を使わない経路は r=nil で動作
  - CLI: `--app-ref` / `--update-key-field-ref` フラグを追加。`MarkFlagRequired("app")` を全箇所で外し、RunE 内で「どちらか必須・両方排他」を `clierr.NewUsageError` で検証
  - MCP: `app: required` を **外し**、`app_ref: string` を追加（既存クライアントは `app: number` を引き続き送れば動作。新規クライアントは `app_ref` を使える）
  - 解決順序: App `ID 直接（id>0 のみ採用）→ code 完全一致 → name 完全一致 → name 部分一致`、Field `code 完全一致 → label 完全一致 → label 部分一致`
  - 各段階でヒットしたら即 return（fallback しない / predictability 優先）
  - エラーコード: `RESOLVER_APP_NOT_FOUND` / `RESOLVER_APP_AMBIGUOUS` / `RESOLVER_FIELD_NOT_FOUND` / `RESOLVER_FIELD_AMBIGUOUS` / `RESOLVER_APP_LIST_TOO_LARGE`。`details.candidates` に候補配列を含める（CLI / facade 双方ミラー）
  - apps_search ページング: 最大 100 ページ × 100 件 = 10,000 アプリで打ち切り（`ErrAppListTooLarge`）
- **設計判断（M09）**:
  - `kintoneapi.NewFromResolvedWithAuth` を新設。`NewFromResolved` のシグネチャと既存テストを変更せず OAuth 用 Authenticator を外注できる設計
  - `internal/cli/auth/helpers.go` が TokenStore + Refresher + Authenticator の構築を担い、`kintoneapi` パッケージは `auth/oauth` / `tokenstore` を知らない（依存方向維持）
  - PKCE (S256) と state は `crypto/rand` で生成。callback の state 検証は `subtle.ConstantTimeCompare` でタイミング攻撃対策
  - loopback サーバは `net.Listen("tcp", "127.0.0.1:<port>")` で bind を loopback 限定。`sync.Once` で callback を 1 度のみ受理
  - `--principal-id` を CLI フラグで必須化（M10 OIDC 対応まで自動取得なし）。形式: `oauth:<任意文字列>` を推奨
  - `auth status` の access_token は先頭 4 + `...` + 末尾 4 にマスク。`config show` の `oauth_client_secret` は `***` にマスク
  - `KINTONE_OAUTH_PKCE_DISABLE=1` で PKCE 無効化（kintone OAuth が PKCE を拒否した場合の escape hatch、M11 本番確認予定）
- **設計判断（M11）**:
  - `internal/cli/completion` を独立パッケージで配置。`NewCmd(root)` がルートコマンドを受け取り cobra の `GenXxxCompletion` を呼ぶ
  - completion 出力は **JSON envelope の例外**として明示（プレーンスクリプトを stdout に書く）。`version --short` と同列
  - `internal/mcp/facade.ToolDeps` に `APIResolver` interface 型 `Factory` フィールドを追加（オプショナル）。`Factory != nil` で per-request の per-user API 解決、`Factory == nil` で従来の `deps.API` を使う後方互換
  - `service/api.PrincipalAPIFactory` は `ForContext(ctx)` メソッドで `APIResolver` を満たすため、上位レイヤから直接代入可能
  - `facade.MapError` は `errors.Is(err, serviceapi.ErrAuthRequired)` を最優先で判定し `AUTH_REQUIRED` envelope を返す
  - `kintoneapi/transport.go` の `defer resp.Body.Close()` を IIFE 形式 `defer func() { _ = resp.Body.Close() }()` に書き換え、errcheck 違反を解消
  - 配布: GoReleaser v2 系で linux/{amd64,arm64} + darwin/{amd64,arm64} + windows/amd64 を cross build。Homebrew Tap (`youyo/homebrew-tap`) と ghcr.io multi-arch への自動配布
  - Dockerfile: `golang:1.26-alpine` → `gcr.io/distroless/static-debian12:nonroot`（uid 65532）。CGO_ENABLED=0（modernc.org/sqlite は pure Go）
- `go test -race -cover ./...` 全 22 パッケージ pass、`golangci-lint run` 違反 0（既存 transport.go errcheck 2 件も解消済み）
- ブランチ: `feat/m11-completion-docker-release`（main への merge 待ち）

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
- M06 詳細計画: `plans/kintone-m06-mcp-server-facade.md`
- M07 詳細計画: `plans/kintone-m07-sqlite-cache-tokenstore.md`
- M08 詳細計画: `plans/kintone-m08-resolver.md`
- M09 詳細計画: `plans/kintone-m09-oauth-auth.md`
- M10 詳細計画: `plans/kintone-m10-idproxy-multiuser-mcp.md`
- M11 以降の詳細計画は着手時に `/devflow:plan` で生成する
