# Changelog

## [Unreleased] - Cloudflare Workers 版への作り直し(`feat/workers-rewrite`)

Go 製の CLI + MCP サーバー(v0.5.6 まで)を、Cloudflare Workers 上の **Remote MCP サーバー**として作り直した。Go 版は `archive/go` ブランチ・タグに保存してあり、v0.5.6 の Homebrew / ghcr のイメージはそのまま使える。移行の手順と破壊的変更の一覧は [README](README.md#go-版の利用者への移行のお知らせ)。

### BREAKING CHANGES

- **CLI を廃止**(Homebrew・Docker・GoReleaser・`go install`・stdio も廃止)。Remote MCP サーバー(Cloudflare Workers + KV + Hono)だけになった
- **ツール名に `kintone_` が付く**(`kintone_apps_search` `kintone_app_describe` `kintone_records_query` `kintone_record_create` `kintone_record_update` `kintone_record_delete`)。引数名は Go 版を踏襲
- **出力・エラーの形式**: 成功は `{"ok":true,"data":...}` の包みをやめ、kintone のデータそのもの(加工したものは snake_case)。失敗は `isError: true` と `{"schema_version":"1","error":{"code","message","retryable","details"?}}`
- **エラーコードが小文字の snake_case**になった(`KINTONE_UNAUTHORIZED` → `authentication_error` など。対応は仕様書 §6)。新しいコード(`resolver_app_exact_match_required` `resolver_field_exact_match_required` `response_too_large` など)を追加
- **認証**: 利用者ごとの kintone OAuth(Worker が OAuth 認可サーバー。トークンは暗号化して Cloudflare KV)と、**Cloudflare Access の email による利用者の識別**(`/authorize` と `/callback` を Access の self-hosted アプリで保護)に変わった。OIDC(idproxy)は廃止
- **API トークン(`X-Cybozu-API-Token`)を廃止**
- OAuth のリダイレクト URI が `<PUBLIC_BASE_URL>/callback` に変わった(cybozu.com の OAuth クライアントの登録のやり直しが必要。scope は Go 版と同じ 6 つ)
- **設定項目**: `KINTONE_DOMAIN` → `KINTONE_HOST`(ホスト全体)。Cloudflare の Variables / Secrets と wrangler の設定に移った(`PUBLIC_BASE_URL` `ACCESS_TEAM_DOMAIN` `ACCESS_AUD` `KINTONE_CLIENT_ID` `KINTONE_CLIENT_SECRET` `KINTONE_OAUTH_PKCE`、KV `OAUTH_KV`)。`config.toml`・プロファイル・SQLite / Redis / DynamoDB は廃止
- 名前解決: キャッシュは利用者(grant)ごと・TTL 600 秒(Go 版は利用者に依存せず TTL 1 年)、名前検索は最大 10 ページ。**書き込みの `app_ref` と `update_key_field_ref` は完全一致だけ**(部分一致のみなら候補を返して書き込まない)

### Added

- **M01 雛形と OAuth**: Worker が OAuth 認可サーバー(DCR・PKCE・`/token`)と MCP サーバーを兼ねる。kintone OAuth(Basic 認証・PKCE・scope 6 つ)、Access JWT の検証と email からの利用者 ID、トークン更新、起動時の設定チェック(`/healthz`)、`mise run verify:oauth`
- **M02 基盤**: kintone REST クライアント(直列・読み取りだけ 429 / 503 を最大 2 回リトライ・呼び出し回数の上限・10 MiB・30 秒)、エラー表、ツール登録(4 区分の annotation・toolset・`?toolsets=` / `?read_only=true`・`max_response_tokens`)、tools/list のスナップショット
- **M03 読み取りツール**: `kintone_apps_search` / `kintone_app_describe` / `kintone_records_query`、名前解決(`app` / `app_ref`)、KV キャッシュ(grant 単位)
- **M04 書き込みツール**: `kintone_record_create` / `kintone_record_update` / `kintone_record_delete`、冪等キャッシュ(`kintone_record_create` だけ。5 分)、書き込みはリトライしない
- **M05 デプロイ準備**:
  - `scripts/render-wrangler-config/`: 環境変数 `KINTONE_MCP_*` から、gitignore 済みの `wrangler.deploy.jsonc` を生成する(`mise run deploy:render`。値は出さずに項目名と理由だけで検証する)。`mise run deploy:check` はダミー値の `wrangler deploy --dry-run`、`mise run deploy` が実際のデプロイ
  - `.github/workflows/ci.yml` に `deploy:check`、`.github/workflows/deploy.yml`(下流の Variables / Secrets で動く。Worker 名の Variable が空の upstream では飛ばす)。アクションはコミット SHA で固定
  - `docs/runbook.md`(OAuth クライアント登録・Access の self-hosted アプリ・KV・Variables / Secrets・動作確認・WAF・IP 制限・切り分け・grant の失効)、README(ツール一覧・商標の注記・Go 版からの移行案内)、ADR 0012
  - `test:e2e` に `node-e2e`(生成スクリプトの実プロセス起動と `wrangler deploy --dry-run -c`)

### Changed

- `kintone_record_create` の description に、明示の冪等キーがないと 5 分以内の同じ引数の呼び出しは再送とみなして前回の結果を返すこと(2 件目は作られない)と、同じ内容を意図して 2 件作る方法(別の `idempotency_key`)を明記した

### Notes

- 実機でしか確かめられない前提は `docs/manual-verification.md`(V01〜V30、P01〜P04)に集約している。デプロイ・実 kintone での確認はユーザーが行う

## [v0.5.0] - 2026-05-15

### Changed

- **deps**: `github.com/youyo/idproxy` v0.4.2 → v0.5.0 にアップグレード
  - 新規 `Config.OnAuthenticated` フックを採用し、OIDC 認証完了直後に kintone OAuth カスケードを実行
  - 新規 `Config.UseStrictPostLoginRedirectValidator()` を常時有効化（open redirect 攻撃対策）
- **mcp**: `internal/idproxy.BuildAuth` シグネチャに OnAuthenticated フック引数を追加（callsite は内部のみ）

### Fixed

- **mcp**: Claude Desktop からの MCP OAuth フローで kintone OAuth が自動カスケードするよう改善（[issue #5](https://github.com/youyo/kintone/issues/5) 二次改善）
  - OIDC 認証完了直後（/callback 内）に kintone OAuth トークンの有無を確認し、未取得なら `/oauth/kintone/start` へ自動 302
  - 初回接続は CALLBACK_PORT timeout で 1 回 retry が必要、2 回目以降は完全自動接続
  - 完全自動化（kintone OAuth 完了後に元の `/authorize?...` へ復帰）は M18 で idproxy 側の `OnAuthenticated(stateData)` 拡張を待って実装予定

### Notes

- kill switch `KINTONE_MCP_DISABLE_OAUTH_CASCADE=1` は M16 cascade middleware と新 OnAuthenticated hook の両方に適用される（process 起動時にのみ評価）
- M16 の `EnsureKintoneOAuthConnected` middleware は per-request フォールバックとして温存（token 期限切れ後の再認証時の保険）

## [v0.4.2] - 2026-05-14

### Fixed

- **mcp**: OIDC コールバックから kintone OAuth への自動カスケードを実装（[issue #5](https://github.com/youyo/kintone/issues/5) 一次修正）
  - `--auth oidc --authz oauth` モードで OIDC ログイン後にブラウザが `/` 404 になるバグを修正
  - `EnsureKintoneOAuthConnected` middleware が OIDC 認証済み + kintone OAuth 未接続のブラウザリクエストを `/oauth/kintone/start` へ自動リダイレクト
  - logvalet の `EnsureBacklogConnected` パターンを参考に実装
  - kill switch: `KINTONE_MCP_DISABLE_OAUTH_CASCADE=1` で旧挙動に戻せる
  - Claude Desktop からの MCP OAuth AS カスケード（`/authorize` → kintone OAuth → `/authorize`）は M17 で対応予定

## [v0.4.2] - 2026-05-14

### Fixed

- **mcp**: OIDC コールバックから kintone OAuth への自動カスケードを実装（[issue #5](https://github.com/youyo/kintone/issues/5) 一次修正）
  - `--auth oidc --authz oauth` モードで OIDC ログイン後にブラウザが `/` 404 になるバグを修正
  - `EnsureKintoneOAuthConnected` middleware が OIDC 認証済み + kintone OAuth 未接続のブラウザリクエストを `/oauth/kintone/start` へ自動リダイレクト
  - logvalet の `EnsureBacklogConnected` パターンを参考に実装
  - kill switch: `KINTONE_MCP_DISABLE_OAUTH_CASCADE=1` で旧挙動に戻せる
  - Claude Desktop からの MCP OAuth AS カスケード（`/authorize` → kintone OAuth → `/authorize`）は M17 で対応予定

## [v0.4.1] - 2026-05-12

### 修正 — MCP serve wiring hardening（M15 / v0.4.1）

- **`mcp serve --authz=oauth` を stdio で指定した場合に fail-fast**（過去は silent no-op で OAuth が無視され API Token で稼働する事故が起こっていた）。`clierr.UsageError` で `USAGE` envelope を返し、復旧手順（`--listen <addr> --auth oidc --authz oauth` への切り替え、または `--authz=oauth` の解除）を含むメッセージを表示する
- **HTTP + authz=oauth で起動時 `buildAPI` を skip**。OAuth 構成では `PrincipalAPIFactory` が per-request にユーザー別トークンから API client を生成するため、固定 API Token を必要としない。config.toml に API Token が無い OAuth 専用デプロイで起動できるようになった
- `internal/mcp/server` に `ErrStdioOAuthUnsupported` typed sentinel を追加し、層分離を維持（user-facing メッセージは CLI 層で構築）
- `runHTTP` に `deps.API == nil && deps.Factory == nil` の defensive check を追加（将来 wiring が変更されたときの NPE 防止）
- 認証マトリクス全 4 経路（stdio+api-token / stdio+oauth / http+api-token / http+oauth）を網羅するテーブルテスト追加
- 後方互換性: stdio + api-token / HTTP + api-token / HTTP + auth=oidc + authz=oauth は完全無影響

### 機能追加 — StateStore 統合 Storage 拡張 + loopback flow 物理削除（M14 / v0.4.0）

- OAuth Authorization Code フロー用 `StateStore` を `internal/store` の 4 backend（memory / sqlite / redis / dynamodb）に統合。`KINTONE_STORE_BACKEND` 単一設定で kintone Token + Cache + idproxy session + OAuth state を同一 backend に格納可能になり、multi-replica MCP サーバ配置に対応
- `Container.StateStore()` メソッドを追加。`internal/mcp/oauthcallback` は `StateStore` / `StateEntry` / `ErrStateNotFound` を `internal/store` への型エイリアスとして再エクスポート（既存 API 後方互換）
- **atomic Take 契約**を全 backend で保証:
  - SQLite: `DELETE ... RETURNING` 単一文（SQLite 3.35+）
  - Redis: HGETALL + DEL を Lua script で atomic 実行
  - DynamoDB: `DeleteItem` with `ReturnValues=ALL_OLD`
  - Memory: `sync.Mutex` + `delete()` のクリティカルセクション
- Conformance テスト `storetest.RunStateStoreConformance` を新設し 4 backend 共通検証（Put/Take roundtrip、不在 Take、空 state、**並行 Take の単一勝者保証 (N=20)**、Close 冪等性）
- SQLite schema に `kintone_oauth_state` テーブルを追加（`schema.sql` は `IF NOT EXISTS` のため既存 DB も自動マイグレーション）
- DynamoDB は単一テーブルに `pk=kintone:oauthstate:<state>` で相乗り（GSI 追加なし）
- Redis は `kintone:oauthstate:<state>` 配下に hash で格納し EXPIRE で自動失効

### 削除 — OAuth loopback サーバ実装（v0.4.0）

- `internal/auth/oauth/flow.go` / `callback.go` / `browser.go` および各 `*_test.go` を物理削除（M13 で deprecated 化済み）
- 関連 sentinel error を削除: `ErrStateMismatch` / `ErrAuthorizationCodeMissing` / `ErrCallbackTimeout` / `ErrInvalidRedirectURL` / `ErrMissingClientCredentials`
- 関連エラーコード `OAUTH_STATE_MISMATCH` / `OAUTH_CALLBACK_TIMEOUT` のマッピングも削除（kintone OAuth は HTTPS redirect_uri 必須のため CLI loopback フローは仕様上不可能であり、MCP サーバホスト型 callback に一本化）
- 保持: `pkce.go` / `state.go` (state generator) / `token.go` / `provider.go` / `refresh.go` / `OAuthError` / `ErrRefreshTokenRevoked` / `ErrTokenExpired`

### 機能追加 — Remote MCP 用サーバホスト型 OAuth callback（M13 / v0.3.0）

- 新規エンドポイント: `mcp serve --listen ... --auth oidc --authz oauth` 起動時に以下を公開
  - `GET /oauth/kintone/start`: OIDC 認証済みユーザーを kintone authorize URL に 302（state cookie 発行 + PKCE S256）
  - `GET /oauth/kintone/callback`: authorization code を token に交換し TokenStore に Domain + PrincipalID + AuthType=oauth で保存
- 認証モデル: AuthZ=oauth では `PrincipalAPIFactory` が per-request にユーザー別 OAuth トークンを引く（M12 で導入済み Storage を活用）
- AUTH_REQUIRED envelope の拡張: 構造化 `AuthRequiredError` を導入し、facade で `details.principal_id` / `details.domain` を含める。`AuthorizeURLBuilder` 注入時は `details.authorize_url` も付与（LLM クライアントが UI に表示してユーザがブラウザで認可可能）
- CSRF 三重保護:
  1. idproxy.Auth.Wrap 経由の OIDC Principal 認証（SameSite=Lax で kintone→callback の top-level GET にも cookie 同伴）
  2. `kintone_oauth_state` cookie と URL state を `subtle.ConstantTimeCompare` で照合
  3. state map の PrincipalID と request Principal の一致確認
- state map: in-memory `MemoryStateStore` + TTL 10 分 + one-shot Take。`StateStore` interface 化により M14 で Storage 拡張可能
- 起動時 fail-fast 検証:
  - `KINTONE_OAUTH_REDIRECT_URL` の HTTPS 強制（`KINTONE_OAUTH_ALLOW_PLAINTEXT_REDIRECT=1` で localhost http のみ opt-in 許容）
  - `KINTONE_OAUTH_REDIRECT_URL == KINTONE_MCP_EXTERNAL_URL + "/oauth/kintone/callback"` の完全一致確認
- E2E: `internal/testsupport/kintonefake` に `/oauth2/authorization` endpoint を追加し、`internal/cli/mcp/serve_e2e_test.go` で start → authorize → callback → Token 永続化を build tag `e2e` で検証
- 削除済みコード: ローカル CLI `kintone auth login` は v0.3.0 で廃止済み（commit 668c33d）

### M13 既知の制約

- state map は in-memory（プロセス再起動・multi-replica で未完了 state は失効、ユーザは再試行が必要）。multi-replica 厳密対応は M14 で Storage backend に拡張予定
- 1 ユーザー × 単一 kintone domain 前提（複数 domain 切替は M14）
- `/oauth/kintone/*` path prefix は M13 では固定（env 化は M14）
- `internal/auth/oauth/{flow,callback}.go` の loopback サーバ部分は deprecated（物理削除は M14）

### BREAKING CHANGES — 認証モデルの整理（v0.3.0）

- **削除**: `kintone auth login` サブコマンド（loopback http フロー）。kintone OAuth が redirect_uri に https を強制する事実が確定したため、ローカル CLI からの OAuth ログインは技術的に成立しないと判断した
- **新方針**: ローカル CLI 利用は **API Token のみ**。OAuth は **リモート MCP サーバ専用**（M13 でサーバホスト型 callback を実装予定）
- **据え置き**: `kintone auth status` / `kintone auth logout` は TokenStore 内のトークン参照・削除のため残す
- **据え置き**: `internal/auth/oauth/` の token exchange / refresh / PKCE / state 生成ロジックは M13 のサーバ側 callback で再利用するため保持

### BREAKING CHANGES — M12 Unified Storage

- 削除した環境変数: `KINTONE_TOKENS_PATH`, `KINTONE_CACHE_PATH`, `KINTONE_CACHE_DISABLE`
- 追加した環境変数: `KINTONE_STORE_BACKEND`, `KINTONE_STORE_SQLITE_DIR`,
  `KINTONE_STORE_REDIS_URL`, `KINTONE_STORE_REDIS_TLS`, `KINTONE_STORE_REDIS_PASSWORD`,
  `KINTONE_STORE_REDIS_INSECURE_PLAINTEXT`,
  `KINTONE_STORE_DYNAMODB_TABLE`, `KINTONE_STORE_DYNAMODB_REGION`,
  `KINTONE_STORE_CACHE_BYPASS`,
  `KINTONE_MCP_SIGNING_KEY_PEM`, `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE`,
  `KINTONE_LOG_LEVEL`
- ファイル配置の変更: 既定の SQLite ファイルが `~/.cache/kintone/{tokens,cache}.db` から
  `~/.local/state/kintone/{kintone,idproxy}.db` に変更
- internal API: `tokenstore` / `cache` パッケージは `store` に統合・削除
- 配布: 全 backend（Memory + SQLite + Redis + DynamoDB）を標準ビルドに同梱（バイナリサイズ ~25MB → ~45MB）
- 動作変更: `auth=oidc` かつ SigningKey が解決できない場合 startup を拒否
- 動作変更: `KINTONE_STORE_BACKEND=memory` × `--auth oidc` の組合せは全面禁止（`STORE_MEMORY_OIDC_FORBIDDEN`）
- CLI 契約: `cache clear --scope <apps|fields|list_apps|all>` は維持 + `--key <prefix>` を追加
- CLI 契約変更: `cache stats` の JSON schema を変更（旧 `db_path/db_exists/db_size_bytes/total/expired` → 新 `backend/location/reachable/entry_count/expired_count/backend_specific`）
- CLI 契約変更: `config show` の `cache_path` フィールドを廃止
- 新コマンド: `kintone store init dynamodb --table NAME --region REGION [--capability full|token|cache|signingkey|idproxy]`
- 新エラー code: `STORE_TABLE_NOT_FOUND` / `STORE_GSI_MISSING` / `STORE_TTL_DISABLED` / `STORE_CONNECTION_FAILED` / `STORE_MEMORY_OIDC_FORBIDDEN` / `STORE_CACHE_BYPASS_INVALID` / `STORE_PLAINTEXT_FORBIDDEN` / `SIGNING_KEY_REQUIRED` / `RESOLVER_PRINCIPAL_NOT_FOUND`
