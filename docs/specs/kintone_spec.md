# kintone CLI / MCP サーバー 超詳細設計仕様書

## 概要
本プロジェクトは、kintone REST API を操作するための統合ツールである。

### 提供機能
- CLI（Cobraベース）
- MCPサーバー（remote対応・multi-user対応）
- OAuth / API Token / idproxy 認証
- SQLiteベースのキャッシュ / TokenStore
- LLMフレンドリーな操作（JSON固定出力）

---

## 設計方針
- API層とLLM向け操作層を分離
- MCPはFacade経由で公開
- JSON出力統一
- multi-user対応
- profile + env override

---

## 技術スタック
- Go / Cobra / SQLite
- OAuth / API Token / idproxy
- CLI / MCP / Container

---

## ディレクトリ構成

```
cmd/kintone
internal/
  cli/
  config/
  auth/
  idproxy/
  store/          ← 統合 Storage（M12～）
    memory/
    sqlite/
    redis/
    dynamodb/
    storetest/
  resolver/
  kintoneapi/
  service/
    api/
    operations/
  mcp/
    server/
    facade/
  output/
```

---

## 設定
### config
~/.config/kintone/config.toml

### 環境変数

```
# 設定
KINTONE_PROFILE
KINTONE_CONFIG_PATH
KINTONE_DOMAIN
KINTONE_AUTH

# 認証
KINTONE_API_TOKEN
KINTONE_OAUTH_CLIENT_ID
KINTONE_OAUTH_CLIENT_SECRET
KINTONE_OAUTH_REDIRECT_URL
KINTONE_OAUTH_SCOPES

# MCP
KINTONE_MCP_AUTH_MODE
KINTONE_MCP_AUTHZ_MODE
KINTONE_MCP_LISTEN_ADDR
KINTONE_MCP_COOKIE_SECRET
KINTONE_MCP_SIGNING_KEY_PEM
KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE

# Storage（KINTONE_STORE_* は env のみ、config.toml には不可）
KINTONE_STORE_BACKEND          # memory / sqlite / redis / dynamodb（既定: sqlite）
KINTONE_STORE_SQLITE_DIR       # SQLite ディレクトリ（既定: ~/.local/state/kintone/）
KINTONE_STORE_REDIS_URL        # Redis URL
KINTONE_STORE_REDIS_TLS        # 1 で redis:// に TLS 強制
KINTONE_STORE_REDIS_PASSWORD   # Redis パスワード
KINTONE_STORE_REDIS_INSECURE_PLAINTEXT  # 1 で非 localhost への平文接続を許可
KINTONE_STORE_CACHE_BYPASS     # 1 でキャッシュのみ無効化
KINTONE_STORE_DYNAMODB_TABLE   # DynamoDB テーブル名
KINTONE_STORE_DYNAMODB_REGION  # DynamoDB リージョン

# ログ
KINTONE_LOG_LEVEL              # debug / info / warn / error（既定: info）
```

優先順位:
CLI > ENV > config（KINTONE_STORE_* は env のみ）

---

## データストア

kintone CLI/MCP は認証情報・キャッシュ・OIDC 状態を単一の Storage に保管する。

### バックエンド種別

| Backend  | 用途                | 物理共有 / 論理分離                      |
|----------|---------------------|----------------------------------------|
| memory   | dev / test          | プロセス内 map（idproxy は別インスタンス）|
| sqlite   | host / single-inst. | 同ディレクトリ・2 ファイル分離（kintone.db + idproxy.db）|
| redis    | k8s / scale-out     | UniversalClient 共有 + kintone:/idproxy: prefix 分離 |
| dynamodb | Lambda / serverless | 単一テーブル + kintone GSI1/GSI2、idproxy は PK のみ |

### キー名前空間

- kintone 側: `kintone:tokens:` / `kintone:cache:` / `kintone:signingkey:`
- idproxy 側: `session:` / `authcode:` / `accesstoken:` / `refreshtoken:` / `client:` / `familyrevoked:`
- 衝突防止: `kintone:` で始まらない PK/key を kintone 自前ストアが書くことを禁止

### TTL

apps / fields / resolver = 1 年

---

## SigningKey 解決順序

1. `KINTONE_MCP_SIGNING_KEY_PEM` 環境変数（PKCS#8 PEM）
2. Storage の SigningKey accessor（`KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` 必須）
3. `auth=none` のみ ephemeral 生成（slog.Warn）
4. `auth=oidc` で 1/2 が解決できなければ fail-fast（`SIGNING_KEY_REQUIRED`）

### Memory backend × auth=oidc は全面禁止

session/auth_code/refresh_token state が memory のためプロセス再起動で全失効、
multi-replica で session が孤立する。`STORE_MEMORY_OIDC_FORBIDDEN` で startup 拒否。

### Threat Model（簡易）

保護対象: OAuth refresh_token / API Token / OIDC SigningKey / idproxy session・refresh_token

at-rest 暗号化はインフラ層に委譲（SQLite=ファイル権限 0o600、Redis=KMS 接続 + ACL、DynamoDB=KMS at rest）。
アプリケーション層 envelope encryption（KMS / Vault 連携）は M13+。

---

## TokenStore

interface:
Get / Put / Delete / ListByDomain

Key:
Domain + PrincipalID + AuthType

---

## Principal
principal_id = provider:sub

---

## 認証
OAuth:
- access_token: 1h
- refresh_token: 無期限
- 自動更新あり

Scope:
record/app/file read/write

---

## MCP認証モデル
Auth:
- none
- oidc

AuthZ:
- oauth
- api-token

---

## レイヤー
CLI/MCP
→ facade
→ operations
→ api
→ client
→ auth
→ cache

---

## 名前解決
App:
1. ID
2. code
3. name
4. partial

Field:
1. code
2. label
3. partial

---

## CLI
kintone
  auth
  api
  ops
  cache
  mcp
  completion

---

## 出力
成功:
{ "ok": true, "data": {} }

エラー:
{ "ok": false, "error": {} }

---

## MCP tools
apps_search
app_describe
records_query
record_create
record_update
record_delete

---

## completion
kintone completion zsh

---

## コンテナ
/data/kintone/cache.db

---

## 完了
この仕様は実装可能である。
