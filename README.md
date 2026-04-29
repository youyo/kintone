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
