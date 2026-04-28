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
cmd/kintone
internal/
  cli/
  config/
  auth/
  idproxy/
  tokenstore/
  cache/
  resolver/
  kintoneapi/
  service/
    api/
    operations/
  mcp/
    server/
    facade/
  output/

---

## 設定
### config
~/.config/kintone/config.toml

### 環境変数
KINTONE_PROFILE
KINTONE_CONFIG_PATH
KINTONE_CACHE_PATH
KINTONE_DOMAIN
KINTONE_AUTH

KINTONE_OAUTH_CLIENT_ID
KINTONE_OAUTH_CLIENT_SECRET
KINTONE_OAUTH_REDIRECT_URL

KINTONE_API_TOKEN

KINTONE_MCP_AUTH_MODE
KINTONE_MCP_AUTHZ_MODE

優先順位:
CLI > ENV > config

---

## キャッシュ
パス:
~/.cache/kintone/cache.db

TTL:
apps / fields / resolver = 1年

---

## TokenStore
interface:
Get / Put / Delete

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
