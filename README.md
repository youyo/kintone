# kintone CLI

kintone API を操作するための CLI ツールです。
全コマンドは LLM・パイプ処理に適した JSON 形式で結果を出力します。

## インストール

```bash
go install github.com/youyo/kintone/cmd/kintone@latest
```

または手動ビルド:

```bash
git clone https://github.com/youyo/kintone.git
cd kintone
go build -o /usr/local/bin/kintone ./cmd/kintone
```

## 使い方

### バージョン確認

```bash
$ kintone version
{"ok":true,"data":{"version":"0.1.0"}}

$ kintone version --short
0.1.0
```

### ヘルプ

```bash
$ kintone --help
```

### 設定（config）

設定は以下の優先順位で解決されます:

1. CLI フラグ（`--profile`, `--config`）
2. 環境変数（`KINTONE_*`）
3. `~/.config/kintone/config.toml`

#### 設定ファイルを初期化

```bash
$ kintone config init
{"ok":true,"data":{"path":"/Users/foo/.config/kintone/config.toml","created":true}}
```

既に存在する場合はエラー（`CONFIG_ALREADY_EXISTS`）になります。上書きしたい場合は `--force` を付けてください。
パスは `--config` または `KINTONE_CONFIG_PATH` で変更できます。

#### 設定ファイルの例

```toml
[default_profile]
name = "default"

[profiles.default]
domain = "example.cybozu.com"
auth   = "api-token"

[profiles.dev]
domain = "dev.cybozu.com"
auth   = "oauth"
```

> 注意: API Token / OAuth client secret などの機微情報は config.toml に書かないでください。
> 環境変数（`KINTONE_API_TOKEN` など）経由で渡すのが推奨です。

#### 現在の設定を表示

```bash
$ kintone config show
{"ok":true,"data":{"profile":"default","domain":"example.cybozu.com","auth":"api-token","api_token":"***","config_path":"/Users/foo/.config/kintone/config.toml","cache_path":"","source":{"profile":"file","domain":"file","auth":"file"}}}
```

`api_token` は機微情報のためマスク表示（`***`）されます。`source` 各フィールドは値の出所（`cli` / `env` / `file` / `default`）を表します。

特定の profile を指定する場合:

```bash
$ kintone config show --profile dev
```

#### 環境変数

| 変数 | 用途 |
|------|------|
| `KINTONE_PROFILE` | 使用する profile 名 |
| `KINTONE_CONFIG_PATH` | config.toml のパス |
| `KINTONE_CACHE_PATH` | cache db のパス（デフォルト: `~/.cache/kintone/cache.db`、コンテナ: `/data/kintone/cache.db`） |
| `KINTONE_CACHE_DISABLE` | `1` で SQLite キャッシュを無効化（API を毎回直叩き） |
| `KINTONE_DOMAIN` | kintone ドメイン（例: `example.cybozu.com`） |
| `KINTONE_AUTH` | 認証モード（`api-token` / `oauth`） |
| `KINTONE_API_TOKEN` | API Token |
| `KINTONE_OAUTH_CLIENT_ID` | OAuth クライアント ID |
| `KINTONE_OAUTH_CLIENT_SECRET` | OAuth クライアントシークレット（config.toml より環境変数推奨） |
| `KINTONE_OAUTH_REDIRECT_URL` | OAuth redirect URI（例: `http://127.0.0.1:18080/callback`）。kintone OAuth クライアント登録と完全一致させること |
| `KINTONE_OAUTH_SCOPES` | OAuth スコープ（スペース区切り。省略時: `k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write`） |

## API Token 認証

`KINTONE_API_TOKEN` 環境変数または config.toml の `api_token` フィールドで API Token を指定します。
`internal/auth` パッケージが `X-Cybozu-API-Token` ヘッダを自動付与します。

## OAuth 認証（M09）

kintone の OAuth 2.0 Authorization Code Grant + PKCE フローに対応しています。
`KINTONE_AUTH=oauth` に設定することで、API Token の代わりに OAuth アクセストークンが使用されます。
アクセストークンの期限切れ（通常 1h）は自動検知し、リフレッシュトークンで透過的に更新します。

### 事前設定

1. kintone 管理画面で OAuth クライアントを登録し、redirect URI に `http://127.0.0.1:<ポート>/callback` を設定する
2. クライアント ID / シークレット / redirect URI を環境変数に設定する:

```bash
export KINTONE_DOMAIN=example.cybozu.com
export KINTONE_AUTH=oauth
export KINTONE_OAUTH_CLIENT_ID=your-client-id
export KINTONE_OAUTH_CLIENT_SECRET=your-client-secret
export KINTONE_OAUTH_REDIRECT_URL=http://127.0.0.1:18080/callback
```

### ログイン（認可コードフロー）

```bash
$ kintone auth login --oauth --principal-id oauth:alice
```

- ブラウザが起動し kintone の認可画面が開きます
- ユーザーが同意すると `http://127.0.0.1:<port>/callback` にリダイレクトされ、アクセストークンが取得されます
- 取得されたトークンは `~/.cache/kintone/tokens.db` に保存されます（ファイル権限 0600）

ブラウザが起動できない環境（SSH / CI）では `--no-browser` フラグで認可 URL を stderr に出力できます:

```bash
$ kintone auth login --oauth --principal-id oauth:alice --no-browser
```

複数ユーザーが同一ドメインを使う場合は `--principal-id` を個別に指定します:

```bash
$ kintone auth login --oauth --principal-id oauth:bob
```

> 注意: `--principal-id` は TokenStore のキーです。同一ドメインの別ユーザーは必ず異なる値を指定してください。
> M10 (OIDC 対応) で自動取得に切り替わる予定です。

### ログイン結果確認

```bash
$ kintone auth status
{"ok":true,"data":{"entries":[{"principal_id":"oauth:alice","expires_at":"2026-04-29T10:00:00Z","has_refresh_token":true,"scope":"k:app_record:read k:app_record:write...","masked_token":"abcd...wxyz"}]}}
```

特定ユーザーを絞り込む場合:

```bash
$ kintone auth status --principal-id oauth:alice
```

### ログアウト

特定ユーザーのトークン削除:

```bash
$ kintone auth logout --principal-id oauth:alice
{"ok":true,"data":{"deleted":1}}
```

ドメイン内の全 OAuth トークン削除:

```bash
$ kintone auth logout --all
{"ok":true,"data":{"deleted":3}}
```

### セキュリティ上の注意

- `KINTONE_OAUTH_CLIENT_SECRET` は環境変数での管理を推奨します（config.toml への記載は非推奨）
- `kintone config show` の出力で `oauth_client_secret` は `***` にマスクされます
- `kintone auth status` の出力でアクセストークンは先頭 4 文字 + `...` + 末尾 4 文字にマスクされます
- tokens.db はファイル権限 0600 で保存されますが、平文保存です（M11 で暗号化予定）
- PKCE (S256) と CSRF state 検証を実施します（`crypto/rand` による生成、`subtle.ConstantTimeCompare` による検証）

## kintoneapi クライアント

`internal/kintoneapi` パッケージは `net/http` 薄ラッパーとして実装されています。

- `Client`: ベース URL / auth / リトライ設定を保持する REST クライアント
- `Transport`: `http.RoundTripper` ラッパー（認証ヘッダ付与・エラーパース）
- `APIError`: kintone 標準エラー（`code` / `id` / `message` / `HTTPStatus` / `RetryAfter`）を構造化
- エンドポイント: `GET /k/v1/records.json`, `/k/v1/record.json`, `/k/v1/app.json`, `/k/v1/app/form/fields.json`
- Retry-After ヘッダ対応（429 レート制限時の待機時間を自動解析）

## API サブコマンド（`kintone api ...`）

kintone REST API を直接叩く透過コマンド群です。出力は JSON 固定で、LLM / `jq` 連携を想定しています。

> 内部構造: `service/api` 層が REST エンドポイントを 1:1 で透過し、`service/operations` 層が LLM 向けに合成・整形します。CLI は `kintoneapi` を直接 import せず、必ず service 層を経由します。

事前に環境変数で domain / API Token を渡してください:

```bash
export KINTONE_DOMAIN=example.cybozu.com
export KINTONE_AUTH=api-token
export KINTONE_API_TOKEN=xxxxxxxxxxxxxxxxxxxx
```

### レコード一覧

```bash
$ kintone api records get --app 1 --query 'name = "foo"' --field name --field age --total-count
{"ok":true,"data":{"records":[{...}],"total_count":3}}

# code / name / partial で App を指定可（M08 名前解決）
$ kintone api records get --app-ref sales --query 'createdAt > LAST_WEEK()'
{"ok":true,"data":{"records":[{...}]}}
```

| フラグ | 型 | 必須 | 説明 |
|--------|---|------|------|
| `--app` | int64 | △ | kintone アプリ ID（数値直指定、`--app-ref` と排他） |
| `--app-ref` | string | △ | アプリ参照（数値文字列 / code / name / partial、`--app` と排他、M08） |
| `--query` | string | - | kintone クエリ言語 |
| `--field` | string（複数指定可） | - | レスポンスを絞り込むフィールドコード |
| `--total-count` | bool | - | true で `total_count` を含める |

`--app` と `--app-ref` は **どちらか必須・両方指定は USAGE エラー**。

> `--field` は **複数フラグ繰り返し**で指定します（`--field name --field age`）。カンマ区切りはサポートしません。

### レコード単件

```bash
$ kintone api record get --app 1 --id 5
{"ok":true,"data":{"record":{...}}}
```

### アプリ情報（snake_case 統一）

```bash
$ kintone api app get --app 1
{"ok":true,"data":{"app_id":"1","code":"myapp","name":"テスト",...}}
```

### フィールド定義

```bash
$ kintone api app fields --app 1 --lang ja
{"ok":true,"data":{"properties":{"name":{"type":"SINGLE_LINE_TEXT",...}},"revision":"5"}}
```

### アプリ + フィールドの合成（operations 経由）

```bash
$ kintone api app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

LLM がアプリ全体像を 1 回の呼び出しで把握できるよう、`app.json` と `app/form/fields.json` を合成して返します。

## Ops サブコマンド（`kintone ops ...`）

LLM 向けの意味付けされたレコード CRUD と app 記述。書き込み系は `--dry-run` で送信予定リクエスト body を検証できます。

> 内部構造: `service/operations` 層が `service/api` 越しに kintoneapi を呼びます。
> CLI は `kintoneapi` を直接 import せず、必ず service 層を経由します。
> 書き込み系（POST/PUT/DELETE）はデフォルトで **リトライ無効**（多重作成リスク回避）です。

### レコード新規登録

単件:
```bash
$ kintone ops record create --app 1 --record-json '{"name":{"value":"foo"}}'
{"ok":true,"data":{"ids":[100],"revisions":[1]}}
```

複数件:
```bash
$ kintone ops record create --app 1 --records-json '[{"name":{"value":"a"}},{"name":{"value":"b"}}]'
{"ok":true,"data":{"ids":[101,102],"revisions":[1,1]}}
```

dry-run（API を呼ばずリクエスト body のみ確認）:
```bash
$ kintone ops record create --app 1 --record-json '{"name":{"value":"foo"}}' --dry-run
{"ok":true,"data":{"dry_run":true,"method":"POST","path":"/k/v1/records.json","body":{"app":1,"records":[{"name":{"value":"foo"}}]}}}
```

| フラグ | 型 | 必須 | 説明 |
|--------|---|------|------|
| `--app` | int64 | △ | kintone アプリ ID（`--app-ref` と排他） |
| `--app-ref` | string | △ | アプリ参照（数値文字列 / code / name / partial、`--app` と排他、M08） |
| `--record-json` | string | △ | 単件レコード JSON |
| `--records-json` | string | △ | 複数件レコード JSON 配列 |
| `--dry-run` | bool | - | true で API を呼ばず送信予定 body を出力 |

`--app` と `--app-ref` は **どちらか必須・両方指定は USAGE エラー**。
`--record-json` と `--records-json` は **どちらか必須・両方指定は USAGE エラー**。

### レコード単件更新

ID 指定:
```bash
$ kintone ops record update --app 1 --id 7 --record-json '{"name":{"value":"updated"}}'
{"ok":true,"data":{"revision":3}}
```

updateKey（ユニークフィールド）指定:
```bash
$ kintone ops record update --app 1 --update-key-field code --update-key-value A1 --record-json '{"name":{"value":"updated"}}'
```

楽観ロック（`--revision`）:
```bash
$ kintone ops record update --app 1 --id 7 --revision 2 --record-json '{"name":{"value":"x"}}'
```

| フラグ | 型 | 必須 | 説明 |
|--------|---|------|------|
| `--app` | int64 | △ | kintone アプリ ID（`--app-ref` と排他） |
| `--app-ref` | string | △ | アプリ参照（数値文字列 / code / name / partial、`--app` と排他、M08） |
| `--id` | int64 | △ | 更新対象レコード ID |
| `--update-key-field` | string | △ | updateKey: フィールドコード（`--update-key-field-ref` と排他） |
| `--update-key-field-ref` | string | △ | updateKey: フィールド参照（label / partial、M08） |
| `--update-key-value` | string | △ | updateKey: 値（updateKey 経路で必須） |
| `--record-json` | string | ◎ | 更新内容 JSON |
| `--revision` | int64 | - | 楽観ロック用 revision |
| `--dry-run` | bool | - | 送信予定 body のみ出力 |

`--app` と `--app-ref` は **どちらか必須・両方指定は USAGE エラー**。
`--id` と `--update-key-*` は **排他**。どちらかが必須です。
`--update-key-field` と `--update-key-field-ref` は **排他**。

`--update-key-field-ref` を指定すると、まず App ID 解決後に label / partial で field code が解決されます。
ambiguous 時は `RESOLVER_FIELD_AMBIGUOUS` で候補を `details.candidates` に返します。

### レコード削除

```bash
$ kintone ops record delete --app 1 --id 7 --id 8
{"ok":true,"data":{"deleted":2}}
```

revisions 付き（楽観ロック）:
```bash
$ kintone ops record delete --app 1 --id 7 --id 8 --revision 3 --revision 4
```

dry-run:
```bash
$ kintone ops record delete --app 1 --id 7 --id 8 --dry-run
{"ok":true,"data":{"dry_run":true,"method":"DELETE","path":"/k/v1/records.json","body":{"app":1,"ids":[7,8]}}}
```

| フラグ | 型 | 必須 | 説明 |
|--------|---|------|------|
| `--app` | int64 | △ | kintone アプリ ID（`--app-ref` と排他） |
| `--app-ref` | string | △ | アプリ参照（数値文字列 / code / name / partial、`--app` と排他、M08） |
| `--id` | int64（複数指定可） | ◎ | 削除対象レコード ID（`--id 1 --id 2`） |
| `--revision` | int64（複数指定可） | - | 楽観ロック用 revision（`--id` と同要素数） |
| `--dry-run` | bool | - | 送信予定 body のみ出力 |

### アプリ記述（ops 配下にも公開）

`kintone api app describe` と等価です。LLM が `ops` 名前空間下から発見できるよう同一 operations を再公開しています。

```bash
$ kintone ops app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

## 名前解決（Resolver / M08）

CLI / MCP の `--app` 引数（および `--update-key-field`）に **数値 ID 以外**の参照を渡せます。

### App の解決順序

1. **ID 直接**: `"42"` のような数値文字列はそのまま App ID として採用
2. **code 完全一致**: `ListApps?codes[]=ref` で完全一致を検索
3. **name 完全一致**: `ListApps?name=ref` の結果から `Name == ref` を抽出
4. **name 部分一致**: 同レスポンスから `Name` に `ref` が含まれるものを抽出

各段階でヒットしたら即採用（fallback しない / predictability 優先）。
ambiguous（複数ヒット）時は `RESOLVER_APP_AMBIGUOUS` を返し `details.candidates` に全候補を含めます。

### Field の解決順序

1. **code 完全一致**: `properties` のキー一致
2. **label 完全一致**: properties 全走査
3. **label 部分一致**: `strings.Contains` で抽出

### 利用例

```bash
# code で App を解決
$ kintone api records get --app-ref sales --query 'createdAt > LAST_WEEK()'
{"ok":true,"data":{"records":[...]}}

# name 部分一致で複数ヒット → ambiguous
$ kintone api app describe --app-ref 営業
{"ok":false,"error":{"code":"RESOLVER_APP_AMBIGUOUS","message":"...","details":{"kind":"app","ref":"営業","candidates":[{"id":"42","code":"sales","name":"営業 A"},{"id":"55","code":"sales2","name":"営業 B"}]}}}

# field を label で解決して updateKey 経由更新
$ kintone ops record update --app-ref sales --update-key-field-ref 顧客名 --update-key-value 山田 --record-json '{"phone":{"value":"080-..."}}'
```

### キャッシュとの統合

Resolver は `service/api.API`（M07 の CachingAPI でラップ済み）越しに `ListApps` / `GetFormFields` を呼びます。
1 年 TTL でキャッシュされるため、同じ ref を 2 回引いた場合の REST 呼び出しは 1 回。

名前変更時の追従:
- 1 年間は古い名前で resolve され続けるため、`kintone cache clear --scope=apps` で手動更新してください。

### 後方互換

既存の `--app <int>` 直指定は変更なしで動作します。
MCP の `app: number` 引数も継続して受理します（`Required` のみ外し、`app_ref: string` を追加）。

---

## キャッシュ管理（`kintone cache ...`）

kintone API の app / field 情報を SQLite にキャッシュし、繰り返しリクエストを削減します。
TokenStore は OAuth アクセストークンを安全に保存・管理します。

### キャッシュの統計確認

```bash
$ kintone cache stats
{"ok":true,"data":{"db_path":"/Users/foo/.cache/kintone/cache.db","exists":true,"size_bytes":49152,"entry_count":12,"expired_count":0}}
```

DB ファイルが存在しない場合は `exists: false` の統計を返します。

### キャッシュの削除

```bash
$ kintone cache clear
{"ok":true,"data":{"cleared":true,"deleted_count":12}}
```

特定キーパターンのみ削除する場合は `--key` フラグを使用します（省略時は全件削除）。

| フラグ | 型 | 必須 | 説明 |
|--------|---|------|------|
| `--key` | string | - | 削除対象キーのプレフィックス（省略時は全件削除） |

## MCP サーバー（`kintone mcp serve`）

LLM クライアント（Claude Desktop など）から kintone を操作するための
MCP（Model Context Protocol）サーバーを起動します。

### モード

- `--listen` 未指定: **stdio JSON-RPC**（既定。Claude Desktop 等の子プロセス起動向け）
- `--listen :8080`: **HTTP / Streamable**（remote MCP・複数クライアントで共有）

### 認証フラグ

| フラグ / 環境変数 | 値 | 既定 | 役割 |
|---|---|---|---|
| `--listen` / `KINTONE_MCP_LISTEN_ADDR` | `host:port` | 空 | 空で stdio、値で HTTP |
| `--auth` / `KINTONE_MCP_AUTH_MODE` | `none` / `oidc` | `none` | リクエスト前段の認証 |
| `--authz` / `KINTONE_MCP_AUTHZ_MODE` | `api-token` / `oauth` | `api-token` | upstream kintone への認証 |

### stdio + API Token（既存・後方互換）

```bash
$ KINTONE_DOMAIN=example.cybozu.com \
  KINTONE_AUTH=api-token \
  KINTONE_API_TOKEN=xxxx \
  kintone mcp serve
```

### HTTP + OIDC + multi-user（M10 から）

`auth=oidc` 時は [github.com/youyo/idproxy](https://github.com/youyo/idproxy) v0.4.2 を組み込み、
リクエストごとに OIDC ベースの Bearer JWT を検証して `principal_id = "<issuer>:<sub>"` を抽出します。
upstream kintone への OAuth トークンは事前に各ユーザーが
`kintone auth login --oauth --principal-id "<issuer>:<sub>"` で TokenStore に登録しておく必要があります。

```bash
$ KINTONE_DOMAIN=example.cybozu.com \
  KINTONE_AUTH=oauth \
  KINTONE_OAUTH_CLIENT_ID=... \
  KINTONE_OAUTH_CLIENT_SECRET=... \
  \
  KINTONE_MCP_OIDC_ISSUER=https://accounts.google.com \
  KINTONE_MCP_OIDC_CLIENT_ID=... \
  KINTONE_MCP_OIDC_CLIENT_SECRET=... \
  KINTONE_MCP_EXTERNAL_URL=https://mcp.example.com \
  KINTONE_MCP_COOKIE_SECRET=$(openssl rand -hex 32) \
  \
  kintone mcp serve --listen :8080 --auth oidc --authz oauth
```

エンドポイント:
- `POST /mcp` — Streamable HTTP transport（MCP クライアントからのメイン呼び出し）
- `/login`, `/callback`, `/select`, `/.well-known/*`, `/authorize`, `/token`, `/register` — idproxy 予約パス

> **MVP 範囲**: M10 では idproxy の SigningKey は起動時に ephemeral 生成されます（再起動で発行済み JWT が無効化）。永続鍵対応は M11+ 予定。
> **プロビジョニング**: 各ユーザーの kintone refresh_token は事前 CLI ログインで TokenStore に登録します。MCP 内からの自動 OAuth 誘導は M11+ の対象です。


提供する 6 つの tools:

| ツール名 | 説明 |
|---------|------|
| `apps_search` | アプリを ids/codes/name/space_ids/limit/offset で検索 |
| `app_describe` | 単一アプリの基本情報 + フォームのフィールド定義を取得（`app` または `app_ref`） |
| `records_query` | kintone クエリでレコード一覧を取得（query / fields / total_count、`app` または `app_ref`） |
| `record_create` | レコード新規登録（record / records 排他、最大 100 件、`app` または `app_ref`） |
| `record_update` | レコード単件更新（id / update_key_* 排他、楽観ロック対応、`app`+`app_ref`、`update_key_field`+`update_key_field_ref` 排他） |
| `record_delete` | レコード複数件削除（revisions 任意、`app` または `app_ref`） |

`apps_search` 以外の全 tool は **M08 から `app_ref: string` 引数を追加**しました。
`app: number` と `app_ref: string` は排他（どちらか必須）。
`record_update` は `update_key_field_ref: string` も追加（label / partial で field code を解決）。

各 tool の出力は CLI と同じ JSON envelope（`{"ok":true,"data":{...}}` /
`{"ok":false,"error":{...}}`）を `CallToolResult.Content[0].Text` に格納します。
LLM 側から `JSON.parse` するだけで CLI と同じ意味論で結果を扱えます。

### Claude Desktop での設定例（macOS）

`~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "kintone": {
      "command": "kintone",
      "args": ["mcp", "serve"],
      "env": {
        "KINTONE_DOMAIN": "example.cybozu.com",
        "KINTONE_AUTH": "api-token",
        "KINTONE_API_TOKEN": "xxxxxxxxxxxxxxxxxxxx"
      }
    }
  }
}
```

> stdio モードでは認証モードは `api-token` / `oauth`（単一ユーザー）に対応。
> HTTP + OIDC による multi-user remote MCP は M10 から対応（上記参照）。
> CLI / 単一ユーザーの OAuth 認証は `kintone auth login --oauth` で利用可能です（M09 以降）。

## JSON 出力規約

全コマンドは以下の形式で stdout に出力します。

**成功時**:
```json
{"ok":true,"data":{...}}
```

**失敗時**:
```json
{"ok":false,"error":{"code":"USAGE","message":"..."}}
```

`jq` でのパース例:

```bash
$ kintone version | jq -r '.data.version'
0.1.0
```

## ロードマップ

詳細は [plans/kintone-roadmap.md](plans/kintone-roadmap.md) を参照してください。

## ライセンス

[MIT License](LICENSE)
