# 技術スタック

## 言語・ランタイム
- Go 1.26（`mise.toml` で固定。`mise install` で導入）

## 主要ライブラリ（採用済み）
- CLI: `github.com/spf13/cobra` v1.8 系
- MCP SDK: `github.com/mark3labs/mcp-go`（公式 Go MCP SDK）
- kintone REST クライアント: **自作**（`net/http` 薄ラッパー、外部 SDK は使わない）
- TOML 設定: 着手時に選定（候補: `BurntSushi/toml` または `pelletier/go-toml`）
- SQLite: 着手時に選定（候補: `modernc.org/sqlite`（CGO 不要）または `mattn/go-sqlite3`）

## 配布チャネル
- `go install` + GitHub Releases（GoReleaser）
- Homebrew Tap（GoReleaser 連携）
- Docker イメージ（ghcr.io）
- GitHub Actions CI（test / vet / golangci-lint / release）

## 設計上の選定理由
- mark3labs/mcp-go: stdio/http 両対応・remote 実装が容易
- net/http 自作: 依存最小化・テスト容易・必要 API のみ型付き
- SQLite: ローカルキャッシュ・TokenStore 用。マルチテーブルだが軽量
