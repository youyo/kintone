# Changelog

## [Unreleased]

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
