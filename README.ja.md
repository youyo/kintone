[English](README.md) | 日本語

# kintone CLI

kintone API を操作するための CLI ツール / MCP サーバーです。
全コマンドは LLM・パイプ処理に適した JSON 形式で結果を出力します。

## 特徴

- **JSON 固定出力**: 成功時 `{"ok":true,"data":{...}}` / 失敗時 `{"ok":false,"error":{...}}` で統一。`jq` / LLM パイプライン連携が容易
- **3 種類の認証**: API Token / OAuth 2.0（PKCE 対応）/ OIDC remote（idproxy 組み込み）
- **MCP サーバー**: stdio / HTTP Streamable transport の両対応。Claude Desktop など LLM クライアントから直接 kintone を操作
- **名前解決**: アプリ・フィールドを ID だけでなく code / name / label の部分一致でも参照可能
- **SQLite キャッシュ**: アプリ・フィールド情報を 1 年 TTL でキャッシュし REST 呼び出しを削減
- **クロスプラットフォーム配布**: Homebrew / Docker (multi-arch) / `go install` / GitHub Releases バイナリ

## インストール

以下の 4 方式から選べます。

### 1. Homebrew（macOS / Linux）

```bash
brew install youyo/tap/kintone
```

### 2. Docker（multi-arch: amd64 / arm64）

```bash
docker pull ghcr.io/youyo/kintone:latest
docker run --rm ghcr.io/youyo/kintone:latest version
```

`~/.config/kintone` と `~/.local/state/kintone` をマウントして使うと便利です:

```bash
docker run --rm -it \
  -v "$HOME/.config/kintone:/home/nonroot/.config/kintone" \
  -v "$HOME/.local/state/kintone:/home/nonroot/.local/state/kintone" \
  ghcr.io/youyo/kintone:latest api records get --app 1
```

### 3. `go install`

```bash
go install github.com/youyo/kintone/cmd/kintone@latest
```

### 4. GitHub Releases バイナリ

[Releases](https://github.com/youyo/kintone/releases) から OS / arch に合った tar.gz / zip を取得し展開してください。
SHA256 checksum (`checksums.txt`) で整合性を検証できます。

### ソースからビルド（開発者向け）

```bash
git clone https://github.com/youyo/kintone.git
cd kintone
go build -o /usr/local/bin/kintone ./cmd/kintone
```

## 認証方式の使い分け

| 方式 | 想定ユース | 必要設定 | コマンド |
|---|---|---|---|
| **API Token** | ローカル CLI / シングルユーザ MCP（stdio） / シンプルな自動化 | `KINTONE_DOMAIN` + `KINTONE_API_TOKEN`（または config.toml） | `kintone api/ops/mcp serve`（既定） |
| **OAuth 2.0**（Remote MCP のみ） | リモート MCP サーバ上でユーザ個別認可 / マルチユーザ | OAuth クライアント登録（redirect_uri = MCP サーバの公開 https URL）+ サーバ側 callback フロー（M13 で実装済み） | リモート MCP の Web フローでユーザがブラウザ承認 → サーバが TokenStore に保存 |
| **OIDC remote (idproxy)** | リモート MCP サーバ + 複数ユーザ + ID プロバイダ連携 | OIDC Issuer / Client / Cookie Secret 等 | `kintone mcp serve --listen :8080 --auth oidc --authz oauth` |

> **注意（v0.3.0 以降）**: kintone OAuth は redirect_uri に https を強制するため、ローカル CLI の loopback フロー（`kintone auth login --oauth`）は廃止されました。**ローカル CLI 利用は API Token のみ**、**OAuth はリモート MCP サーバ経由**の二系統に整理されています。

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
| `KINTONE_DOMAIN` | kintone ドメイン（例: `example.cybozu.com`） |
| `KINTONE_AUTH` | 認証モード（`api-token` / `oauth`） |
| `KINTONE_API_TOKEN` | API Token |
| `KINTONE_OAUTH_CLIENT_ID` | OAuth クライアント ID |
| `KINTONE_OAUTH_CLIENT_SECRET` | OAuth クライアントシークレット（config.toml より環境変数推奨） |
| `KINTONE_OAUTH_REDIRECT_URL` | OAuth redirect URI（例: `http://127.0.0.1:18080/callback`）。kintone OAuth クライアント登録と完全一致させること |
| `KINTONE_OAUTH_SCOPES` | OAuth スコープ（スペース区切り。省略時: `k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write`） |
| `KINTONE_STORE_BACKEND` | Storage バックエンド（`memory` / `sqlite` / `redis` / `dynamodb`。既定: `sqlite`） |
| `KINTONE_STORE_SQLITE_DIR` | SQLite ファイルを置くディレクトリ（既定: `~/.local/state/kintone/`。`kintone.db` と `idproxy.db` を配置） |
| `KINTONE_STORE_REDIS_URL` | Redis URL（DB index 含む。`redis://` または `rediss://`。backend=redis 時は必須） |
| `KINTONE_STORE_REDIS_TLS` | `1` で `redis://` 接続に TLS を強制（`rediss://` は常時 TLS） |
| `KINTONE_STORE_REDIS_PASSWORD` | Redis 認証パスワード（URL に含めない場合） |
| `KINTONE_STORE_REDIS_INSECURE_PLAINTEXT` | `1` で非 localhost への `redis://` 平文接続を明示的に許可（既定: `0`。セキュリティ上の理由でデフォルト無効） |
| `KINTONE_STORE_CACHE_BYPASS` | `1` でキャッシュのみ無効化（TokenStore / SigningKey は通常通り動作。`KINTONE_CACHE_DISABLE` の後継） |
| `KINTONE_STORE_DYNAMODB_TABLE` | DynamoDB テーブル名（backend=dynamodb 時は必須） |
| `KINTONE_STORE_DYNAMODB_REGION` | DynamoDB リージョン（AWS SDK のリージョン解決にフォールバック） |
| `KINTONE_MCP_SIGNING_KEY_PEM` | OIDC JWT 署名鍵の PKCS#8 PEM 文字列（Storage より優先。本番の `auth=oidc` では必須） |
| `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE` | `1` で Storage への署名鍵自動生成を許可（dev / test 専用。本番非推奨） |
| `KINTONE_LOG_LEVEL` | ログレベル（`debug` / `info` / `warn` / `error`。既定: `info`） |

## Storage バックエンド

kintone CLI/MCP は認証情報・キャッシュ・OIDC 状態を 1 つの Storage バックエンドに保管します。

| Backend  | 用途                          | 主要 ENV                                              |
|----------|-------------------------------|------------------------------------------------------|
| memory   | dev / test                    | `KINTONE_STORE_BACKEND=memory`                       |
| sqlite   | host / single-instance（既定）| `KINTONE_STORE_SQLITE_DIR=...`                       |
| redis    | k8s / Fargate scale-out       | `KINTONE_STORE_REDIS_URL=rediss://...`               |
| dynamodb | Lambda / serverless           | `KINTONE_STORE_DYNAMODB_TABLE=...`                   |

全 backend で kintone TokenStore / Cache / OIDC SigningKey / OAuth State / idproxy session・refresh_token が
1 接続で共存します（key prefix で論理分離。SQLite のみ同ディレクトリ・2 ファイル分離）。

M14 以降、OAuth Authorization Code フローの `StateStore` も同じ Storage backend に統合されました。
multi-replica な MCP サーバ配置では `sqlite` / `redis` / `dynamodb` を選択してください（`memory` は
`auth=oidc` 時に起動時拒否されます）。

### Backend 別設定例

```bash
# Memory（dev / test）
export KINTONE_STORE_BACKEND=memory

# SQLite（既定）
export KINTONE_STORE_BACKEND=sqlite
export KINTONE_STORE_SQLITE_DIR=$HOME/.local/state/kintone

# Redis（k8s / Fargate）
export KINTONE_STORE_BACKEND=redis
export KINTONE_STORE_REDIS_URL=rediss://prod-redis.example.com:6380/0
export KINTONE_STORE_REDIS_PASSWORD=$REDIS_PASSWORD

# DynamoDB（Lambda / serverless）
export KINTONE_STORE_BACKEND=dynamodb
export KINTONE_STORE_DYNAMODB_TABLE=kintone-prod
export KINTONE_STORE_DYNAMODB_REGION=ap-northeast-1
```

### DynamoDB セットアップ

事前にテーブルを作成してください:

```bash
aws dynamodb create-table --table-name kintone-prod \
  --attribute-definitions \
    AttributeName=pk,AttributeType=S \
    AttributeName=gsi1pk,AttributeType=S AttributeName=gsi1sk,AttributeType=S \
    AttributeName=gsi2pk,AttributeType=S AttributeName=gsi2sk,AttributeType=S \
  --key-schema AttributeName=pk,KeyType=HASH \
  --global-secondary-indexes '[
    {"IndexName":"gsi1","KeySchema":[{"AttributeName":"gsi1pk","KeyType":"HASH"},{"AttributeName":"gsi1sk","KeyType":"RANGE"}],"Projection":{"ProjectionType":"ALL"}},
    {"IndexName":"gsi2","KeySchema":[{"AttributeName":"gsi2pk","KeyType":"HASH"},{"AttributeName":"gsi2sk","KeyType":"RANGE"}],"Projection":{"ProjectionType":"KEYS_ONLY"}}
  ]' \
  --billing-mode PAY_PER_REQUEST

aws dynamodb update-time-to-live --table-name kintone-prod \
  --time-to-live-specification 'Enabled=true,AttributeName=ttl'

# kintone CLI で検証:
kintone store init dynamodb --table kintone-prod --region ap-northeast-1
```

最小 IAM ポリシー（Scan は不要、Query/BatchWriteItem のみ）:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": [
      "dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:DeleteItem",
      "dynamodb:Query", "dynamodb:BatchWriteItem", "dynamodb:DescribeTable",
      "dynamodb:DescribeTimeToLive"
    ],
    "Resource": [
      "arn:aws:dynamodb:*:*:table/kintone-prod",
      "arn:aws:dynamodb:*:*:table/kintone-prod/index/*"
    ]
  }]
}
```

### Redis 推奨 ACL（最小権限）

```
ACL SETUSER kintone on >password \
    ~kintone:* ~idproxy:* \
    +@read +@write +@string +@hash +@scripting -@admin -@dangerous
```

### MCP Secret 2 種

`KINTONE_MCP_COOKIE_SECRET`（cookie 暗号化）と `KINTONE_MCP_SIGNING_KEY_PEM`（OIDC JWT 署名鍵）は
Storage と独立に管理してください。`auth=oidc` で SigningKey が解決できない場合 startup を拒否します。

dev/test では `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` で Storage 自動生成を許可できます
（本番では非推奨。`KINTONE_MCP_SIGNING_KEY_PEM` の外部供給を推奨）。

### Multi-user MCP の principal_id 規約

OIDC を使う MCP HTTP サーバーでは、ユーザーの principal_id は `<issuer>:<sub>` 形式で TokenStore に保存されます。
例: `https://accounts.google.com:1234567890`

サーバ側 OAuth callback（M13）が OIDC の `sub` クレームを TokenStore キーに自動マッピングするため、
ユーザー側の手動プロビジョニング操作は不要です。

### Memory backend の制約

`KINTONE_STORE_BACKEND=memory` と `--auth oidc` の組み合わせは startup で拒否されます
（`STORE_MEMORY_OIDC_FORBIDDEN`）。memory backend ではプロセス再起動で state が失われ、
multi-replica 環境で session が孤立するためです。dev で `auth=oidc` を試す場合は
`KINTONE_STORE_BACKEND=sqlite` + `KINTONE_STORE_SQLITE_DIR=$(mktemp -d)` を推奨します。

## API Token 認証

`KINTONE_API_TOKEN` 環境変数または config.toml の `api_token` フィールドで API Token を指定します。
`X-Cybozu-API-Token` ヘッダが自動付与されます。

API Token の発行方法は [kintone REST API 共通仕様 — 認証](https://cybozu.dev/ja/kintone/docs/rest-api/overview/authentication/) を参照してください。

## OAuth 認証（リモート MCP サーバ専用）

kintone の OAuth 2.0 Authorization Code Grant + PKCE（S256）フローに対応していますが、
**v0.3.0 以降、ローカル CLI からの OAuth ログインは廃止されました**。

理由: kintone OAuth は redirect_uri に **https を強制**します（loopback http 不可）。
ローカル CLI が自己署名 https サーバを立てる方式は UX が悪く、配布形態（CLI バイナリ）にも合わないため、
OAuth は**リモート MCP サーバ上のみ**で利用する設計に整理しました。

### 認証モデル

| 利用形態 | 認証 |
|---|---|
| ローカル CLI（`api`/`ops`/`mcp serve` stdio） | API Token |
| リモート MCP サーバ（`mcp serve --listen ...`） | OAuth（サーバホスト型 callback、M13 実装済み） |

### サーバホスト型 OAuth フロー（M13）

リモート MCP サーバを `https://mcp.example.com` で公開する想定で、以下が登録すべき OAuth クライアント設定です:

- **redirect URI**: `https://mcp.example.com/oauth/kintone/callback`（公開 https / 全ユーザ共通 / 完全一致）
- **scopes**: `k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write`
- **OAuth クライアント追加手順**: [cybozu developer network — OAuth クライアントを追加する](https://cybozu.dev/ja/common/docs/oauth-client/add-client/)

MCP サーバの必須環境変数:

- `KINTONE_OAUTH_CLIENT_ID` / `KINTONE_OAUTH_CLIENT_SECRET`
- `KINTONE_OAUTH_REDIRECT_URL`（HTTPS、`KINTONE_MCP_EXTERNAL_URL + /oauth/kintone/callback` と完全一致）
- `KINTONE_MCP_EXTERNAL_URL`（idproxy 用と兼用）
- 任意: `KINTONE_OAUTH_SCOPES`
- 任意（dev 用）: `KINTONE_OAUTH_ALLOW_PLAINTEXT_REDIRECT=1` で `http://localhost` を opt-in 許容

公開エンドポイント（`auth=oidc + authz=oauth` 時）:

| Path | 役割 |
|------|------|
| `GET /oauth/kintone/start` | OIDC 認証済みユーザーを kintone authorize URL に 302（PKCE S256 + state cookie） |
| `GET /oauth/kintone/callback` | authorization code を token に交換し、TokenStore に `Domain + PrincipalID + AuthType=oauth` で保存 |

フロー:

1. ユーザーが MCP サーバに OIDC でログイン（`--auth oidc`）
2. MCP ツール呼び出し時に kintone トークン未取得が判定された場合、サーバは `AUTH_REQUIRED` envelope を返し `details.authorize_url` を含める
3. LLM クライアントが URL を提示 → ユーザーがブラウザで開く
4. MCP サーバの `/oauth/kintone/start` が kintone authorize に 302 リダイレクト
5. ユーザーが kintone で認可 → `/oauth/kintone/callback` が code を受信し token に交換、TokenStore に保存
6. 以降の MCP 呼び出しは `PrincipalAPIFactory` が自動でユーザー別 token を取得し、refresh も透過的に実行

セキュリティ:

- CSRF 三重保護: idproxy OIDC Principal + `kintone_oauth_state` cookie + state map の PrincipalID 比較（constant-time）
- state TTL: 10 分、one-shot（OAuth 2.0 仕様準拠）
- state map は M14 で `internal/store` の 4 backend（memory / sqlite / redis / dynamodb）に統合され、`KINTONE_STORE_BACKEND` の選択でそのまま multi-replica MCP に対応できます（memory backend は `auth=oidc` 時に起動拒否）

> 1 サーバ＝1 kintone domain 前提（multi-domain 切替は将来対応）。

> ローカル CLI で OAuth トークンを直接取得する手段は提供しません。`kintone auth status` / `kintone auth logout` は、リモート MCP サーバが TokenStore に保存したトークンを参照・削除するための補助コマンドとして残しています。

### ログイン状態の確認

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
- `kintone.db`（SQLite バックエンド）はファイル権限 0600 で保存されます（平文保存）
- PKCE (S256) と CSRF state 検証を実施します（`crypto/rand` で生成、`subtle.ConstantTimeCompare` で検証）

## API サブコマンド（`kintone api ...`）

kintone REST API を直接叩く透過コマンド群です。出力は JSON 固定で、LLM / `jq` 連携を想定しています。

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

# code / name / partial で App を指定可（名前解決）
$ kintone api records get --app-ref sales --query 'createdAt > LAST_WEEK()'
{"ok":true,"data":{"records":[{...}]}}
```

| フラグ | 型 | 必須 | 説明 |
|--------|---|------|------|
| `--app` | int64 | △ | kintone アプリ ID（数値直指定、`--app-ref` と排他） |
| `--app-ref` | string | △ | アプリ参照（数値文字列 / code / name / partial、`--app` と排他） |
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

### アプリ + フィールドの合成

```bash
$ kintone api app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

LLM がアプリ全体像を 1 回の呼び出しで把握できるよう、`app.json` と `app/form/fields.json` を合成して返します。

## Ops サブコマンド（`kintone ops ...`）

LLM 向けの意味付けされたレコード CRUD と app 記述。書き込み系は `--dry-run` で送信予定リクエスト body を検証できます。

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
| `--app-ref` | string | △ | アプリ参照（数値文字列 / code / name / partial、`--app` と排他） |
| `--record-json` | string | △ | 単件レコード JSON |
| `--records-json` | string | △ | 複数件レコード JSON 配列 |
| `--dry-run` | bool | - | true で API を呼ばず送信予定 body を出力 |

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
| `--app-ref` | string | △ | アプリ参照（数値文字列 / code / name / partial、`--app` と排他） |
| `--id` | int64 | △ | 更新対象レコード ID |
| `--update-key-field` | string | △ | updateKey: フィールドコード（`--update-key-field-ref` と排他） |
| `--update-key-field-ref` | string | △ | updateKey: フィールド参照（label / partial） |
| `--update-key-value` | string | △ | updateKey: 値（updateKey 経路で必須） |
| `--record-json` | string | ◎ | 更新内容 JSON |
| `--revision` | int64 | - | 楽観ロック用 revision |
| `--dry-run` | bool | - | 送信予定 body のみ出力 |

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
| `--app-ref` | string | △ | アプリ参照（数値文字列 / code / name / partial、`--app` と排他） |
| `--id` | int64（複数指定可） | ◎ | 削除対象レコード ID（`--id 1 --id 2`） |
| `--revision` | int64（複数指定可） | - | 楽観ロック用 revision（`--id` と同要素数） |
| `--dry-run` | bool | - | 送信予定 body のみ出力 |

### アプリ記述（ops 配下にも公開）

`kintone api app describe` と等価です。LLM が `ops` 名前空間下から発見できるよう同一 operations を再公開しています。

```bash
$ kintone ops app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

## 名前解決（Resolver）

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

Resolver は内部で `ListApps` / `GetFormFields` を SQLite キャッシュ越しに呼び出します。
1 年 TTL でキャッシュされるため、同じ ref を 2 回引いた場合の REST 呼び出しは 1 回。

名前変更に追従するには `kintone cache clear` で手動更新してください。

## キャッシュ管理（`kintone cache ...`）

kintone API の app / field 情報を SQLite にキャッシュし、繰り返しリクエストを削減します。

### キャッシュ統計

```bash
$ kintone cache stats
{"ok":true,"data":{"db_path":"/Users/foo/.cache/kintone/cache.db","exists":true,"size_bytes":49152,"entry_count":12,"expired_count":0}}
```

DB ファイルが存在しない場合は `exists: false` の統計を返します。

### キャッシュ削除

```bash
$ kintone cache clear
{"ok":true,"data":{"cleared":true,"deleted_count":12}}
```

特定キーパターンのみ削除する場合は `--key` フラグを使用します（省略時は全件削除）。

| フラグ | 型 | 必須 | 説明 |
|--------|---|------|------|
| `--key` | string | - | 削除対象キーのプレフィックス（省略時は全件削除） |

## シェル補完（`kintone completion`）

bash / zsh / fish / powershell の補完スクリプトを生成します。

> **出力規約の例外**: completion 出力はシェルが直接 source / Invoke-Expression するため、
> 通常の JSON envelope（`{"ok":true,...}`）には包まずプレーンスクリプトを stdout に出力します。
> `version --short` と同列の例外として明示しています。

### bash

```bash
# Linux
kintone completion bash | sudo tee /etc/bash_completion.d/kintone > /dev/null

# macOS（Homebrew）
kintone completion bash > "$(brew --prefix)/etc/bash_completion.d/kintone"
```

### zsh

```bash
kintone completion zsh > "${fpath[1]}/_kintone"
# 反映
autoload -U compinit && compinit
```

### fish

```bash
kintone completion fish > ~/.config/fish/completions/kintone.fish
```

### PowerShell

`$PROFILE` に追記:

```powershell
kintone completion powershell | Out-String | Invoke-Expression
```

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

### サポートされる組み合わせ（M15）

| Transport | `--auth` | `--authz`   | 状態                                                            |
|-----------|----------|-------------|-----------------------------------------------------------------|
| stdio     | `none`   | `api-token` | OK（既定）                                                      |
| stdio     | `none`   | `oauth`     | **USAGE エラー**（OAuth は per-request principal binding が必須で HTTP transport を要する） |
| stdio     | `oidc`   | any         | USAGE エラー（OIDC は HTTP transport を要する）                  |
| HTTP      | `none`   | `api-token` | OK（信頼 LAN 内）                                                |
| HTTP      | `oidc`   | `api-token` | OK（multi-user で共通 API Token）                                |
| HTTP      | `oidc`   | `oauth`     | OK（multi-user で per-user kintone OAuth）                       |

### stdio + API Token

```bash
$ KINTONE_DOMAIN=example.cybozu.com \
  KINTONE_AUTH=api-token \
  KINTONE_API_TOKEN=xxxx \
  kintone mcp serve
```

### HTTP + OIDC + multi-user

`auth=oidc` 時は [github.com/youyo/idproxy](https://github.com/youyo/idproxy) を組み込み、
リクエストごとに OIDC ベースの Bearer JWT を検証して `principal_id = "<issuer>:<sub>"` を抽出します。
upstream kintone への OAuth トークンは、サーバホスト型 OAuth callback フロー（M13 で実装済み）が
OIDC `sub` をキーに TokenStore に自動保存します。ユーザーは初回 MCP 呼び出し時にブラウザで kintone 認可するだけで利用可能になります。

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

> idproxy の SigningKey は `KINTONE_MCP_SIGNING_KEY_PEM`（env）または Storage から永続解決されます。`auth=oidc` 時に SigningKey が解決できない場合 startup を拒否します（`SIGNING_KEY_REQUIRED`）。dev では `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` で自動生成を許可できます。

### 提供する 6 つの tools

| ツール名 | 説明 |
|---------|------|
| `apps_search` | アプリを ids/codes/name/space_ids/limit/offset で検索 |
| `app_describe` | 単一アプリの基本情報 + フォームのフィールド定義を取得（`app` または `app_ref`） |
| `records_query` | kintone クエリでレコード一覧を取得（query / fields / total_count、`app` または `app_ref`） |
| `record_create` | レコード新規登録（record / records 排他、最大 100 件、`app` または `app_ref`） |
| `record_update` | レコード単件更新（id / update_key_* 排他、楽観ロック対応、`app`+`app_ref`、`update_key_field`+`update_key_field_ref` 排他） |
| `record_delete` | レコード複数件削除（revisions 任意、`app` または `app_ref`） |

`apps_search` 以外の全 tool は `app: number` と `app_ref: string` のいずれかを排他必須で受け付けます。
`record_update` は `update_key_field_ref: string` も受け付けます（label / partial で field code を解決）。

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

> stdio モードでは `api-token` 認証のみ対応します。stdio に `--authz=oauth` を指定すると
> 起動時に USAGE エラーで拒否されます（OAuth は per-request principal binding が必須で
> HTTP transport を要するため）。multi-user OAuth は上記の `--listen <addr> --auth oidc
> --authz oauth` を使ってください。

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

例外: `kintone completion` および `kintone version --short` はシェル / 人間向けにプレーン文字列を出力します。

## リリース手順（メンテナ向け）

タグを push すると `.github/workflows/release.yml` が起動し、
GitHub Releases / Homebrew Tap / ghcr.io へ成果物を配布します。

```bash
# 1. main の最終確認
git checkout main && git pull
go test -race ./... && golangci-lint run ./...

# 2. タグ作成と push
git tag v0.1.0
git push origin v0.1.0
```

## ライセンス

[MIT License](LICENSE)
