# Changelog

## [Unreleased]

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
