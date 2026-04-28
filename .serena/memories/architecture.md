# アーキテクチャ

## 層構造（仕様書 docs/specs/kintone_spec.md より）

```
CLI / MCP
   ↓
facade        ← MCP 公開層（internal/mcp/facade）
   ↓
operations    ← LLM 向け抽象化（internal/service/operations）
   ↓
api           ← 薄い API 透過層（internal/service/api）
   ↓
client        ← net/http 自作の REST クライアント（internal/kintoneapi）
   ↓
auth          ← API Token / OAuth / idproxy（internal/auth, internal/idproxy）
   ↓
cache         ← SQLite キャッシュ・TokenStore（internal/cache, internal/tokenstore）
```

## 予定ディレクトリ構成

```
cmd/kintone/                # main.go
internal/
  cli/                      # Cobra コマンド群
  config/                   # config.toml + env override + profile
  auth/                     # API Token / OAuth
  idproxy/                  # OIDC プロキシ（multi-user 用）
  tokenstore/               # トークン永続化（Domain+PrincipalID+AuthType）
  cache/                    # SQLite キャッシュ
  resolver/                 # 名前解決（App/Field）
  kintoneapi/               # REST クライアント（net/http 薄ラッパー）
  service/
    api/                    # 薄い API 透過層
    operations/             # LLM 向け抽象化
  mcp/
    server/                 # stdio / HTTP / SSE
    facade/                 # MCP tools 実装
  output/                   # JSON 固定出力
```

## 横断的な設計原則

### JSON 固定出力
- 成功: `{"ok":true,"data":{...}}`
- 失敗: `{"ok":false,"error":{"code":"...","message":"...","details":{...}}}`
- `internal/output` パッケージに統一
- 例外: `completion`、`version --short` などプレーン出力は明示する

### 設定優先順位
- CLI フラグ > 環境変数 (`KINTONE_*`) > `~/.config/kintone/config.toml`
- profile + env override 構造

### multi-user 対応
- TokenStore キー: `Domain + PrincipalID + AuthType`
- `principal_id = provider:sub`

### キャッシュ
- ホスト: `~/.cache/kintone/cache.db`
- コンテナ: `/data/kintone/cache.db`
- TTL: apps / fields / resolver = 1 年

### 名前解決（Resolver）
- App: `ID → code → name → partial` の順
- Field: `code → label → partial` の順

### MCP 認証モデル
- Auth: `none | oidc`
- AuthZ: `oauth | api-token`

## 環境変数（仕様書より）
```
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
```

## CLI コマンド階層（予定）
```
kintone
  auth        # login / logout / status
  api         # 薄い API 透過呼び出し
  ops         # LLM 向け抽象化操作
  cache       # clear / stats
  mcp         # serve（stdio / http）
  completion  # bash/zsh/fish/powershell
```

## MCP tools（予定）
- apps_search
- app_describe
- records_query
- record_create
- record_update
- record_delete
