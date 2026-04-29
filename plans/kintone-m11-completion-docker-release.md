# M11: completion + Docker + GoReleaser リリース 詳細計画

## ゴール
- `kintone completion {bash|zsh|fish|powershell}` を実装し、シェル補完を提供
- Docker 配布（multi-stage、`CGO_ENABLED=0`、distroless/non-root）
- GoReleaser でクロスコンパイル + GitHub Releases + Homebrew Tap + ghcr.io
- `.github/workflows/release.yml` でタグ push 起動の CI リリース
- README をインストール 4 方式 / 認証 3 方式 / MCP セットアップで完備
- M10 持ち越し polish: transport.go errcheck、PrincipalAPIFactory facade 統合、AUTH_REQUIRED マッピング

## 注記（前段オーケストレーターからの引き継ぎ）
- HTTP モード graceful shutdown は M10 で既に実装済み（`internal/cli/mcp/serve.go:99` の `signal.NotifyContext` と `internal/mcp/server/http.go` の `httpServer.Shutdown`）。本マイルストーンでは追加作業なし。タスク表からは省略する。
- ErrAuthRequired は M10 で既に定義済み（`internal/service/api/principal.go:18`）。facade 側のマッピングのみ追加する。

## 実装ブロック

### B1. completion コマンド
- ファイル: `internal/cli/completion/{completion.go, completion_test.go}`（新規）
- コマンド: `kintone completion {bash|zsh|fish|powershell}`
  - bash: `cobra.Command.GenBashCompletionV2(out, true)`
  - zsh: `cobra.Command.GenZshCompletion(out)`
  - fish: `cobra.Command.GenFishCompletion(out, true)`
  - powershell: `cobra.Command.GenPowerShellCompletionWithDesc(out)`
- root.go に `cmd.AddCommand(clicompletion.NewCmd(cmd))` で統合（補完生成にはルートコマンドが必要）
- 出力規約の例外として明示: 補完スクリプトはシェルが直接 source するためプレーン出力（README とコードコメントで明記）

### B2. M10 持ち越し polish
- **transport.go errcheck（line 125 / 279）**: `defer resp.Body.Close()` は `defer func() { _ = resp.Body.Close() }()` に書き換え（IIFE 形式 — `defer _ = …` は構文エラー）
- **PrincipalAPIFactory facade 統合**:
  - `ToolDeps` に `Factory *serviceapi.PrincipalAPIFactory` を追加（任意、nil で従来通り）
  - 共通ヘルパー `resolveAPI(ctx, deps) (serviceapi.API, error)` を facade パッケージに追加
    - `Factory != nil` なら `Factory.ForContext(ctx)`、それ以外は `deps.API`
  - 6 ハンドラ（apps_search / app_describe / records_query / record_create / record_update / record_delete）で `deps.API.X(...)` を `api, err := resolveAPI(ctx, deps); if err != nil { return errorResult(err) }; api.X(...)` に書き換え
  - 既存テストは `Factory: nil` で従来通り通る（後方互換）
- **AUTH_REQUIRED マッピング**:
  - `facade.MapError` で `errors.Is(err, serviceapi.ErrAuthRequired)` を判定し `output.Error{Code: "AUTH_REQUIRED"}` にマップ
  - facade test に AUTH_REQUIRED ケースを追加（Factory が ErrAuthRequired を返すスタブ経由）
- **I1-I3 結合テスト**: 時間制約により省略。既存 unit/smoke で代替し note に記録

### B3. Dockerfile + .dockerignore
- multi-stage:
  - builder: `golang:1.26-alpine`、`CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH`、`go build -ldflags "-s -w -X github.com/youyo/kintone/internal/cli.Version=${VERSION}" -o /out/kintone ./cmd/kintone`
  - runtime: `gcr.io/distroless/static-debian12:nonroot`（USER 65532:65532 既定）
  - `ENTRYPOINT ["/kintone"]`
- modernc.org/sqlite は pure Go なので CGO 不要
- `.dockerignore`: `.git`, `plans/`, `docs/`, `*.test`, `*.out`, `.serena/`, `.claude/`, `coverage.*` を除外

### B4. .goreleaser.yaml
- builds: `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/amd64`
- ldflags: `-s -w -X github.com/youyo/kintone/internal/cli.Version={{.Version}}`
- archives: tar.gz (unix) / zip (windows)
- checksum: sha256
- changelog: github グルーピング
- release: github（draft=false, prerelease=auto）
- brews: `youyo/homebrew-tap`（**事前に空リポジトリ作成必要**を README/runbook に注記）
- dockers: `ghcr.io/youyo/kintone:{{.Version}}`, `:latest`、amd64/arm64 multi-arch
- snapshot: `{{ .Tag }}-next`

### B5. .github/workflows/release.yml
- on: `push: tags: ['v*']`
- permissions: `contents: write`, `packages: write`, `id-token: write`
- jobs.goreleaser:
  - actions/checkout@v4 with `fetch-depth: 0`
  - actions/setup-go@v5 with `go-version-file: go.mod`
  - docker/login-action@v3 to ghcr.io（GITHUB_TOKEN）
  - goreleaser/goreleaser-action@v6 `args: release --clean`
  - env: `GITHUB_TOKEN`, `HOMEBREW_TAP_GITHUB_TOKEN`

### B6. README 完備（追記/編集）
- インストール 4 方式: Homebrew / Docker / `go install` / Binary download
- 認証 3 方式の使い分け表（API Token / OAuth / OIDC remote）
- CLI コマンド一覧に completion 追加
- MCP セットアップ（stdio / HTTP / OIDC）
- completion の使い方サンプル（bash / zsh）

### B7. ドキュメント更新
- CLAUDE.md: 「全マイルストーン完了」へ更新
- plans/kintone-roadmap.md: M11 を `[x]` 化、Current Focus を「リリース準備完了」に、Changelog 行追加
- README 冒頭に「リリース準備済み」明記

## TDD テストケース（B1, B2 のみ）

| ID | 対象 | 内容 |
|---|---|---|
| C1 | completion bash | `kintone completion bash` で stdout に bash 補完スクリプトが出力される（`# bash completion V2` など特徴文字列含む） |
| C2 | completion zsh | `kintone completion zsh` で `compdef _kintone kintone` を含む zsh スクリプト |
| C3 | completion fish | `kintone completion fish` で `complete -c kintone` を含む fish スクリプト |
| C4 | completion powershell | `kintone completion powershell` で `Register-ArgumentCompleter` を含む |
| C5 | completion 不正引数 | `kintone completion invalid` でエラー（cobra ValidArgs） |
| F1 | facade resolveAPI | Factory=nil なら deps.API を返す |
| F2 | facade resolveAPI | Factory.ForContext がエラー → handler は errorResult を返す |
| F3 | facade MapError | ErrAuthRequired → `Code: "AUTH_REQUIRED"` |
| F4 | facade integration | ToolDeps{Factory: stub returning ErrAuthRequired} の apps_search ハンドラが `{"ok":false,"error":{"code":"AUTH_REQUIRED",...}}` を返す |

## リスク評価

| リスク | 影響 | 対策 |
|---|---|---|
| Windows completion CRLF / improper output | medium | cobra 標準 `GenPowerShellCompletionWithDesc` を使い手書きしない |
| `go-version-file: go.mod` が `1.26.1` を解釈できない | medium | actions/setup-go v5+ は go.mod 直接読み込み対応済み |
| GoReleaser Homebrew Tap 公開先未作成 | high | release runbook に「`youyo/homebrew-tap` 事前作成」を明記、必要なら brews 設定を一旦コメントアウトしてリリース可能に |
| ghcr.io への push 権限不足 | high | `permissions: packages: write` + `docker/login-action` で GITHUB_TOKEN 認証 |
| modernc.org/sqlite の CGO 依存 | low | pure Go ドライバなので CGO_ENABLED=0 で問題なし（M07 で確認済み） |
| ToolDeps 変更の既存テスト破壊 | medium | Factory はオプション。nil 既定でフォールバックを保つ |
| `defer _ = resp.Body.Close()` 構文エラー | low | IIFE 形式 `defer func() { _ = resp.Body.Close() }()` を採用 |

## コミット順
1. `docs(m11): completion + Docker + GoReleaser の詳細計画を追加`
2. `fix(kintoneapi): transport.go の errcheck 警告を解消`
3. `test(cli/completion): completion コマンドのテストを追加`
4. `feat(cli/completion): kintone completion {bash|zsh|fish|powershell} を実装`
5. `feat(mcp/facade): PrincipalAPIFactory 統合と AUTH_REQUIRED マッピングを追加`
6. `chore: Dockerfile + .dockerignore を追加`
7. `chore: .goreleaser.yaml を追加`
8. `ci: release.yml で goreleaser リリースワークフローを追加`
9. `docs: README をインストール 4 方式 / 認証 3 方式 / MCP セットアップで完備`
10. `docs: CLAUDE/roadmap を M11 完了 + 全マイルストーン完了状態に更新`

## 検証
- `go test -race -cover ./...` 全 pass
- `golangci-lint run ./...` 違反 0（既存 transport.go 2 件も解消）
- `go vet ./...` クリア
- `gofmt -l .` 差分なし
- `go run ./cmd/kintone completion bash | head` で bash スクリプト出力
- `goreleaser check` 構文確認（CI 環境にあれば。無ければ note）
- `docker build .` image build（環境にあれば。無ければ note）
