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
| `--app` | int64 | ◎ | kintone アプリ ID |
| `--record-json` | string | △ | 単件レコード JSON |
| `--records-json` | string | △ | 複数件レコード JSON 配列 |
| `--dry-run` | bool | - | true で API を呼ばず送信予定 body を出力 |

`--record-json` と `--records-json` は **どちらか必須・両方指定は USAGE エラー** です。

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
| `--app` | int64 | ◎ | kintone アプリ ID |
| `--id` | int64 | △ | 更新対象レコード ID |
| `--update-key-field` / `--update-key-value` | string | △ | updateKey 指定（id と排他） |
| `--record-json` | string | ◎ | 更新内容 JSON |
| `--revision` | int64 | - | 楽観ロック用 revision |
| `--dry-run` | bool | - | 送信予定 body のみ出力 |

`--id` と `--update-key-*` は **排他**。どちらかが必須です。

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
| `--app` | int64 | ◎ | kintone アプリ ID |
| `--id` | int64（複数指定可） | ◎ | 削除対象レコード ID（`--id 1 --id 2`） |
| `--revision` | int64（複数指定可） | - | 楽観ロック用 revision（`--id` と同要素数） |
| `--dry-run` | bool | - | 送信予定 body のみ出力 |

### アプリ記述（ops 配下にも公開）

`kintone api app describe` と等価です。LLM が `ops` 名前空間下から発見できるよう同一 operations を再公開しています。

```bash
$ kintone ops app describe --app 1 --lang ja
{"ok":true,"data":{"app":{"app_id":"1","name":"テスト",...},"fields":{...},"revision":"5"}}
```

## MCP サーバー（`kintone mcp serve`）

LLM クライアント（Claude Desktop など）から kintone を操作するための
MCP（Model Context Protocol）サーバーを stdio JSON-RPC モードで起動します。

```bash
$ KINTONE_DOMAIN=example.cybozu.com \
  KINTONE_AUTH=api-token \
  KINTONE_API_TOKEN=xxxx \
  kintone mcp serve
```

提供する 6 つの tools:

| ツール名 | 説明 |
|---------|------|
| `apps_search` | アプリを ids/codes/name/space_ids/limit/offset で検索 |
| `app_describe` | 単一アプリの基本情報 + フォームのフィールド定義を取得 |
| `records_query` | kintone クエリでレコード一覧を取得（query / fields / total_count） |
| `record_create` | レコード新規登録（record / records 排他、最大 100 件） |
| `record_update` | レコード単件更新（id / update_key_* 排他、楽観ロック対応） |
| `record_delete` | レコード複数件削除（revisions 任意） |

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

> 認証モードは現在 `api-token` のみ対応。OAuth / multi-user remote
> サーバーは M09 以降で対応予定です。

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
