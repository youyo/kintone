# M14: StateStore 統合 Storage 拡張 + loopback flow 物理削除

## 目的
M13 で in-memory に閉じた OAuth `StateStore` を `internal/store` の 4 backend に統合し、`KINTONE_STORE_BACKEND` 単一設定で kintone Token + Cache + idproxy session + OAuth state を同一 backend に格納できるようにする（multi-replica MCP 対応）。
あわせて M13 で deprecated 化した OAuth loopback サーバを物理削除する。

> **NOTE**: Agent/Task tool が当該環境で未供給のため、`devflow:planning-agent` / `devils-advocate` / `advocate` / `implementer` / `code-reviewer` の subagent 4 段ループは省略し、`advisor()` 1 回相当の品質ゲートを内包したまま直接実装する。完了報告でこの差異を明記する。

## 設計の核

### A. StateStore 配置

正準定義を **`internal/store/state.go`** に新設する（types.go ではなく独立ファイル）。

```go
package store

type StateEntry struct {
    State       string
    PrincipalID string
    Verifier    string
    Method      string
    CreatedAt   time.Time
}

type StateStore interface {
    Put(ctx context.Context, entry StateEntry) error
    Take(ctx context.Context, state string) (*StateEntry, error)
    Close() error
}

var ErrStateNotFound = errors.New("store: oauth state not found")
const DefaultStateTTL = 10 * time.Minute
```

`internal/mcp/oauthcallback/state.go` は **型エイリアス + sentinel 再エクスポート + MemoryStateStore の薄いラッパー** に縮退する:

```go
type StateStore = store.StateStore        // alias
type StateEntry = store.StateEntry        // alias
var ErrStateNotFound = store.ErrStateNotFound
const DefaultStateTTL = store.DefaultStateTTL
// NewMemoryStateStore は memory backend の StateStore を返す（test 用後方互換）
func NewMemoryStateStore(opts ...MemoryStateStoreOption) store.StateStore { ... }
```

これにより `handler.go` / `handler_test.go` の `StateStore` / `StateEntry` / `ErrStateNotFound` 参照は無修正。

### B. 各 backend での実装

#### Memory: `internal/store/memory/state.go`
- 既存 `oauthcallback.MemoryStateStore` のロジックを移植
- `Container.StateStore() (store.StateStore, error)` を追加（lazy init）
- Close は内部 map クリア

#### SQLite: `internal/store/sqlite/state.go`
- スキーマに新規テーブル追加（`schema.sql` は IF NOT EXISTS のため idempotent）:
  ```sql
  CREATE TABLE IF NOT EXISTS kintone_oauth_state (
      state        TEXT PRIMARY KEY,
      principal_id TEXT NOT NULL,
      verifier     TEXT NOT NULL,
      method       TEXT NOT NULL DEFAULT 'S256',
      created_at   INTEGER NOT NULL,
      expires_at   INTEGER NOT NULL
  );
  CREATE INDEX IF NOT EXISTS idx_kintone_oauth_state_expires ON kintone_oauth_state(expires_at);
  ```
- **one-shot Take は `DELETE ... RETURNING` で atomic**（SQLite 3.35+、modernc.org/sqlite 1.50.0 ✅）
- 期限切れ Take は `RETURNING` でも返るので Go 側で `expires_at < now` 判定して `ErrStateNotFound`
- Container.StateStore は db 共有 + sync.Once
- StateStore.Close は no-op（DB は Container 所有）

#### Redis: `internal/store/redis/state.go`
- キー: `kintone:oauthstate:<state>` に hash で `principal_id` / `verifier` / `method` / `created_at` を格納
- TTL は `EXPIRE` で自動失効（10 分）
- **one-shot Take は `HGETALL` → `DEL` ではなく、Lua スクリプトでアトミック化**
  - `GETDEL` は string 用で hash には使えないため Lua 必須
  - スクリプト: `local v = redis.call('HGETALL', KEYS[1]); if #v > 0 then redis.call('DEL', KEYS[1]); end; return v`
- Container.StateStore は client 共有 + sync.Once

#### DynamoDB: `internal/store/dynamodb/state.go`
- PK: `kintone:oauthstate:<state>` で単一テーブルに相乗り
- 属性: `principal_id` / `verifier` / `method` / `created_at`(ns) / `expires_at`(ns) / `ttl`(sec)
- **one-shot Take は `DeleteItem` with `ReturnValues=ALL_OLD`** — 不在は Attributes 空 → `ErrStateNotFound`
- 期限切れチェックは Go 側（DynamoDB Auto-TTL は eventual のため）
- `keys.go` に `PKPrefixKintoneOAuthState = "kintone:oauthstate:"` を追加
- Container.StateStore は client 共有 + sync.Once

### C. Container interface 拡張

```go
type Container interface {
    Tokens() (TokenStore, error)
    CacheForDecorator() (CacheStore, error)
    CacheForAdmin() (CacheStore, error)
    SigningKey() (SigningKeyStore, error)
    StateStore() (StateStore, error)   // 新規
    IDProxyStore() (idproxy.Store, error)
    LocationString() string
    Close(ctx context.Context) error
}
```

全 4 backend Container に `StateStore()` を実装。

### D. Handler 配線変更

`internal/cli/mcp/oauth_glue.go:114` を変更:

```go
- states := oauthcallback.NewMemoryStateStore()
+ states, err := container.StateStore()
+ if err != nil { return nil, fmt.Errorf("mcp serve: get StateStore: %w", err) }
```

`oauthSetup.StateStore` フィールドの型を `store.StateStore` に変更。`closeStates()` は Container が一括 Close するので削除可（ただし冪等のため呼んでも害なし → そのまま残す）。

### E. loopback 物理削除範囲

削除ファイル:
- `internal/auth/oauth/flow.go`（Login）
- `internal/auth/oauth/flow_test.go`
- `internal/auth/oauth/callback.go`（NewCallbackServer / CallbackServer）
- `internal/auth/oauth/callback_test.go`
- `internal/auth/oauth/browser.go`（DefaultOpenBrowser）
- `internal/auth/oauth/browser_test.go`

`errors.go` から削除:
- `ErrStateMismatch`（callback.go のみで使用）
- `ErrAuthorizationCodeMissing`（callback.go のみで使用）
- `ErrCallbackTimeout`（flow / callback のみで使用）
- `ErrInvalidRedirectURL`（flow.go のみで使用）
- `ErrMissingClientCredentials`（flow.go のみで使用）

保持:
- `pkce.go` / `state.go`（state generator、handler.go から呼ばれる）/ `token.go` / `provider.go` / `refresh.go` / `OAuthError`
- `ErrRefreshTokenRevoked` / `ErrTokenExpired`（refresh.go で利用）

doc コメントから `loopback callback サーバ` / `クロスプラットフォームブラウザ起動` 言及を削除。

### F. Conformance テスト

`internal/store/storetest/state_conformance.go`:

```go
func RunStateStoreConformance(t *testing.T, makeStore func() (store.StateStore, func()))
```

サブテスト:
1. **Put_Take_Roundtrip** — Put → Take で entry が返り、再 Take は `ErrStateNotFound`
2. **TakeMissing** — 不在 state は `ErrStateNotFound`
3. **TTLExpiration** — TTL を 10ms に設定（実装側に Now hook がない場合は無視可）か、または backend デフォルト TTL に依存しない設計とする。`storetest` 共通シナリオは「短 TTL でも Put 直後の Take は成功する」ことを担保し、TTL 失効は各 backend 単体テストに委ねる
4. **ConcurrentTake_OneWinner** — N=10 goroutine で同時 Take、勝者は **ちょうど 1**、残りは `ErrStateNotFound`。これがアトミック性の核となる
5. **CloseIdempotent** — Close を 2 回呼んで panic / err しない

各 backend の `store_test.go` で `storetest.RunStateStoreConformance` を呼ぶ。

### G. E2E 拡張

`internal/cli/mcp/serve_e2e_test.go`（既存 build tag `e2e`）に sqlite backend での state put → state take シナリオを追加。Redis miniredis は既存 helper があれば併設、なければ M15 に先送り。

### H. ドキュメント更新

- `README.md` / `README.ja.md` の Storage Backend 表に **OAuth State** 列を追加（〇/〇/〇/〇）
- `docs/specs/kintone_spec.md` の MCP 認証フロー節を「multi-replica 配置可」明記、StateStore を Storage backend に統合済みと記述
- `CLAUDE.md` の M11 設計判断後に M14 を追記
- `CHANGELOG.md` に v0.4.0 エントリ（BREAKING: なし、CHANGE: state map が Storage に移行、REMOVED: oauth.Login / CallbackServer / DefaultOpenBrowser）

## TDD ステップ

1. **Red** — `internal/store/state.go` を追加し、`storetest/state_conformance.go` で `RunStateStoreConformance` を定義
2. **Red** — memory backend に `state.go` 追加（空実装で Conformance 走らせて FAIL）
3. **Green** — memory backend 実装、Conformance PASS
4. **Red** — sqlite に `state.go` + `state_test.go`（Conformance + 並行 Take）
5. **Green** — sqlite 実装（DELETE RETURNING）、PASS
6. **Red→Green** — redis（Lua スクリプト + miniredis テスト）
7. **Red→Green** — dynamodb（DeleteItem ALL_OLD、既存 fake 流用）
8. **Refactor** — `internal/mcp/oauthcallback/state.go` を alias 形式に縮小、handler_test.go の参照確認
9. **Refactor** — `oauth_glue.go` を Container.StateStore() 経由に
10. **Delete** — loopback 関連ファイル & errors 削除、ビルド検証
11. **E2E** — `serve_e2e_test.go` 拡張
12. **Docs** — README/specs/CHANGELOG

## リスク評価

| リスク | 対策 |
|--------|------|
| DynamoDB TTL eventual deletion → 期限切れ state を Take で返す | Go 側で `expires_at < now()` 判定し ErrStateNotFound（cache.go と同パターン） |
| Redis Lua の cluster atomicity | 単一キー操作のため CROSSSLOT は発生しない（cluster でも OK） |
| SQLite 並行 Take の race | `DELETE ... RETURNING` は単一文 = atomic。WAL モードと組み合わせて並行性確保 |
| Memory backend multi-replica 利用 | `ErrMemoryOIDCForbidden` が auth=oidc を Container open 時に拒否済み（追加対策不要） |
| state cookie と StateStore の二重防御 | 役割分担: cookie = 同一ブラウザ確認、StateStore = サーバ側 one-shot 強制。両者は補完関係で残す |
| loopback 削除で残存 import | `grep -rn "oauth.Login\|oauth.NewCallbackServer\|oauth.DefaultOpenBrowser" --include="*.go"` で 0 件であることを確認済み（テストファイル除く）|
| oauthcallback テストでの MemoryStateStore 参照 | alias 経由で型互換、`NewMemoryStateStore` も再エクスポート関数として残す |

## 後方互換性

- `oauthcallback.NewHandler` のシグネチャ無変更（`HandlerConfig.States StateStore` は alias 経由で `store.StateStore` を受け入れ）
- `oauthcallback.StateEntry` / `oauthcallback.ErrStateNotFound` も alias / 再エクスポートで参照可能
- `oauthcallback.NewMemoryStateStore` は memory backend container を内部で生成して返す関数として残す（test 後方互換）

## 完了条件

- [ ] `go test -race -cover ./...` 全 PASS
- [ ] `go test -race -tags e2e ./...` PASS
- [ ] `golangci-lint run` 違反 0
- [ ] `gofmt -l .` 出力 0 行
- [ ] `go vet ./...` PASS
- [ ] `grep -rn "oauth.Login\|oauth.NewCallbackServer\|oauth.DefaultOpenBrowser" internal/ cmd/ --include="*.go"` 結果 0
- [ ] README / CHANGELOG / spec 更新
- [ ] Conventional Commits 日本語コミット
