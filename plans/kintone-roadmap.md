# Roadmap: kintone CLI / MCP サーバー

## Meta
| 項目 | 値 |
|------|---|
| ゴール | kintone REST API を操作する統合 CLI / MCP サーバーをリリース可能な品質で提供する |
| 成功基準 | (1) CLI から API Token + OAuth で record CRUD と app_describe が実行可能 (2) MCP サーバーが remote/multi-user で動作 (3) GoReleaser/Homebrew/Docker で配布可能 (4) JSON 固定出力 (5) TDD によるユニットテスト網羅 |
| 制約 | Go 1.26 / 仕様書（docs/specs/kintone_spec.md）準拠 / multi-user 対応 / profile + env override / 配布形態 4 種 |
| 対象リポジトリ | /Users/youyo/src/github.com/youyo/kintone |
| 作成日 | 2026-04-29 |
| 最終更新 | 2026-04-29 09:15 |
| ステータス | 進行中（M01 完了） |

## Current Focus
- **マイルストーン**: M2: config 層（toml + env + profile）
- **直近の完了**: M01 — プロジェクト雛形 + JSON 出力規約（feat/m01-project-skeleton ブランチ）
- **次のアクション**: M02 着手（`/devflow:plan` で詳細計画 → `/devflow:implement`）

## Progress

### M1: プロジェクト雛形 + JSON 出力規約 ✅ 完了
- [x] go.mod 作成（module github.com/youyo/kintone, go 1.26）
- [x] cmd/kintone/main.go（Cobra root）
- [x] internal/cli/{root.go, version.go, errors.go, *_test.go}
- [x] internal/output/{json.go, json_test.go}（成功/エラー JSON フォーマット統一）
- [x] .github/workflows/ci.yml（go test / go vet / golangci-lint）
- [x] README.md / LICENSE (MIT) / .gitignore / .golangci.yml
- [x] 実行確認: `go run ./cmd/kintone version` で `{"ok":true,"data":{"version":"0.1.0"}}` 出力
- [x] 全テスト pass（output 85.0% / cli 90.9% カバレッジ）
- 詳細: plans/kintone-m01-project-skeleton.md
- ブランチ: feat/m01-project-skeleton（main へ merge 待ち）

### M2: config 層（toml + env + profile）
- [ ] internal/config/{config.go, profile.go, env.go, *_test.go}
- [ ] ~/.config/kintone/config.toml ローダー
- [ ] 優先順位: CLI > ENV > config 実装
- [ ] KINTONE_PROFILE / KINTONE_CONFIG_PATH / KINTONE_DOMAIN / KINTONE_AUTH 等環境変数
- [ ] CLI: `kintone config show` / `kintone config init`
- 詳細: 着手時に /devflow:plan で生成

### M3: kintoneapi クライアント + API Token 認証
- [ ] internal/auth/{apitoken.go, *_test.go}
- [ ] internal/kintoneapi/{client.go, transport.go, errors.go, *_test.go}（net/http 薄ラッパー）
- [ ] エンドポイント: GET /k/v1/records.json, GET /k/v1/record.json, GET /k/v1/app.json, GET /k/v1/app/form/fields.json
- [ ] レート制限・リトライ（Retry-After 対応）
- [ ] httptest によるモック動作確認
- 詳細: 着手時生成

### M4: service 層（read 系 operations）+ CLI api コマンド
- [ ] internal/service/api/{*.go}（薄い API 透過層）
- [ ] internal/service/operations/{records_query.go, app_describe.go, *_test.go}（LLM 向け抽象化）
- [ ] internal/cli/api/{records.go, app.go}
- [ ] 動作確認: 実 kintone 環境で record query が JSON で返る
- 詳細: 着手時生成

### M5: CLI ops コマンド（write 系 + describe）
- [ ] operations: record_create / record_update / record_delete / app_describe（fields 含む）
- [ ] internal/cli/ops/{record.go, app.go}
- [ ] バリデーション（必須項目・型）
- [ ] dry-run フラグ
- 詳細: 着手時生成

### M6: MCP サーバー雛形 + Facade 層
- [ ] internal/mcp/server/{stdio.go, *_test.go}（mark3labs/mcp-go）
- [ ] internal/mcp/facade/{tools.go, apps_search.go, app_describe.go, records_*.go}
- [ ] CLI: `kintone mcp serve` (stdio mode)
- [ ] 6 つの MCP tools 実装: apps_search, app_describe, records_query, record_create/update/delete
- [ ] 動作確認: Claude Desktop からの接続テスト
- 詳細: 着手時生成

### M7: SQLite キャッシュ + TokenStore
- [ ] internal/cache/{sqlite.go, schema.sql, ttl.go, *_test.go}（modernc.org/sqlite または mattn/go-sqlite3）
- [ ] internal/tokenstore/{store.go, sqlite.go, *_test.go}（Get/Put/Delete, Key=Domain+PrincipalID+AuthType）
- [ ] CLI: `kintone cache clear` / `kintone cache stats`
- [ ] TTL: apps/fields/resolver=1 年
- [ ] パス: ~/.cache/kintone/cache.db (host) / /data/kintone/cache.db (container)
- 詳細: 着手時生成

### M8: Resolver（名前解決）
- [ ] internal/resolver/{app.go, field.go, *_test.go}
- [ ] App: ID → code → name → partial の順
- [ ] Field: code → label → partial の順
- [ ] キャッシュ統合
- [ ] CLI/MCP からの透過利用
- 詳細: 着手時生成

### M9: OAuth 認証 + 自動更新
- [ ] internal/auth/oauth/{flow.go, refresh.go, *_test.go}
- [ ] access_token: 1h / refresh_token: 無期限 / 自動更新あり
- [ ] Scope: record/app/file read/write
- [ ] KINTONE_OAUTH_CLIENT_ID/SECRET/REDIRECT_URL
- [ ] CLI: `kintone auth login --oauth` / `kintone auth status`
- [ ] PKCE 対応・state 検証
- 詳細: 着手時生成

### M10: idproxy + multi-user MCP（remote/oidc）
- [ ] internal/idproxy/{provider.go, oidc.go, *_test.go}
- [ ] principal_id = provider:sub
- [ ] MCP Auth: none / oidc
- [ ] MCP AuthZ: oauth / api-token
- [ ] KINTONE_MCP_AUTH_MODE / KINTONE_MCP_AUTHZ_MODE
- [ ] HTTP/SSE remote MCP サーバー
- [ ] multi-user TokenStore 連携
- 詳細: 着手時生成

### M11: completion + Docker + GoReleaser リリース
- [ ] CLI: `kintone completion {bash|zsh|fish|powershell}`
- [ ] Dockerfile（multi-stage build, alpine base）
- [ ] .goreleaser.yaml（cross-compile + GitHub Releases + Homebrew Tap + ghcr.io）
- [ ] .github/workflows/release.yml（タグプッシュで起動）
- [ ] README 完備（インストール 4 方式 / 認証 3 方式 / CLI コマンド一覧 / MCP セットアップ）
- 詳細: 着手時生成

## Blockers
なし

## Architecture Decisions
| # | 決定 | 理由 | 日付 |
|---|------|------|------|
| 1 | MCP SDK は mark3labs/mcp-go を採用 | Go エコシステムで最も使われており stdio/http 両対応・remote 実装が容易 | 2026-04-29 |
| 2 | kintone REST クライアントは net/http 薄ラッパーを自作 | 依存最小化・テスト容易・必要な API のみ型付きで実装 | 2026-04-29 |
| 3 | TDD 必須（Red → Green → Refactor） | 認証・キャッシュ・Resolver など複雑ロジックの品質担保 | 2026-04-29 |
| 4 | 垂直スライス進行（M1 ごとに動く成果物） | 大規模仕様に対し早期動作確認とフィードバック反映を優先 | 2026-04-29 |
| 5 | API Token を先行実装、OAuth/idproxy は後段 | 動作確認が早期に可能・実装難易度を平準化 | 2026-04-29 |
| 6 | キャッシュ/Resolver は M7-M8 で導入 | データ整合性問題を避けるため CLI/MCP の主要機能が動作確認後 | 2026-04-29 |

## Changelog
| 日時 | 種別 | 内容 |
|------|------|------|
| 2026-04-29 07:55 | 作成 | ロードマップ初版作成（インタビューに基づき M1-M11 を確定） |
| 2026-04-29 09:15 | 進捗 | M01 完了（feat/m01-project-skeleton ブランチで 9 コミット）。devflow:cycle の Planner→devils-advocate→advocate(2 周)→advisor()→implementer(TDD)→手動動作確認まで一気通貫で実施。Current Focus を M02 に更新 |
