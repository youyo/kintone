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
