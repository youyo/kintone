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
| `KINTONE_CACHE_PATH` | cache db のパス（M07 で利用） |
| `KINTONE_DOMAIN` | kintone ドメイン（例: `example.cybozu.com`） |
| `KINTONE_AUTH` | 認証モード（`api-token` / `oauth`） |
| `KINTONE_API_TOKEN` | API Token |

## API Token 認証

`KINTONE_API_TOKEN` 環境変数または config.toml の `api_token` フィールドで API Token を指定します。
`internal/auth` パッケージが `X-Cybozu-API-Token` ヘッダを自動付与します。

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
```

| フラグ | 型 | 必須 | 説明 |
|--------|---|------|------|
| `--app` | int64 | ◎ | kintone アプリ ID |
| `--query` | string | - | kintone クエリ言語 |
| `--field` | string（複数指定可） | - | レスポンスを絞り込むフィールドコード |
| `--total-count` | bool | - | true で `total_count` を含める |

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
