# Roadmap: kintone CLI / MCP サーバー

## Meta
| 項目 | 値 |
|------|---|
| ゴール | kintone REST API を操作する統合 CLI / MCP サーバーをリリース可能な品質で提供する |
| 成功基準 | (1) CLI から API Token + OAuth で record CRUD と app_describe が実行可能 (2) MCP サーバーが remote/multi-user で動作 (3) GoReleaser/Homebrew/Docker で配布可能 (4) JSON 固定出力 (5) TDD によるユニットテスト網羅 |
| 制約 | Go 1.26 / 仕様書（docs/specs/kintone_spec.md）準拠 / multi-user 対応 / profile + env override / 配布形態 4 種 |
| 対象リポジトリ | /Users/youyo/src/github.com/youyo/kintone |
| 作成日 | 2026-04-29 |
| 最終更新 | 2026-05-01 09:50 |
| ステータス | 進行中（M11 完了 / M12 Phase 0-1 進行中） |

## Current Focus
- **マイルストーン**: M12（統合 Storage バックエンド）Phase 0-1 完了
- **直近の完了**: M12 Phase 0（事実確認 + 決定ドキュメント 3 本）+ Phase 1（internal/store 骨格 + memory backend + Container/Factory + slog 基盤）
- **次のアクション**: Phase 2（SQLite backend）→ Phase 3-9 を順次実施。詳細は `plans/binary-imagining-lemur.md` を参照

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

### M6: MCP サーバー雛形 + Facade 層 ✅ 完了
- [x] internal/kintoneapi/apps.go（GET /k/v1/apps.json = ListApps を新規追加）
- [x] internal/service/api: API interface に ListApps を拡張
- [x] internal/mcp/server/{server.go, server_test.go}（mark3labs/mcp-go v0.49.0 stdio）
- [x] internal/mcp/facade/{facade.go, errors.go, result.go, apps_search.go, app_describe.go, records_query.go, record_create.go, record_update.go, record_delete.go, *_test.go}
- [x] CLI: `kintone mcp serve` (stdio mode) + helpers (NewAPIBuilder hook)
- [x] 6 つの MCP tools 実装: apps_search, app_describe, records_query, record_create/update/delete
- [x] facade.MapError で operations.Err\* / kintoneapi.APIError / network エラーを output.Error コードへマップ
- [x] in-process client で 6 tools 全て往復テスト（panic 防止 + envelope 検証）
- [x] 全テスト pass（mcp/facade 80.3% / mcp/server 75.0% / cli/mcp 57.1% カバレッジ）
- [x] golangci-lint run 新規違反 0、go vet クリア、gofmt クリア
- 詳細: plans/kintone-m06-mcp-server-facade.md
- ブランチ: feat/m06-mcp-server-facade（main への merge 待ち）

### M7: SQLite キャッシュ + TokenStore ✅ 完了
- [x] internal/cache/{path.go, sqlite.go, ttl.go, store.go, schema.sql, *_test.go}（modernc.org/sqlite v1.50.0）
- [x] internal/tokenstore/{store.go, sqlite.go, schema.sql, *_test.go}（Get/Put/Delete, Key=Domain+PrincipalID+AuthType）
- [x] CLI: `kintone cache clear` / `kintone cache stats`（internal/cli/cache）
- [x] TTL: apps/fields=1 年（TTL 管理 + 自動 expired 削除）
- [x] パス: `~/.cache/kintone/cache.db` (host) / `KINTONE_CACHE_PATH` 環境変数で上書き
- [x] `CachingAPI` decorator（service/api）: `GetApp` / `GetAppFormFields` / `ListApps` をキャッシュ
- [x] `KINTONE_CACHE_DISABLE=1` でキャッシュ無効化（cli/{api,ops,mcp}/helpers.go に注入）
- [x] 全テスト pass（cache 76.2% / cli/cache 73.6% / tokenstore 79.0% / service/api 88.9%）
- [x] golangci-lint run 新規違反 0、go vet クリア、gofmt クリア
- 詳細: plans/kintone-m07-sqlite-cache-tokenstore.md
- ブランチ: feat/m07-sqlite-cache-tokenstore（main への merge 待ち）

### M8: Resolver（名前解決） ✅ 完了
- [x] internal/resolver/{resolver.go, app.go, field.go, errors.go, *_test.go}（resolver パッケージ実装、coverage 97.8%）
- [x] App: ID 直接 → code 完全一致 → name 完全一致 → name 部分一致 の順
- [x] Field: code 完全一致 → label 完全一致 → label 部分一致 の順
- [x] キャッシュ統合（CachingAPI 経由で apps/fields=1 年 TTL）
- [x] operations 層に AppRef / UpdateKeyFieldRef ハイブリッド追加（後方互換維持）
- [x] CLI に `--app-ref` / `--update-key-field-ref` フラグを全コマンドで追加
- [x] MCP 全 tools に `app_ref` / `update_key_field_ref` を追加（`app: required` を外す）
- [x] エラーコード: `RESOLVER_APP_NOT_FOUND` / `RESOLVER_APP_AMBIGUOUS` / `RESOLVER_FIELD_NOT_FOUND` / `RESOLVER_FIELD_AMBIGUOUS` / `RESOLVER_APP_LIST_TOO_LARGE`、`details.candidates` に候補配列を含める
- [x] cli/errors.go と facade/errors.go の両方に Resolver エラーマッピング追加（CLI=USAGE、MCP=INVALID_PARAMS）
- [x] 全テスト pass（resolver 97.8% / operations 98.2% / cli 85.2% / cli/api 72.1% / cli/ops 70.9% / facade 79.6%）
- [x] golangci-lint クリア（既存 transport.go 2 件は M11 polish 対象として残存）
- 詳細: plans/kintone-m08-resolver.md
- ブランチ: feat/m08-resolver（main への merge 待ち）

### M9: OAuth 認証 + 自動更新 ✅ 完了
- [x] internal/auth/oauth/{pkce.go, state.go, token.go, callback.go, flow.go, refresh.go, provider.go, browser.go, errors.go, *_test.go}
- [x] access_token 自動更新（skew 60s / sync.Mutex による並行制御）
- [x] Scope: k:app_record:read/write / k:app_settings:read/write / k:file:read/write（6 個確定）
- [x] KINTONE_OAUTH_CLIENT_ID/SECRET/REDIRECT_URL/SCOPES 環境変数
- [x] CLI: `kintone auth login --oauth --principal-id <id>` / `kintone auth status` / `kintone auth logout`
- [x] PKCE S256 (crypto/rand) + state 検証 (subtle.ConstantTimeCompare)
- [x] loopback callback サーバ（sync.Once + graceful shutdown）
- [x] kintoneapi.NewFromResolvedWithAuth を新設（依存方向維持）
- [x] TokenStore 統合（M07 既存基盤を本格利用）
- [x] go test -race -cover ./... 全 pass（auth/oauth 88.3% / cli/auth 74.1%）
- [x] golangci-lint クリーン（既存 transport.go 2 件は M11 polish 対象として残存）
- 詳細: plans/kintone-m09-oauth-auth.md
- ブランチ: feat/m09-oauth-auth（main への merge 待ち）

### M10: idproxy + multi-user MCP（remote/oidc）
- [x] internal/idproxy/{config.go, principal.go, middleware.go, *_test.go}（idproxy v0.4.2 の thin wrapper）
- [x] principal_id = provider:sub（`User.Issuer + ":" + User.Subject`）
- [x] MCP Auth: none / oidc（`server.ParseAuthMode` + `ValidateModes`）
- [x] MCP AuthZ: oauth / api-token（`server.ParseAuthZMode`）
- [x] KINTONE_MCP_AUTH_MODE / KINTONE_MCP_AUTHZ_MODE / KINTONE_MCP_LISTEN_ADDR
- [x] HTTP/Streamable remote MCP サーバー（`mcp/server/http.go`、SSE は M11+）
- [x] multi-user TokenStore 連携（`service/api/principal.go` の `PrincipalAPIFactory`）
- [x] 既存 stdio + auth=none + authz=api-token は完全後方互換
- [x] go test -race ./... 全 21 パッケージ pass
- [x] 新規 lint 違反 0（既存 transport.go errcheck 2 件は M11 polish 対象として継続）
- 詳細: plans/kintone-m10-idproxy-multiuser-mcp.md
- ブランチ: feat/m10-idproxy-multiuser-mcp（main への merge 待ち）

### M11: completion + Docker + GoReleaser リリース ✅ 完了
- [x] CLI: `kintone completion {bash|zsh|fish|powershell}`（cobra Gen*Completion 経由 / JSON envelope の例外として明示）
- [x] Dockerfile（multi-stage build, golang:1.26-alpine → distroless/static:nonroot, CGO_ENABLED=0）+ .dockerignore
- [x] .goreleaser.yaml（linux/{amd64,arm64} + darwin/{amd64,arm64} + windows/amd64 + Homebrew Tap + ghcr.io multi-arch manifest）
- [x] .github/workflows/release.yml（タグ push → goreleaser/goreleaser-action@v6 / actionlint クリア）
- [x] README 完備（インストール 4 方式 / 認証 3 方式表 / completion セクション / MCP セットアップ / リリース手順 runbook）
- [x] M10 持ち越し polish: transport.go errcheck 解消（IIFE 形式）/ facade ToolDeps に APIResolver Factory 追加 / ErrAuthRequired → AUTH_REQUIRED マッピング
- [x] go test -race -cover ./... 全 22 パッケージ pass、golangci-lint 違反 0（既存 transport.go 2 件も解消）
- 詳細: plans/kintone-m11-completion-docker-release.md
- ブランチ: feat/m11-completion-docker-release（main への merge 待ち）

### M12: 統合 Storage バックエンド（進行中）
4 backend (Memory + SQLite + Redis + DynamoDB) を 1 つの `internal/store` パッケージに統合。kintone TokenStore + Cache + OIDC SigningKey + idproxy 状態を同一 backend に格納。
- [x] Phase 0: 事実確認 + 決定ドキュメント（`docs/decisions/0001-sqlite-pool.md` / `0002-idproxy-store-fact-finding.md` / `0003-interface-compat.md`）
- [x] Phase 1: `internal/store` 骨格 + Memory backend + Container/Factory + `internal/output/logger.go`（slog 基盤）+ goleak 検証
- [ ] Phase 2: SQLite backend（kintone.db のみ、modernc.org/sqlite + WAL + busy_timeout）
- [ ] Phase 3: SigningKey 永続化 + idproxy.Store 統合（auth=oidc 軸 fail-fast）
- [ ] Phase 4: Redis backend
- [ ] Phase 5: DynamoDB backend（GSI1 + GSI2）
- [ ] Phase 6: CLI/MCP 配線（Wave A/B、Container ライフサイクル、エラー envelope）
- [ ] Phase 7: 旧 `internal/tokenstore` / `internal/cache` 削除
- [ ] Phase 8: `kintone store init` コマンド
- [ ] Phase 8.5: E2E ハーネス（OIDC stub + kintone-fake + CI matrix）
- [ ] Phase 9: ドキュメント更新
- 詳細: plans/binary-imagining-lemur.md
- ブランチ: feat-m12-unified-storage-phase01

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
| 7 | M12 統合 Storage は同一 backend で kintone と idproxy を論理分離 | 工数最小・upstream 追従容易・設定 1 つの体感 | 2026-05-01 |
| 8 | M12 SQLite は同ディレクトリ・2 ファイル分離（kintone.db + idproxy.db）| idproxy v0.4.2 の `New(path)` API 制約により *sql.DB 共有不可（ADR 0001） | 2026-05-01 |
| 9 | M12 DynamoDB は upstream が GSI 不使用のため kintone 側で GSI1/GSI2 を追加し共存 | テーブル分離より配布シンプル（ADR 0002）| 2026-05-01 |
| 10 | M12 backend dispatch は register パターン（caller blank import 必須）| 循環 import 回避のため | 2026-05-01 |

## Changelog
| 日時 | 種別 | 内容 |
|------|------|------|
| 2026-04-29 07:55 | 作成 | ロードマップ初版作成（インタビューに基づき M1-M11 を確定） |
| 2026-04-29 09:15 | 進捗 | M01 完了（feat/m01-project-skeleton ブランチで 9 コミット）。devflow:cycle の Planner→devils-advocate→advocate(2 周)→advisor()→implementer(TDD)→手動動作確認まで一気通貫で実施。Current Focus を M02 に更新 |
| 2026-04-29 09:40 | 進捗 | M02 完了（feat/m02-config-layer ブランチ）。internal/config（91.4% カバレッジ）と CLI config show/init を実装。advisor() 指摘 4 件を計画に反映後 TDD で実装、手動確認 8 件 pass。Current Focus を M03 に更新 |
| 2026-04-29 12:58 | 進捗 | M03 完了（feat/m03-kintoneapi-client ブランチ）。internal/auth/apitoken（100%）・internal/kintoneapi（86.2%）・cli エラーマッピング（87.4%）を TDD で実装。全テスト pass、golangci-lint クリーン。Current Focus を M04 に更新 |
| 2026-04-29 13:14 | 進捗 | M04 完了（feat/m04-service-read-cli-api ブランチ）。internal/service/api（100%）・internal/service/operations（100%）・internal/cli/api（82%）を TDD で実装。`kintone api {records,record,app} ...` で kintone REST を JSON で叩けるように。CLI から kintoneapi 直 import 禁止のレイヤー分離を確立。全テスト pass、M04 新規 lint 違反 0。Current Focus を M05 に更新 |
| 2026-04-29 13:40 | 進捗 | M05 完了（feat/m05-cli-ops-write ブランチ）。kintoneapi に write 系（POST/PUT/DELETE）を追加し、service/api interface 拡張、operations.{RecordCreate, RecordUpdate, RecordDelete} を実装。CLI に `ops record {create,update,delete}` と `ops app describe` を追加。`--dry-run` で送信予定 body を JSON 出力（実 API と byte 一致）、書き込み系は MaxAttempts=1 デフォルト、`UsageError` 型 sentinel で USAGE 分類を堅牢化（advisor 6 件指摘反映済）。全テスト pass、カバレッジ目標達成。Current Focus を M06 に更新 |
| 2026-04-29 13:55 | 進捗 | M06 完了（feat/m06-mcp-server-facade ブランチ）。mark3labs/mcp-go v0.49.0 を採用、`internal/mcp/{server,facade}` と `internal/cli/mcp` を実装。kintoneapi に `ListApps`（GET /k/v1/apps.json）を新規追加、service/api interface 拡張。MCP 6 tools（apps_search / app_describe / records_query / record_create / record_update / record_delete）が完成し、`kintone mcp serve` で stdio 起動可能。`facade.MapError` で operations.Err\* / kintoneapi.APIError / network → MCP code をマップ（M05 ハンドオフ最重要事項対応）。出力は CLI と同じ `output.Success/Failure` envelope を `CallToolResult.Content[0].Text` に格納、契約共有を実現。in-process client で 6 tools 往復テストを網羅。全テスト pass、新規 lint 違反 0。Current Focus を M07 に更新 |
| 2026-04-29 18:10 | 進捗 | M07 完了（feat/m07-sqlite-cache-tokenstore ブランチ）。modernc.org/sqlite v1.50.0 を採用、`internal/cache`（SQLite キャッシュ層・TTL・パス解決）と `internal/tokenstore`（OAuth トークン保存）を TDD で実装。`CachingAPI` decorator（service/api）で `GetApp` / `GetAppFormFields` / `ListApps` にキャッシュを注入。CLI に `kintone cache clear / stats` サブコマンドを追加。`KINTONE_CACHE_DISABLE=1` で無効化対応。全テスト pass（cache 76.2% / tokenstore 79.0% / service/api 88.9%）、新規 lint 違反 0。Current Focus を M08 に更新 |
| 2026-04-29 18:50 | 進捗 | M08 完了（feat/m08-resolver ブランチ）。`internal/resolver` パッケージで App / Field 名前解決を TDD で実装（coverage 97.8%）。App: `ID 直接 → code 完全一致 → name 完全一致 → name 部分一致`、Field: `code → label 完全一致 → label 部分一致`、各段階でヒットしたら即 return（fallback しない）。operations 層に `AppRef` / `UpdateKeyFieldRef` フィールドを追加し、resolver 引数（nil 許容）でハイブリッド解決（既存 `App int64` 直指定経路は完全後方互換）。CLI 全コマンドに `--app-ref` / `--update-key-field-ref` を追加、MCP 全 tools に `app_ref` / `update_key_field_ref` を追加（`app: required` を外す）。`RESOLVER_APP_NOT_FOUND` / `RESOLVER_APP_AMBIGUOUS` 等のエラーコードと `details.candidates` を CLI/facade 両方にミラー実装。CachingAPI 経由で apps/fields のキャッシュを共有（resolver 専用キャッシュは持たない）。全テスト pass（resolver 97.8% / operations 98.2% / cli 85.2% / cli/api 72.1% / cli/ops 70.9% / facade 79.6%）、新規 lint 違反 0。Current Focus を M09 に更新 |
| 2026-04-29 23:00 | 進捗 | M09 完了（feat/m09-oauth-auth ブランチ）。`internal/auth/oauth` パッケージで OAuth 2.0 Authorization Code + PKCE フローを TDD で実装（coverage 88.3%）。loopback callback サーバ（sync.Once + graceful shutdown）/ PKCE S256（crypto/rand）/ state 検証（subtle.ConstantTimeCompare）/ refresh_token 自動更新（skew 60s + sync.Mutex 並行制御）/ OS 別ブラウザ起動（darwin/linux/windows）。`kintoneapi.NewFromResolvedWithAuth` を新設し依存方向を維持。CLI に `kintone auth login --oauth --principal-id <id>` / `auth status` / `auth logout` を追加。config に `KINTONE_OAUTH_CLIENT_ID/SECRET/REDIRECT_URL/SCOPES` 環境変数を追加、`config show` で client_secret を `***` マスク。TokenStore（M07 既存基盤）を本格利用。全テスト pass（auth/oauth 88.3% / cli/auth 74.1% / cli 86.7%）、新規 lint 違反 0。Current Focus を M10 に更新 |
| 2026-04-30 00:50 | 進捗 | M10 完了（feat/m10-idproxy-multiuser-mcp ブランチ）。`github.com/youyo/idproxy` v0.4.2 を採用し thin wrapper（`internal/idproxy`）で kintone Principal context へ正規化。HTTP/Streamable transport を `internal/mcp/server/http.go` に追加し、`mark3labs/mcp-go` v0.49.0 の `NewStreamableHTTPServer` で /mcp を提供。`kintone mcp serve --listen :8080 --auth oidc --authz oauth` で multi-user remote MCP 起動が可能に。`service/api/PrincipalAPIFactory` で per-request にユーザー別 TokenStore（M07 基盤）を引く設計。SSE transport は仕様 2025-03-26 で非推奨方向のため M11+ に明示移動。advisor の指摘 3 点（mcp-go API 検証 / principalFromUser 単体テスト / プロビジョニングモデル明記）を計画に反映後 TDD で実装。全 21 パッケージ test pass（race 検出なし）、新規 lint 違反 0、既存 stdio + auth=none + authz=api-token は完全後方互換。Current Focus を M11 に更新 |
| 2026-05-01 09:50 | 進捗 | M12 Phase 0-1 完了（feat-m12-unified-storage-phase01 ブランチ）。**Phase 0**: idproxy v0.4.2 の SQLite/Redis/DynamoDB Store 構築 API、modernc.org/sqlite v1.50.0 の DSN 構文（`_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)`）と 2 Pool 並行挙動（5000 ops / 0 BUSY）を検証。新旧 interface 形状の互換性を確認し ADR 0001/0002/0003 として `docs/decisions/` に固定。DynamoDB は upstream に GSI が無いため kintone 側のみ GSI1/GSI2 を追加する方針で共存（テーブル共有維持）。**Phase 1**: `internal/store/{doc,errors,types,env,tokens,cache,signingkey,container,factory}.go` + `internal/store/memory/{tokens,cache,signingkey,idproxy_adapter,container,register}.go` + `internal/store/storetest/conformance.go` を TDD で実装。`Container` interface（`Tokens / CacheForDecorator / CacheForAdmin / SigningKey / IDProxyStore / LocationString / Close(ctx)`）+ context helper（`WithContainer` / `ContainerFromContext`）+ register パターンで factory dispatch（循環 import 回避、caller は blank import が必要）。`internal/output/logger.go` に slog ベースの stderr logger（`KINTONE_LOG_LEVEL` 制御）を新設。`go.uber.org/goleak v1.3.0` を依存追加し `Container.Close` 後の goroutine leak ゼロを検証。Conformance テストで TokenStore/CacheStore/SigningKeyStore の正常系・異常系を backend 横断的にカバー。code-reviewer 9 観点レビューで Critical 1 件（SA1019 deprecated `k1.D`）+ High 2 件（factory メッセージの phase 依存削除 + register 文書化）を修正。全 24 パッケージ test pass、`golangci-lint run` 違反 0、`gofmt` 差分 0。 |
| 2026-04-30 01:05 | 進捗 | M11 完了（feat/m11-completion-docker-release ブランチ）。`internal/cli/completion` パッケージで `kintone completion {bash|zsh|fish|powershell}` を実装（cobra `Gen*Completion` ラップ、JSON envelope の例外として明示）。Dockerfile を multi-stage（`golang:1.26-alpine` → `gcr.io/distroless/static-debian12:nonroot` / CGO_ENABLED=0 / uid 65532）で作成、`.dockerignore` で context 最小化。`.goreleaser.yaml` で linux/{amd64,arm64} + darwin/{amd64,arm64} + windows/amd64 cross build、Homebrew Tap (`youyo/homebrew-tap`)、ghcr.io multi-arch manifest を構成（`goreleaser check` 通過）。`.github/workflows/release.yml` でタグ push 起動の goreleaser/goreleaser-action@v6 ワークフローを追加（actionlint クリア）。M10 持ち越し polish: transport.go errcheck を IIFE 形式 `defer func() { _ = resp.Body.Close() }()` で解消、`internal/mcp/facade.ToolDeps` に `APIResolver` interface 型 `Factory` フィールドを追加（オプショナル / 後方互換）、6 ハンドラを `resolveAPI(ctx, deps)` 共通ヘルパー経由に統一、`facade.MapError` に `serviceapi.ErrAuthRequired` → `AUTH_REQUIRED` マッピングを追加。README をインストール 4 方式 / 認証 3 方式 / completion / MCP セットアップ / リリース runbook で完備。全 22 パッケージ test pass（race 検出なし）、`golangci-lint run` 違反 0（既存 transport.go errcheck 2 件も解消）。**全 11 マイルストーン完了 / リリース準備完了**。 |
