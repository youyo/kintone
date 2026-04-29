# Roadmap: kintone CLI / MCP サーバー

## Meta
| 項目 | 値 |
|------|---|
| ゴール | kintone REST API を操作する統合 CLI / MCP サーバーをリリース可能な品質で提供する |
| 成功基準 | (1) CLI から API Token + OAuth で record CRUD と app_describe が実行可能 (2) MCP サーバーが remote/multi-user で動作 (3) GoReleaser/Homebrew/Docker で配布可能 (4) JSON 固定出力 (5) TDD によるユニットテスト網羅 |
| 制約 | Go 1.26 / 仕様書（docs/specs/kintone_spec.md）準拠 / multi-user 対応 / profile + env override / 配布形態 4 種 |
| 対象リポジトリ | /Users/youyo/src/github.com/youyo/kintone |
| 作成日 | 2026-04-29 |
| 最終更新 | 2026-04-29 13:40 |
| ステータス | 進行中（M05 完了） |

## Current Focus
- **マイルストーン**: M6: MCP サーバー雛形 + Facade 層
- **直近の完了**: M05 — CLI ops コマンド（write 系 + describe）（feat/m05-cli-ops-write ブランチ）
- **次のアクション**: M06 着手（`/devflow:plan` で詳細計画 → `/devflow:implement`）

## Progress

### M1: プロジェクト雛形 + JSON 出力規約 ✅ 完了
- [x] go.mod 作成（module github.com/youyo/kintone, go 1.26）
- [x] cmd/kintone/main.go（Cobra root）
- [x] internal/cli/{root.go, version.go, errors.go, *_test.go}
- [x] internal/output/{json.go, json_test.go}（成功/エラー JSON フォーマット統一）
- [x] .github/workflows/ci.yml（go test / go vet / golangci-lint）
- [x] README.md / LICENSE (MIT) / .gitignore / .golangci.yml
- [x] 実行確認: `go run ./cmd/kintone version` で `{"ok":true,"data":{"version":"0.1.0"}}` 出力
- [x] 全テスト pass（output 85.0% / cli 90.9% カバレッジ）
- 詳細: plans/kintone-m01-project-skeleton.md
- ブランチ: feat/m01-project-skeleton（main へ merge 待ち）

### M2: config 層（toml + env + profile） ✅ 完了
- [x] internal/config/{config.go, profile.go, env.go, loader.go, resolver.go, *_test.go}
- [x] ~/.config/kintone/config.toml ローダー（BurntSushi/toml v1.6.0）
- [x] 優先順位: CLI > ENV > config 実装（Resolver 単一責務）
- [x] KINTONE_PROFILE / KINTONE_CONFIG_PATH / KINTONE_CACHE_PATH / KINTONE_DOMAIN / KINTONE_AUTH / KINTONE_API_TOKEN 環境変数
- [x] CLI: `kintone config show`（mask 済み JSON）/ `kintone config init`（0o600・atomic write・--force）
- [x] PersistentFlags: `--profile` / `--config` / `--no-color`（root に登録）
- [x] errors.go 拡張: CONFIG_PROFILE_NOT_FOUND / CONFIG_PARSE_ERROR / CONFIG_ALREADY_EXISTS / CONFIG_NOT_FOUND
- [x] 全テスト pass（config 91.4% / cli 84.1% / output 85.0% カバレッジ）
- 詳細: plans/kintone-m02-config-layer.md
- ブランチ: feat/m02-config-layer（main への merge 待ち）

### M3: kintoneapi クライアント + API Token 認証 ✅ 完了
- [x] internal/auth/{apitoken.go, apitoken_test.go}（X-Cybozu-API-Token ヘッダ付与）
- [x] internal/kintoneapi/{client.go, transport.go, errors.go, *_test.go}（net/http 薄ラッパー）
- [x] エンドポイント: GET /k/v1/records.json, GET /k/v1/record.json, GET /k/v1/app.json, GET /k/v1/app/form/fields.json
- [x] レート制限・リトライ（Retry-After 対応）
- [x] httptest によるモック動作確認
- [x] internal/cli/errors.go: KINTONE_UNAUTHORIZED / FORBIDDEN / NOT_FOUND / RATE_LIMITED / VALIDATION / INTERNAL / NETWORK のマッピング追加
- [x] 全テスト pass（auth 100% / kintoneapi 86.2% / cli 87.4% カバレッジ）
- 詳細: plans/kintone-m03-kintoneapi-client.md
- ブランチ: feat/m03-kintoneapi-client（main への merge 待ち）

### M4: service 層（read 系 operations）+ CLI api コマンド ✅ 完了
- [x] internal/service/api/{api.go, doc.go, api_test.go}（薄い API 透過層、interface でモック容易化）
- [x] internal/service/operations/{records_query.go, app_describe.go, doc.go, *_test.go}（LLM 向け抽象化）
- [x] internal/cli/api/{root.go, helpers.go, records.go, record.go, app.go, *_test.go}
- [x] 既存 cli/root.go に `api` サブコマンドを統合
- [x] 動作確認: `kintone api --help` 動作 / 全テスト pass / カバレッジ達成（service/api 100% / service/operations 100% / cli/api 82%）
- 詳細: plans/kintone-m04-service-read-cli-api.md
- ブランチ: feat/m04-service-read-cli-api（main への merge 待ち）

### M5: CLI ops コマンド（write 系 + describe） ✅ 完了
- [x] kintoneapi: POST/PUT/DELETE エンドポイント追加（`InsertRecords` / `UpdateRecord` / `DeleteRecords`）+ `doJSONWithBody`（書き込み系は MaxAttempts=1 デフォルト）
- [x] service/api: interface に write 系 3 メソッドを追加（透過実装）
- [x] operations: `RecordCreate` / `RecordUpdate`（id / updateKey 排他）/ `RecordDelete`（revisions 任意）
- [x] internal/cli/ops/{root.go, helpers.go, record.go, app.go}（cobra）
- [x] バリデーション: 必須項目 / 排他フラグ（id ⊕ updateKey、record-json ⊕ records-json）
- [x] dry-run フラグ: `BuildXxxBody` で実 API 送信と byte 完全一致を担保
- [x] USAGE 分類の堅牢化: `cli/ops.UsageError` 型 sentinel + `errors.As` 分岐
- [x] 動作確認: `kintone ops record create/update/delete --dry-run` / `kintone ops app describe` 動作 / 全テスト pass / カバレッジ達成（kintoneapi 85.5% / service/api 100% / service/operations 98.8% / cli/ops 87.5% / cli/api 82.0%）
- 詳細: plans/kintone-m05-cli-ops-write.md
- ブランチ: feat/m05-cli-ops-write（main への merge 待ち）

### M6: MCP サーバー雛形 + Facade 層
- [ ] internal/mcp/server/{stdio.go, *_test.go}（mark3labs/mcp-go）
- [ ] internal/mcp/facade/{tools.go, apps_search.go, app_describe.go, records_*.go}
- [ ] CLI: `kintone mcp serve` (stdio mode)
- [ ] 6 つの MCP tools 実装: apps_search, app_describe, records_query, record_create/update/delete
- [ ] 動作確認: Claude Desktop からの接続テスト
- 詳細: 着手時生成

### M7: SQLite キャッシュ + TokenStore
- [ ] internal/cache/{sqlite.go, schema.sql, ttl.go, *_test.go}（modernc.org/sqlite または mattn/go-sqlite3）
- [ ] internal/tokenstore/{store.go, sqlite.go, *_test.go}（Get/Put/Delete, Key=Domain+PrincipalID+AuthType）
- [ ] CLI: `kintone cache clear` / `kintone cache stats`
- [ ] TTL: apps/fields/resolver=1 年
- [ ] パス: ~/.cache/kintone/cache.db (host) / /data/kintone/cache.db (container)
- 詳細: 着手時生成

### M8: Resolver（名前解決）
- [ ] internal/resolver/{app.go, field.go, *_test.go}
- [ ] App: ID → code → name → partial の順
- [ ] Field: code → label → partial の順
- [ ] キャッシュ統合
- [ ] CLI/MCP からの透過利用
- 詳細: 着手時生成

### M9: OAuth 認証 + 自動更新
- [ ] internal/auth/oauth/{flow.go, refresh.go, *_test.go}
- [ ] access_token: 1h / refresh_token: 無期限 / 自動更新あり
- [ ] Scope: record/app/file read/write
- [ ] KINTONE_OAUTH_CLIENT_ID/SECRET/REDIRECT_URL
- [ ] CLI: `kintone auth login --oauth` / `kintone auth status`
- [ ] PKCE 対応・state 検証
- 詳細: 着手時生成

### M10: idproxy + multi-user MCP（remote/oidc）
- [ ] internal/idproxy/{provider.go, oidc.go, *_test.go}
- [ ] principal_id = provider:sub
- [ ] MCP Auth: none / oidc
- [ ] MCP AuthZ: oauth / api-token
- [ ] KINTONE_MCP_AUTH_MODE / KINTONE_MCP_AUTHZ_MODE
- [ ] HTTP/SSE remote MCP サーバー
- [ ] multi-user TokenStore 連携
- 詳細: 着手時生成

### M11: completion + Docker + GoReleaser リリース
- [ ] CLI: `kintone completion {bash|zsh|fish|powershell}`
- [ ] Dockerfile（multi-stage build, alpine base）
- [ ] .goreleaser.yaml（cross-compile + GitHub Releases + Homebrew Tap + ghcr.io）
- [ ] .github/workflows/release.yml（タグプッシュで起動）
- [ ] README 完備（インストール 4 方式 / 認証 3 方式 / CLI コマンド一覧 / MCP セットアップ）
- 詳細: 着手時生成

## Blockers
なし

## Architecture Decisions
| # | 決定 | 理由 | 日付 |
|---|------|------|------|
| 1 | MCP SDK は mark3labs/mcp-go を採用 | Go エコシステムで最も使われており stdio/http 両対応・remote 実装が容易 | 2026-04-29 |
| 2 | kintone REST クライアントは net/http 薄ラッパーを自作 | 依存最小化・テスト容易・必要な API のみ型付きで実装 | 2026-04-29 |
| 3 | TDD 必須（Red → Green → Refactor） | 認証・キャッシュ・Resolver など複雑ロジックの品質担保 | 2026-04-29 |
| 4 | 垂直スライス進行（M1 ごとに動く成果物） | 大規模仕様に対し早期動作確認とフィードバック反映を優先 | 2026-04-29 |
| 5 | API Token を先行実装、OAuth/idproxy は後段 | 動作確認が早期に可能・実装難易度を平準化 | 2026-04-29 |
| 6 | キャッシュ/Resolver は M7-M8 で導入 | データ整合性問題を避けるため CLI/MCP の主要機能が動作確認後 | 2026-04-29 |

## Changelog
| 日時 | 種別 | 内容 |
|------|------|------|
| 2026-04-29 07:55 | 作成 | ロードマップ初版作成（インタビューに基づき M1-M11 を確定） |
| 2026-04-29 09:15 | 進捗 | M01 完了（feat/m01-project-skeleton ブランチで 9 コミット）。devflow:cycle の Planner→devils-advocate→advocate(2 周)→advisor()→implementer(TDD)→手動動作確認まで一気通貫で実施。Current Focus を M02 に更新 |
| 2026-04-29 09:40 | 進捗 | M02 完了（feat/m02-config-layer ブランチ）。internal/config（91.4% カバレッジ）と CLI config show/init を実装。advisor() 指摘 4 件を計画に反映後 TDD で実装、手動確認 8 件 pass。Current Focus を M03 に更新 |
| 2026-04-29 12:58 | 進捗 | M03 完了（feat/m03-kintoneapi-client ブランチ）。internal/auth/apitoken（100%）・internal/kintoneapi（86.2%）・cli エラーマッピング（87.4%）を TDD で実装。全テスト pass、golangci-lint クリーン。Current Focus を M04 に更新 |
| 2026-04-29 13:14 | 進捗 | M04 完了（feat/m04-service-read-cli-api ブランチ）。internal/service/api（100%）・internal/service/operations（100%）・internal/cli/api（82%）を TDD で実装。`kintone api {records,record,app} ...` で kintone REST を JSON で叩けるように。CLI から kintoneapi 直 import 禁止のレイヤー分離を確立。全テスト pass、M04 新規 lint 違反 0。Current Focus を M05 に更新 |
| 2026-04-29 13:40 | 進捗 | M05 完了（feat/m05-cli-ops-write ブランチ）。kintoneapi に write 系（POST/PUT/DELETE）を追加し、service/api interface 拡張、operations.{RecordCreate, RecordUpdate, RecordDelete} を実装。CLI に `ops record {create,update,delete}` と `ops app describe` を追加。`--dry-run` で送信予定 body を JSON 出力（実 API と byte 一致）、書き込み系は MaxAttempts=1 デフォルト、`UsageError` 型 sentinel で USAGE 分類を堅牢化（advisor 6 件指摘反映済）。全テスト pass、カバレッジ目標達成。Current Focus を M06 に更新 |
