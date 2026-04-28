# 開発コマンド集

## 環境セットアップ
```bash
mise install                       # Go 1.26 をインストール
go mod init github.com/youyo/kintone   # M01 で初回のみ
go mod tidy                        # 依存解決
```

## ビルド・実行（M01 完了後に有効）
```bash
go build ./...                     # 全パッケージビルド
go build -o bin/kintone ./cmd/kintone   # バイナリ生成
go run ./cmd/kintone version       # JSON 出力で動作確認
go run ./cmd/kintone version --short    # プレーン出力
```

## テスト
```bash
go test ./...                      # 全テスト
go test -v ./internal/output       # 単一パッケージ詳細出力
go test -run TestSuccess ./internal/output    # 特定テスト関数
go test -race ./...                # レース検出
go test -cover ./...               # カバレッジ
go test -coverprofile=cover.out ./... && go tool cover -html=cover.out  # HTML カバレッジ
```

## 静的解析・フォーマット
```bash
go vet ./...
gofmt -w .                         # フォーマット適用
goimports -w .                     # import 整理
golangci-lint run                  # lint 全体
golangci-lint run --fix            # 自動修正
```

## devflow スキル（プロジェクト推奨ワークフロー）
```
/devflow:plan          # 単一マイルストーンの詳細計画
/devflow:implement     # 単一マイルストーンを実装
/devflow:cycle         # 未完了マイルストーンを連続自律実行
/devflow:roadmap       # ロードマップ更新・追加
```

## Git
```bash
git status
git diff
git log --oneline -20
git checkout -b feat/m01-project-skeleton    # ブランチ命名規則: 単一文字の前にハイフン禁止
git add <files>                    # `-A` や `.` は使わず明示的に指定
git commit -m "feat: <内容>"       # 日本語 Conventional Commits
```

## macOS（Darwin）特有の注意
- BSD 系 `sed` / `find` のため、GNU 形式オプションが効かない場合がある
- `sed -i` は引数必要: `sed -i '' 's/old/new/g' file`
- `find -regex` は GNU 拡張不可。`find -E` で拡張正規表現
- ファイルシステムは大文字小文字を区別しない（HFS+/APFS デフォルト）

## ファイル操作
```bash
ls -la
find . -name "*.go" -not -path "./vendor/*"
grep -rn "KINTONE_" internal/      # 環境変数の参照箇所探索
rg "KINTONE_"                      # ripgrep（あれば高速）
```

## リリース（M11 完了後）
```bash
goreleaser release --clean         # リリース実行
goreleaser release --snapshot --clean   # ローカルスナップショット
git tag v0.1.0 && git push origin v0.1.0  # タグプッシュで CI 起動
```

## Docker（M11 完了後）
```bash
docker build -t ghcr.io/youyo/kintone:dev .
docker run --rm -v ~/.config/kintone:/root/.config/kintone ghcr.io/youyo/kintone:dev mcp serve
```
