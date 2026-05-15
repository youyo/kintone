# Roadmap: kintone CLI / MCP サーバー

## Meta
| 項目 | 値 |
|------|---|
| ゴール | kintone REST API を操作する統合 CLI / MCP サーバーをリリース可能な品質で提供する |
| 成功基準 | (1) CLI から API Token + OAuth で record CRUD と app_describe が実行可能 (2) MCP サーバーが remote/multi-user で動作 (3) GoReleaser/Homebrew/Docker で配布可能 (4) JSON 固定出力 (5) TDD によるユニットテスト網羅 |
| 制約 | Go 1.26 / 仕様書（docs/specs/kintone_spec.md）準拠 / multi-user 対応 / profile + env override / 配布形態 4 種 |
| 対象リポジトリ | /Users/youyo/src/github.com/youyo/kintone |
| 作成日 | 2026-04-29 |
| 最終更新 | 2026-05-12 |
| ステータス | M1-M15 完了 (v0.4.1 リリース準備完了) |

## Current Focus
- **マイルストーン**: M17 完了（idproxy v0.5.0 OnAuthenticated 採用による Claude Desktop OAuth カスケード対応）
- **直近の決定**: idproxy v0.5.0 で新規追加された `Config.OnAuthenticated` フックを採用し、OIDC 認証完了直後（/callback 内）に kintone OAuth トークンの有無を確認して未取得なら `/oauth/kintone/start` へ自動カスケード。当初の M17 計画（`KintoneAuthorizeGate` + `StateEntry.ContinueURL` + 4-backend schema 変更）を大幅に簡略化した。Claude Desktop の初回接続は CALLBACK_PORT timeout で 1 回 retry が必要だが、2 回目以降は完全自動。
- **次のアクション**: タグ `v0.5.0` を push して GoReleaser ワークフローを起動 → リリース。M18（idproxy への OnAuthenticated(stateData) 拡張要望の起票 → Claude Desktop 完全自動化）は upstream 改善次第。

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
- [x] Phase 2: SQLite backend（kintone.db / idproxy.db 別ファイル分離、WAL + busy_timeout、2 Pool 並行 stress test pass）
- [x] Phase 3: SigningKey 解決ロジック + 禁止組合せ検証（`internal/idproxy/signingkey.go` 独立関数として実装、BuildAuth 配線は Phase 6）
- [x] Phase 4: Redis backend（kintone:/idproxy: prefix 分離、rediss:// 強制 TLS、URL sanitize、miniredis ベース conformance）
- [x] Phase 5: DynamoDB backend（単一テーブル + kintone 側 GSI1/GSI2、Auto-TTL、Scan 不使用 全ページ Query、ConditionalPutItem race-safe、fake injection test）
- [x] Phase 6: CLI/MCP 配線（Phase 6a backends 集約 + root Container ライフサイクル / 6b auth/cache caller / 6c api/ops/mcp + per-principal Factory + BuildAuth / 6d 新エラー code 9 種 mapping）
- [x] Phase 7: 旧 `internal/tokenstore` / `internal/cache` 削除（grep でゼロ確認、全 24 パッケージ test PASS）
- [x] Phase 8: `kintone store init` コマンド（DynamoDB テーブル検証）
- [x] Phase 8.5: in-process E2E ハーネス基盤（oidcstub + kintonefake + SeedTokenForE2E + e2e build tag テスト、Makefile）
- [x] Phase 9: ドキュメント更新（README / spec / CHANGELOG）
- 詳細: plans/binary-imagining-lemur.md
- ブランチ: feat-m12-unified-storage-phase01

### M13: Remote MCP 用 サーバホスト型 OAuth callback ✅ 完了
kintone OAuth は redirect_uri に https を強制するため、ローカル CLI loopback フローは廃止（v0.3.0）。リモート MCP サーバ自身が OAuth client として振る舞い、ユーザの kintone トークンをサーバ側で取得・保存する。
- [x] `mcp serve --listen` 時に `/oauth/kintone/start` と `/oauth/kintone/callback` ハンドラを追加（専用パッケージ `internal/mcp/oauthcallback`）
- [x] `state` パラメータに OIDC `sub` を紐付け（`StateStore` interface + in-memory 実装 / TTL 10 分 / one-shot Take semantics）
- [x] callback で受信した authorization code を kintone token endpoint に交換し、TokenStore に `Domain + sub + AuthType=oauth` で保存（既存スキーマ流用）
- [x] PrincipalAPIFactory が token 不在を検知した場合、構造化 `AuthRequiredError` を返し facade が `AUTH_REQUIRED` envelope の `details.authorize_url` に authorize URL を含める
- [x] redirect_uri は MCP サーバの公開 https URL 1 つに固定 + 起動時 fail-fast 検証（HTTPS 強制 + ExternalURL 完全一致 / `KINTONE_OAUTH_ALLOW_PLAINTEXT_REDIRECT=1` で localhost http opt-in 許容）
- [x] CSRF 三重保護: idproxy OIDC Principal + state cookie + state map の PrincipalID 比較（constant-time compare）
- [x] `internal/auth/oauth/` の token exchange / refresh ロジックは流用、loopback サーバ部分は deprecated（unexport / 物理削除は M14）
- [x] E2E: `internal/testsupport/kintonefake` を `/oauth2/authorization` 追加で拡張し、start → authorize → callback → Token 永続化のフローを build tag `e2e` で検証（CSRF 異常系も）
- [x] ドキュメント: README.md / README.ja.md / docs/specs / CHANGELOG.md 更新
- 詳細: plans/kintone-m13-remote-mcp-oauth-callback.md
- ブランチ: feat-auth-model-cli-apitoken-mcp-oauth（v0.3.0 リリース完了 / merged）

### M14: StateStore 統合 Storage 拡張 + loopback flow 物理削除 ✅ 完了
M13 で in-memory に閉じた OAuth `StateStore` を `internal/store` の 4 backend（Memory/SQLite/Redis/DynamoDB）に統合し、`KINTONE_STORE_BACKEND` 単一設定で kintone Token + Cache + idproxy session + OAuth state を同一 backend に格納できるようにする（multi-replica MCP サーバ対応）。あわせて M13 で deprecated 化した OAuth loopback サーバの物理削除を実施する。
- [x] `internal/store` に `StateStore` interface を移設（または mirror）し、`Container.StateStore()` メソッドを追加
- [x] Memory backend: 既存 `oauthcallback.MemoryStateStore` を `internal/store/memory` に移植し register
- [x] SQLite backend: `kintone_oauth_state` テーブル追加、`DELETE ... RETURNING` で one-shot Take atomic
- [x] Redis backend: `kintone:oauthstate:<state>` hash + EXPIRE TTL、HGETALL+DEL を Lua script で atomic
- [x] DynamoDB backend: 既存単一テーブルに `pk=kintone:oauthstate:<state>` で相乗り、`DeleteItem ReturnValues=ALL_OLD` で atomic Take
- [x] `internal/mcp/oauthcallback` の Handler コンストラクタを `Container.StateStore()` 経由に切替
- [x] Conformance テスト追加: `RunStateStoreConformance`（並行 Take 単一勝者保証 N=20 含む）を 4 backend で実行
- [x] `internal/auth/oauth/{flow,callback,browser}.go` の loopback サーバ部分を物理削除
- [x] ドキュメント: README.md / README.ja.md / docs/specs/kintone_spec.md / CHANGELOG.md 更新
- [x] 後方互換: `oauthcallback.NewHandler` / `MemoryStateStore` / `StateEntry` / `ErrStateNotFound` は型エイリアス + ラッパーで API 維持
- 詳細: plans/kintone-m14-statestore-storage-integration.md

### M15: MCP serve wiring hardening
M14 の copilot:code-review で指摘された MCP serve の wiring 既存課題 2 件を fix-only マイルストーンとして解消する（v0.4.1 patch）。silent no-op を fail-fast に変え、HTTP+OAuth では起動時の buildAPI を skip する。
- [x] **(1) stdio + authz=oauth の fail-fast**: stdio transport は単一プロセス・単一認証文脈のため OAuth 認可と矛盾する。`mcp serve`（stdio）で `--authz=oauth` 指定 or `KINTONE_MCP_AUTHZ_MODE=oauth` 設定時は起動時に `clierr.UsageError` で拒否し、`USAGE`/`STORE_*` envelope を返す（silent no-op の運用事故を排除）
- [x] **(2) HTTP + authz=oauth で buildAPI を skip**: HTTP transport + OAuth では `PrincipalAPIFactory` が per-request にユーザー別 token から API client を生成するため、起動時の固定 API client (`buildAPI`) は不要かつ誤動作の原因。`buildAPI` の呼び出しを mode で分岐し、HTTP+OAuth 時は skip して Factory 経由を強制する
- [x] 既存テストへの影響を最小化（後方互換）: stdio + authz=api-token / HTTP + authz=api-token / HTTP + authz=oauth の 3 経路を網羅するテーブルテスト追加
- [x] エラーメッセージ: 「stdio transport does not support authz=oauth (use --listen and authz=oauth instead, or remove --authz=oauth for API token)」のように具体的な復旧手順を含める
- [x] ドキュメント: README の MCP serve 認証マトリクス、docs/specs の MCP wiring 節を更新、CHANGELOG に v0.4.1 エントリ追加
- [x] 検証: `go test -race ./...` 全 pass / `gofmt -l .` 差分 0 / `golangci-lint run` 違反 0 / `go vet ./...` クリア
- 詳細: plans/kintone-m15-mcp-serve-wiring-hardening.md

### M16: OIDC callback ブラウザフロー自動カスケード（issue #5 一次修正） ✅ 完了
`kintone mcp serve --auth oidc --authz oauth` で OIDC ログイン後にブラウザが `/` にリダイレクトされ 404 となるバグ（[issue #5](https://github.com/youyo/kintone/issues/5)）の一次修正。idproxy v0.4.2 が認証後デフォルトで `"/"` へ redirect するが kintone は `/` にハンドラ未登録。logvalet の `EnsureBacklogConnected` パターンを踏襲し cascade middleware を追加。
- [x] `internal/cli/mcp/cascade.go`: `EnsureKintoneOAuthConnected` middleware 実装（kill switch `KINTONE_MCP_DISABLE_OAUTH_CASCADE=1` 対応）
- [x] `internal/cli/mcp/cascade_test.go`: テーブル駆動テスト 11 ケース（N1-N3, E1-E5, E13, E14）
- [x] `internal/cli/mcp/oauth_glue.go`: `oauthSetup` に `Tokens` / `StartURL` フィールド追加
- [x] `internal/cli/mcp/serve.go`: `(auth=oidc, authz=oauth)` のとき cascade middleware を内側に合成
- [x] `internal/cli/mcp/serve_e2e_test.go`: E2E カスケードフロー検証追加（`/login → /callback → / → /oauth/kintone/start → /oauth/kintone/callback`）
- [x] 全テスト pass（`go test -race -cover ./...` + `-tags e2e`）、golangci-lint 0 violations
- **M17 予定**: `KintoneAuthorizeGate`（Claude Desktop OAuth AS カスケード）/ `continue` URL / `StateEntry.ContinueURL` / 4-backend schema 拡張
- 詳細: plans/wondrous-swinging-locket.md

### M17: idproxy v0.5.0 OnAuthenticated 採用による Claude Desktop OAuth カスケード対応 ✅ 完了
issue #5 の Claude Desktop 経路を改善するため、idproxy v0.5.0 で新規追加された `Config.OnAuthenticated` フックを採用し、OIDC 認証完了直後に kintone OAuth トークンの有無を確認して未取得なら `/oauth/kintone/start` へ自動カスケード。当初計画（KintoneAuthorizeGate + ContinueURL + 4-backend schema）を大幅に簡略化。
- [x] `go.mod` / `go.sum`: idproxy v0.4.2 → v0.5.0
- [x] `internal/idproxy/config.go::BuildAuth`: 末尾に `hook` 引数を追加、`UseStrictPostLoginRedirectValidator()` を常時呼び出し（open redirect 対策）
- [x] `internal/cli/mcp/idproxy_glue.go::buildOnAuthenticatedHook`: kintone OAuth カスケード hook 実装（kill switch `KINTONE_MCP_DISABLE_OAUTH_CASCADE=1` 対応、相対パス `/oauth/kintone/start` 強制で StrictValidator 互換）
- [x] `internal/cli/mcp/serve.go::runHTTP`: hook を `buildOIDCMiddleware` 経由で `BuildAuth` に伝搬、`defer setup.closeStates()` を `if setup != nil` ブロック冒頭に移動（buildHTTPMiddleware エラー時のリソースリーク防止）
- [x] `internal/cli/mcp/idproxy_glue_test.go`: テーブル駆動テスト N1-N4, E1-E3, E6 全 10 ケース実装
- [x] 全テスト pass（`go test -race -cover ./...` + `-tags e2e`）、gofmt / go vet クリア
- [x] M16 cascade middleware（`EnsureKintoneOAuthConnected`）は **temporal フォールバック**として温存
- **M18 予定**: idproxy 側で `OnAuthenticated` シグネチャに `stateData *AuthCodeData` を追加してもらう upstream issue を起票し、kintone callback から元の `/authorize?...` URL へ自動復帰する完全自動化を実装。
- 詳細: plans/kintone-m17-idproxy-v0.5-onauthenticated.md

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
| 11 | M12 KINTONE_STORE_* は env のみ（config.toml に不可）| K8s Secret / Lambda env 注入が主用途・secrets の誤コミット防止 | 2026-05-01 |
| 12 | M12 auth=oidc × memory backend は全面禁止（STORE_MEMORY_OIDC_FORBIDDEN）| multi-replica session 孤立・プロセス再起動全失効リスクを startup 時点で排除 | 2026-05-01 |
| 13 | M12 SigningKey は env > Storage > ephemeral（auth=oidc は fail-fast）| 再起動耐性 OIDC JWT を保証しつつ dev 利便性を維持 | 2026-05-01 |

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
| 2026-05-01 17:25 | 進捗 | M12 Phase 6-7 完了（feat-m12-unified-storage-phase01 ブランチ、4fa4b62 まで 14 コミット）。**Phase 6a** backends 集約 import + root Container ライフサイクル管理（4 backend を `cmd/kintone/main.go` + `internal/cli/backends.go` で blank import 集約、`ExecuteWith` に PersistentPreRunE で Container Open + defer で sync.Once Close を集中、`needsStore` マトリクスで read-only コマンドの DB 副作用ゼロを保証）。**Phase 6b** auth/cache caller を Storage 経由に切替（`SetOpenStoreFn` / `SetNewContainerBuilder` hook 移行、`cache stats` JSON schema を新形式に刷新 [backend/location/reachable/entry_count/expired_count/backend_specific]、`cache clear --key <prefix>` 高度デバッグフラグ追加）。**Phase 6c** api/ops/mcp + per-principal Factory + BuildAuth 配線（`internal/auth/oauth/{provider,refresh}.go` を `store.TokenStore` 引数化、`internal/service/api/{principal,caching}.go` で per-request lazy CacheProvider に切替し長寿命プロセスの自動回復を実現、`internal/cli/{api,ops,mcp}/helpers.go` を Storage 経由 + `KINTONE_STORE_CACHE_BYPASS` 対応で旧 `KINTONE_CACHE_DISABLE` を削除、`internal/mcp/server/server.go::NewWithDeps` 新設、`internal/cli/mcp/serve.go` の authzMode を `buildHTTPMiddleware` に伝播、`internal/idproxy/config.go::BuildAuth(ctx, env, authMode, authZMode, container)` シグネチャ変更で `ResolveSigningKey` + `Container.IDProxyStore` 経由の解決を確立）。**Phase 6d** 新エラー code 9 種を CLI/MCP envelope に mapping（`STORE_TABLE_NOT_FOUND` / `STORE_GSI_MISSING` / `STORE_TTL_DISABLED` / `STORE_CONNECTION_FAILED` / `STORE_MEMORY_OIDC_FORBIDDEN` / `SIGNING_KEY_REQUIRED` / `STORE_CACHE_BYPASS_INVALID` / `STORE_PLAINTEXT_FORBIDDEN` / `RESOLVER_PRINCIPAL_NOT_FOUND`、`internal/output/sanitize.go::ClassifyBackendError` で `STORE_CONNECTION_FAILED.details.cause_class` を network/auth/timeout/unknown に分類し raw err leak 防止）。**Phase 7** 旧 `internal/tokenstore` + `internal/cache` ディレクトリを完全削除（grep で残存 import がゼロ、`go test -race ./...` 全 24 パッケージ PASS、`go vet` / `gofmt` クリア）。残作業は Phase 8（store init コマンド、0.5 日）/ Phase 8.5（E2E ハーネス、1.5 日）/ Phase 9（docs、0.5 日）。 |
| 2026-05-01 18:30 | 進捗 | M12 全 11 フェーズ完了（feat-m12-unified-storage-phase01 ブランチ、441dc6b まで 17 コミット）。**Phase 8.5**: in-process E2E ハーネス基盤を新設（`internal/testsupport/oidcstub/oidcstub.go` で httptest ベース最小 OIDC プロバイダ [discovery / jwks / authorize / token / userinfo, RS256 署名, RSA 鍵起動時生成]、`internal/testsupport/kintonefake/kintonefake.go` で kintone OAuth + REST mock [refresh_token rotation 対応, Bearer 検証, SeedTokenFor 公開]、`internal/store/storetest/seed.go::SeedTokenForE2E` ヘルパ、`internal/cli/mcp/serve_e2e_test.go` の build tag `e2e` テストで sqlite backend × `oauth.Refresher.Refresh` + Storage 永続化を検証、`Makefile` に `test-quick` / `test-integration` / `test-e2e` ターゲット追加）。CI matrix への組み込みは M13+ で対応予定。全 25 パッケージ test pass、`go vet` / `gofmt` クリア。**M12 統合 Storage バックエンド完了 / リリース準備完了**。 |
| 2026-05-01 21:55 | 進捗 | M12 Phase 8-9 完了（feat-m12-unified-storage-phase01 ブランチ）。**Phase 8**: `kintone store init dynamodb --table NAME --region REGION [--capability]` コマンド実装（DynamoDB テーブル存在確認・GSI 検証・TTL 確認、新エラー code `STORE_TABLE_NOT_FOUND` / `STORE_GSI_MISSING` / `STORE_TTL_DISABLED` を返す）。**Phase 9**: README.md / README.ja.md を Storage Backend セクション追加で更新（4 backend 表 / 設定例 / DynamoDB 最小 IAM / Redis ACL / MCP secret 2 種 / principal_id 規約 / memory+auth=oidc 禁止）、環境変数表を新体系に更新（旧 KINTONE_CACHE_PATH/TOKENS_PATH/CACHE_DISABLE 削除、KINTONE_STORE_* 系追加）、CHANGELOG.md を新規作成（M12 BREAKING CHANGES 全エントリ）、docs/specs/kintone_spec.md のデータストア節・ディレクトリ構成・環境変数を統一 Storage 構成に書き換え、plans/kintone-roadmap.md を Phase 8/9 完了に更新（Architecture Decisions 4 件追加）。残作業は Phase 8.5（E2E ハーネス）のみ。 |
| 2026-05-01 11:55 | 進捗 | M12 Phase 2-5 完了（feat-m12-unified-storage-phase01 ブランチ、b39131b まで 7 コミット）。**Phase 2** SQLite backend（kintone.db / idproxy.db 別ファイル、WAL + busy_timeout=5000 + synchronous=NORMAL DSN、3 テーブル DDL、0o600 ファイル権限、2 Pool 並行 stress test pass、ファイル lazy init で IDProxyStore 未呼び出し時は idproxy.db 未作成）。**Phase 3** SigningKey 解決ロジック + 禁止組合せ検証（`internal/idproxy/signingkey.go` で `ResolveSigningKey` + `ValidateBackendForAuthMode` を独立関数として実装、env > Storage > ephemeral の優先順序、auth=oidc + memory backend は startup 拒否、`KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` opt-in、PEM パース PKCS#8 一次・EC PRIVATE KEY フォールバック、`internal/output/logger.go` に test seam 追加。BuildAuth 配線は Phase 6 Wave B に持ち越し）。**Phase 4** Redis backend（`redis.UniversalClient` 単一共有、`kintone:` / `idproxy:` 2 prefix 分離、rediss:// 常時 TLS（downgrade 不可）、`KINTONE_STORE_REDIS_INSECURE_PLAINTEXT=1` 制御、`internal/output/sanitize.go` で URL の userinfo / クエリ password を `***` マスク、idproxy adapter は client Close 抑制 wrapper、SigningKey は SETNX で race-safe、miniredis v2 ベース conformance test pass）。**Phase 5** DynamoDB backend（単一テーブル + kintone 側 `gsi1pk/gsi1sk/gsi2pk/gsi2sk` + GSI1/GSI2 を追加、idproxy 側は GSI 不使用で共存、属性名 lowercase で upstream 整合、`expires_at`(ns) + `ttl`(sec) 両用 Auto-TTL、Scan 不使用、ListByDomain は GSI1 Query で全ページ走査、Cache.Stats は GSI2 Query KEYS_ONLY 全ページ count、BatchWriteItem 25 件ずつ + UnprocessedItems 再送 max 3、ConditionalPutItem で SigningKey race-safe 永続化、`dynamoDBAPI` interface + fake injection で dynamodb-local 不要の unit test）。全 27 パッケージ test pass、`go vet` 違反 0、`gofmt` 差分 0。残り Phase 6（CLI/MCP 配線、4.5 日）/ 7（旧パッケージ削除）/ 8（store init）/ 8.5（E2E）/ 9（docs）。 |
| 2026-05-12 08:35 | 進捗 | M15 完了（feat-m15-mcp-serve-wiring-hardening ブランチ）。`internal/mcp/server/auth.go` に `ErrStdioOAuthUnsupported` typed sentinel と `ValidateModes` の stdio+oauth 拒否を追加し、`internal/cli/mcp/serve.go::runServe` で sentinel を検出して `clierr.NewUsageError` で復旧手順入りメッセージに wrap。HTTP + authz=oauth では起動時 `buildAPI` を skip し、Factory + per-request OAuth token 解決に一本化。`runHTTP` に `deps.API == nil && deps.Factory == nil` の defensive 検証を追加（将来 wiring 変更時の NPE 防止）。認証マトリクス 4 経路（stdio+api-token / stdio+oauth / http+api-token / http+oauth）のテーブルテストを `serve_test.go` に追加（`isolateMCPEnv` で CI 環境変数の漏れを遮断）。README.md / README.ja.md / docs/specs/kintone_spec.md / CHANGELOG.md を更新。全 26 パッケージ `go test -race` PASS、`go test -race -tags e2e` PASS、`gofmt -l .` 差分 0、`golangci-lint run` 違反 0、`go vet` クリア。後方互換: stdio + api-token / HTTP + api-token / HTTP + auth=oidc + authz=oauth は完全無影響。v0.4.1 patch リリース準備完了。 |
| 2026-05-01 09:50 | 進捗 | M12 Phase 0-1 完了（feat-m12-unified-storage-phase01 ブランチ）。**Phase 0**: idproxy v0.4.2 の SQLite/Redis/DynamoDB Store 構築 API、modernc.org/sqlite v1.50.0 の DSN 構文（`_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)`）と 2 Pool 並行挙動（5000 ops / 0 BUSY）を検証。新旧 interface 形状の互換性を確認し ADR 0001/0002/0003 として `docs/decisions/` に固定。DynamoDB は upstream に GSI が無いため kintone 側のみ GSI1/GSI2 を追加する方針で共存（テーブル共有維持）。**Phase 1**: `internal/store/{doc,errors,types,env,tokens,cache,signingkey,container,factory}.go` + `internal/store/memory/{tokens,cache,signingkey,idproxy_adapter,container,register}.go` + `internal/store/storetest/conformance.go` を TDD で実装。`Container` interface（`Tokens / CacheForDecorator / CacheForAdmin / SigningKey / IDProxyStore / LocationString / Close(ctx)`）+ context helper（`WithContainer` / `ContainerFromContext`）+ register パターンで factory dispatch（循環 import 回避、caller は blank import が必要）。`internal/output/logger.go` に slog ベースの stderr logger（`KINTONE_LOG_LEVEL` 制御）を新設。`go.uber.org/goleak v1.3.0` を依存追加し `Container.Close` 後の goroutine leak ゼロを検証。Conformance テストで TokenStore/CacheStore/SigningKeyStore の正常系・異常系を backend 横断的にカバー。code-reviewer 9 観点レビューで Critical 1 件（SA1019 deprecated `k1.D`）+ High 2 件（factory メッセージの phase 依存削除 + register 文書化）を修正。全 24 パッケージ test pass、`golangci-lint run` 違反 0、`gofmt` 差分 0。 |
| 2026-04-30 01:05 | 進捗 | M11 完了（feat/m11-completion-docker-release ブランチ）。`internal/cli/completion` パッケージで `kintone completion {bash|zsh|fish|powershell}` を実装（cobra `Gen*Completion` ラップ、JSON envelope の例外として明示）。Dockerfile を multi-stage（`golang:1.26-alpine` → `gcr.io/distroless/static-debian12:nonroot` / CGO_ENABLED=0 / uid 65532）で作成、`.dockerignore` で context 最小化。`.goreleaser.yaml` で linux/{amd64,arm64} + darwin/{amd64,arm64} + windows/amd64 cross build、Homebrew Tap (`youyo/homebrew-tap`)、ghcr.io multi-arch manifest を構成（`goreleaser check` 通過）。`.github/workflows/release.yml` でタグ push 起動の goreleaser/goreleaser-action@v6 ワークフローを追加（actionlint クリア）。M10 持ち越し polish: transport.go errcheck を IIFE 形式 `defer func() { _ = resp.Body.Close() }()` で解消、`internal/mcp/facade.ToolDeps` に `APIResolver` interface 型 `Factory` フィールドを追加（オプショナル / 後方互換）、6 ハンドラを `resolveAPI(ctx, deps)` 共通ヘルパー経由に統一、`facade.MapError` に `serviceapi.ErrAuthRequired` → `AUTH_REQUIRED` マッピングを追加。README をインストール 4 方式 / 認証 3 方式 / completion / MCP セットアップ / リリース runbook で完備。全 22 パッケージ test pass（race 検出なし）、`golangci-lint run` 違反 0（既存 transport.go errcheck 2 件も解消）。**全 11 マイルストーン完了 / リリース準備完了**。 |
