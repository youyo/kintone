---
title: M12 - 統合 Storage バックエンド (Unified Storage Backend)
project: kintone
created: 2026-04-30
status: Draft / Ready for Review
complexity: H
---

# M12: 統合 Storage バックエンド

## 0. スコープ判断

本マイルストーンには storage 統合の必須事項に加えて以下の CLI 契約変更が含まれる：
- `cache stats` JSON schema 刷新（`db_path`/`db_exists` → `backend`/`location`/`reachable`）
- `cache clear --scope` は維持+ `--key <prefix>` を高度デバッグ用に追加
- `config show` の `cache_path` フィールド削除 / `storage` フィールド追加

**これらを M12 に含める根拠**:

1. **`cache stats` 刷新は不可避**: backend が SQLite 以外（Redis/DynamoDB）になると `db_path` / `db_size_bytes` の意味が消える。スキーマを変えずに M12 を出すと「正しい backend 情報が得られない API」が永続化する
2. **`cache clear --scope` は維持**: `--scope=apps` の論理抽象は backend 中立で表現可能（SQLite=`LIKE 'v1:app:%'`、Redis=`SCAN MATCH 'kintone:cache:v1:app:*'`、DynamoDB=`Query GSI2 begins_with 'v1:app:'`）。新たに `--key <raw-prefix>` を高度デバッグ用に追加するのみ（互換維持）
3. **`config show` の `cache_path` 削除は内部削除の連鎖**: `Resolved.CachePath` を消す以上、出力フィールドも消す必要がある
4. **分離した場合のコスト**: M12 と M12.1 で 2 回 breaking change を出すと、ユーザーが migration を 2 回行う必要がある（v0.1.0 → v0.2.0 → v0.3.0）。リリース直前ユーザー0 の今こそ一括が最も低コスト

**よって本マイルストーンに含める**。CHANGELOG では「Storage 統合に伴う必然的な CLI 契約変更」として明記する。

---

## 1. Context — なぜこの変更が必要か

### 現状: 認証情報・キャッシュが 3 系統に分散

kintone CLI/MCP は永続化層を **3 つの分離した系統**で持つ：

| # | 系統 | 場所 | バックエンド | スキーマ | 永続化 |
|---|------|------|-------------|---------|--------|
| 1 | TokenStore (`internal/tokenstore/`) | `~/.cache/kintone/tokens.db` | SQLite (modernc.org/sqlite v1.50.0) | `oauth_tokens(domain, principal_id, auth_type, ...)` 複合 PK | あり |
| 2 | Cache (`internal/cache/`) | `~/.cache/kintone/cache.db` | SQLite | `cache(key, value, expires_at, created_at)` | あり |
| 3 | idproxy Store (`internal/idproxy/config.go` L143) | プロセスメモリ | `idstore.NewMemoryStore()` | sessions / auth_codes / access_tokens / refresh_tokens (CAS) / clients / family_revocations | **なし** |

加えて `idproxy.Auth` の `SigningKey`（OIDC JWT 署名鍵 / ES256）は `BuildAuth()` 起動時毎に `ecdsa.GenerateKey()` で **ephemeral 生成**される（`internal/idproxy/config.go` L129）。

### この設計が引き起こす問題

1. **設定の散在** — `KINTONE_TOKENS_PATH`, `KINTONE_CACHE_PATH`, `KINTONE_CACHE_DISABLE`, `KINTONE_MCP_COOKIE_SECRET` 等が独立して存在し、Lambda / Fargate / k8s の Secret/ConfigMap が肥大化する。
2. **分散環境で動かない** — multi-instance MCP（Lambda 同時実行・Fargate scale-out・複数 Pod）で同一ユーザの session/auth_code/refresh_token を共有する手段がない（MemoryStore 固定）。
3. **SigningKey ローテーションの事実上の強制** — プロセス再起動で全 OIDC JWT が失効、ユーザーは毎回再ログイン。Lambda cold start でも同様。
4. **ライブラリの生臭さ** — ユーザーが「idproxy」というサードパーティ依存の存在を意識し、その独自設定を別途行う必要がある。kintone CLI の自前パッケージとして抽象化すべき。

### 意図する成果

- 1 つの `KINTONE_STORE_*` 設定で 4 種のバックエンドを切替（memory / sqlite / redis / dynamodb）
- kintone TokenStore + Cache + SigningKey + idproxy 状態を**同一バックエンド種別**に格納（多重化された接続情報を排除）
- 再起動耐性を持つ OIDC SigningKey（永続化または env 注入、本番では ephemeral 禁止）
- ユーザーから idproxy の存在は完全に隠蔽

---

## 2. 確定方針（ユーザー回答に基づく）

| # | 項目 | 確定内容 |
|---|------|---------|
| 1 | 統合スコープ | **同一バックエンド・別論理**。1 つの Storage 設定で sqlite なら同一ディレクトリ、redis なら同一接続、dynamodb なら同一テーブルを共有。内部はテーブル名 / キー prefix で分離 |
| 2 | バックエンド | **Memory + SQLite + Redis + DynamoDB の 4 つすべて** |
| 3 | 既存 ENV | **即削除** OK（v0.1.0 リリース前でユーザー0確定、migration 不要） |

---

## 3. 採用アーキテクチャ

### 基本戦略

**idproxy v0.4.2 の公式 SQLite/Redis/DynamoDB Store 実装をそのまま使う**。kintone は 17 メソッドの Store interface を再実装しない（保守コスト最小化）。kintone TokenStore / Cache / SigningKey は kintone 自前で実装し、論理分離（テーブル名 / キー prefix）で共存させる。

### Backend 別の物理共有方針

| Backend  | 物理共有 | 論理分離 | 備考 |
|----------|---------|---------|------|
| memory   | 別 Memory インスタンス（プロセス内） | 不要 | dev/test 用途のみ |
| **sqlite** | **同一ディレクトリ・2 ファイル**（`kintone.db` と `idproxy.db`）| ファイル分離 | **idproxy v0.4.2 が `New(path)` のみで `NewWithDB` がないため、1 ファイル 2 Pool は WAL チェックポイント競合・modernc 固有の Pool 並行挙動の未踏領域となるため棄却**。同ディレクトリ・2 ファイルで「設定 1 つ」の体感を維持 |
| redis    | 単一 `redis.UniversalClient` 共有 | `kintone:` / `idproxy:` の 2 prefix | `idpredis.NewWithClient(client, "idproxy:")` で client 共有 |
| dynamodb | 単一 `dynamodb.Client` + 単一テーブル | PK prefix 分離 + **GSI1**（kintone のみ sparse index）| GSI1: `gsi1pk=<domain>`, `gsi1sk=<principal_id>:<auth_type>` を kintone token のみに付与し ListByDomain は GSI1 Query。Scan 撤廃（C3 反映）。**全 backend を 1 binary に同梱**（build tag 分離なし、ユーザー要件「全部」を厳密に遵守）|

### パッケージ構成

```
internal/
├── store/                          # 新規 — 全データストア統合
│   ├── doc.go                       # パッケージ概要 + 4 backend 選定基準
│   ├── container.go                 # Container interface + Close 所有権
│   ├── env.go                       # KINTONE_STORE_* env → Config
│   ├── factory.go                   # OpenFromEnv / OpenFromConfig
│   ├── tokens.go                    # TokenStore interface + Token struct
│   ├── cache.go                     # CacheStore interface + Stats struct
│   ├── signingkey.go                # SigningKeyStore interface
│   ├── memory/
│   │   ├── tokens.go
│   │   ├── cache.go                 # TTL は lazy delete + cleanup goroutine
│   │   ├── signingkey.go
│   │   └── idproxy_adapter.go       # idstore.NewMemoryStore() ラップ
│   ├── sqlite/
│   │   ├── open.go                  # *sql.DB を WAL+busy_timeout で開く
│   │   ├── schema.sql               # kintone_oauth_tokens / kintone_kv_cache / kintone_signing_keys
│   │   ├── tokens.go
│   │   ├── cache.go
│   │   ├── signingkey.go
│   │   └── idproxy_adapter.go       # idproxysqlite.New(idproxyPath) を別ファイルで開く
│   ├── redis/
│   │   ├── open.go                  # redis.UniversalClient 構築
│   │   ├── tokens.go                # HSET kintone:tokens:{d}:{p}:{a}
│   │   ├── cache.go                 # SET kintone:cache:{key} EX ttl
│   │   ├── signingkey.go
│   │   └── idproxy_adapter.go       # idpredis.NewWithClient(client, "idproxy:")
│   ├── dynamodb/
│   │   ├── open.go                  # dynamodb.Client 構築 + DescribeTable 疎通確認
│   │   ├── keys.go                  # PK prefix 定数 + 衝突防止アサート
│   │   ├── tokens.go                # PK=kintone:tokens:... + GSI1 attr 付与
│   │   ├── cache.go                 # PK=kintone:cache:... + ttl 属性
│   │   ├── signingkey.go            # PK=kintone:signingkey:current
│   │   └── idproxy_adapter.go       # idpdynamodb.NewDynamoDBStoreWithClient(client, table)
│   └── storetest/
│       └── conformance.go           # 全 backend 共通の conformance test
├── idproxy/
│   ├── config.go                    # BuildAuth(ctx, env, container) シグネチャ変更
│   ├── signingkey.go                # 新規: env > Storage > ephemeral 解決
│   └── ...
├── tokenstore/                      # Phase 8 で削除
├── cache/                           # Phase 8 で削除
└── ...
```

### Container interface

```go
// internal/store/container.go
package store

type Container interface {
    // 各アクセサは lazy init: 初回呼び出し時に sub-store と接続/ファイルを open する。
    // - Tokens() のみ呼ばれた CLI auth login 経路では Cache/SigningKey/IDProxyStore は Open されない（kintone.db のみ作成）
    // - SigningKey() / IDProxyStore() を呼ぶ MCP serve 経路で初めて idproxy.db を Open する
    Tokens() (TokenStore, error)
    // CacheForDecorator は API decorator 経路用 (fail-open: 接続失敗時 nil, nil)
    CacheForDecorator() (CacheStore, error)
    // CacheForAdmin は cache stats/clear/store init 経路用 (fail-fast: 接続失敗時 nil, err)
    CacheForAdmin() (CacheStore, error)
    SigningKey() (SigningKeyStore, error)
    IDProxyStore() (idpstore.Store, error)   // upstream `github.com/youyo/idproxy/store.Store` を import alias で参照、kintone wrapper は持たない
    LocationString() string                  // "sqlite:///path/to/kintone.db" 等の表示用
    Close(ctx context.Context) error         // context 対応（タイムアウト制御）
}
```

**Lazy init の責務**:
- **mandatory アクセサ** (`Tokens()` / `SigningKey()` / `IDProxyStore()` / `CacheForAdmin()`): `sync.Once` で 1 度だけ open。エラー時もキャッシュし再試行しない（init エラーは fatal、mcp serve では起動失敗で exit）
- **optional アクセサ** (`CacheForDecorator()`): エラーを永続キャッシュしない。次回呼び出し時に再 init を試みる（一時的な Redis/DynamoDB 障害でも長寿命プロセスは自動回復）。ただし暴走防止のため最後の失敗から最低 1 秒の cooldown を設ける（連続失敗時の thundering herd 防止）

**自動回復の効く経路 = decorator 境界の専用サブフェーズ**:

現状 `internal/cli/api/helpers.go` / `internal/cli/mcp/helpers.go` / `internal/cli/mcp/serve.go` は起動時に API を 1 回だけ構築する。これでは起動時 cache 失敗が長寿命プロセスで永続化する。これは「単なる配線変更」ではなく **decorator 境界の再設計**なので、Phase 6 内に**専用サブステップ B-CACHE-1〜3** として独立化:

- **B-CACHE-1**: `internal/service/api/caching.go::CachingAPI` 構造体に `cacheProvider func() (store.CacheStore, error)` フィールド追加。各 method（`GetApp` / `GetAppFormFields` / `ListApps`）の冒頭で `cacheProvider()` を呼んで現在の cache を取得する形に変更（per-request lazy resolution）
- **B-CACHE-2**: `service/api.NewCachingAPI(upstream, container, domain)` のシグネチャを `(upstream, cacheProvider, domain)` に変更。`internal/cli/{api,ops,mcp}/helpers.go` から `cacheProvider = container.CacheForDecorator` を渡す
- **B-CACHE-3**: `internal/service/api/caching_test.go` を新シグネチャに更新。test では `cacheProvider = func() { return memCache, nil }` で固定 cache を返す

これにより MCP HTTP の各リクエスト時点で cache 接続が再評価され、起動時失敗から自動回復する。性能オーバーヘッドはアクセサが lazy init キャッシュ済みなら関数呼び出し 1 回のみ（実害なし）。

CLI 単発実行の `kintone api/ops` では従来通り 1 回構築で十分（プロセス寿命が短いため再試行不要）が、設計の単一性のため同じ `cacheProvider` 経路を通す。

**Phase 6 工数追加**: B-CACHE-1〜3 で +0.5 日 → Wave B 全体は 2.5 日 → 3.0 日。

**用途別 Open 経路**:
- `kintone version` / `completion` / `config show` 等の read-only コマンド: Container を **そもそも `OpenFromEnv` しない**（root command で lazy 判定）
- `kintone auth login/status/logout`: `Tokens()` のみ呼ぶ → kintone.db のみ open
- `kintone cache stats/clear`: `Cache()` のみ呼ぶ → kintone.db のみ open
- `kintone api ... / ops ...`（**`auth=api-token`**）:
  - `KINTONE_STORE_CACHE_BYPASS=1`: **Container 自体を Open しない**（store 完全非依存。既存 `KINTONE_CACHE_DISABLE=1` 経路と等価）
  - cache 有効: `Cache()` のみ呼ぶ（TokenStore は不要、API Token は env から直接取得）
- `kintone api ... / ops ...`（**`auth=oauth`**）:
  - `Tokens()` + `Cache()` を呼ぶ（`KINTONE_STORE_CACHE_BYPASS=1` 時は `Tokens()` のみ）
- `kintone mcp serve`（`auth=none`）: `auth=oauth` 経路と同じ
- `kintone mcp serve --auth oidc`: 上記 + `SigningKey()` + `IDProxyStore()` を呼ぶ → idproxy.db も open

### Cache の fail-open ポリシー

既存の `internal/cli/{api,ops,mcp}/helpers.go` は **cache 接続失敗時に upstream を直接返す** fail-open 設計（`cache.OpenIfExists` が nil を返したら CachingAPI を素通し）。一方 `cache stats / cache clear` は store 操作そのものなので接続失敗時は fail-fast。M12 ではこの**経路別の差異を明示的に維持**する。

**store の依存度を経路別に分類**:

| store 種別 | 経路 | 依存度 | 接続失敗時の挙動 |
|-----------|------|-------|----------------|
| TokenStore (`Tokens()`) | 全経路 | mandatory | fail-fast（エラー envelope を返す） |
| SigningKey (`SigningKey()`) | `auth=oidc` | mandatory | fail-fast（`SIGNING_KEY_REQUIRED`） |
| IDProxyStore (`IDProxyStore()`) | `auth=oidc` | mandatory | fail-fast |
| Cache (`Cache()`) | **API decorator 経路（`api/ops/mcp` の read-path）** | **optional** | **fail-open**: warn ログ + `nil, nil` 返却、caller は upstream を直接使う |
| Cache (`Cache()`) | **管理コマンド経路（`cache stats` / `cache clear` / `store init`）** | **mandatory** | **fail-fast**: `STORE_CONNECTION_FAILED` エラー envelope |

**実装**: `Container.Cache()` ではなく `Container.CacheForDecorator()` と `Container.CacheForAdmin()` の **2 つのアクセサ**に分割する。前者は接続失敗時 `nil, nil`、後者は接続失敗時 error を返す。caller は用途に応じて使い分ける（API decorator は前者、`cli/cache/{stats,clear}.go` と `cli/store/init.go` は後者）。

**例外**: `auth=api-token + cache_bypass=1` ファストパスは Container 自体を Open しないため、本ポリシーは関与しない。

これにより Redis/DynamoDB の cache レイヤ障害が `api-token` 経路の全機能停止には波及せず、しかし管理コマンドでは「接続失敗を成功風 no-op として隠蔽する」事故も防ぐ。

### Container ライフサイクル管理

**問題**: `cli/api/helpers.go` / `cli/ops/helpers.go` は現状 `service/api.API` を返すだけで、内部で開いた DB ハンドルを caller が close する経路がなく、Container 化すると DB/client リークの危険がある。

**解決**: **root command レベルで Container を一元管理**し、`PersistentPreRunE` で open、各 RunE 内 `defer` で close する（PersistentPostRunE は RunE 失敗時に走らないため使わない）。

```go
// internal/cli/root.go (新規責務)
//
// 設計修正: PersistentPreRunE/PostRunE は使わない（Cobra は RunE 失敗時に PostRun が走らないため
// cleanup が漏れる）。代わりに root command の Execute をラップし、defer で必ず Close を呼ぶ。
// globalContainer の package 変数は廃止し、rootCommand struct field に移動（test 汚染防止）。

type rootCommand struct {
    cmd       *cobra.Command
    container store.Container  // ライフサイクルは Execute スコープ
}

func NewRootCmd() *cobra.Command {
    rc := &rootCommand{}
    rc.cmd = &cobra.Command{
        Use: "kintone",
        PersistentPreRunE: func(c *cobra.Command, args []string) error {
            resolved, env, err := config.Resolve(...)
            if err != nil { return err }
            if !needsStore(c, resolved, env) {
                return nil
            }
            container, err := store.OpenFromEnv()
            if err != nil { return err }
            rc.container = container
            ctx := store.WithContainer(c.Context(), container)
            c.SetContext(ctx)
            return nil
        },
    }
    return rc.cmd
}

// Execute は cobra.Command.Execute をラップし、defer で必ず Container を Close する。
// RunE が error を返しても、panic でも、cleanup が走る。
func Execute(ctx context.Context) (err error) {
    rc := newRootCommandStruct()
    cmd := rc.buildCmd()
    defer func() {
        if rc.container != nil {
            closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
            defer cancel()
            if cerr := rc.container.Close(closeCtx); cerr != nil && err == nil {
                err = cerr
            }
        }
    }()
    return cmd.ExecuteContext(ctx)
}

// needsStore は cmd / resolved config / env から Container 必要性を判定する単一関数。

func needsStore(c *cobra.Command, resolved *config.Resolved, env *store.Env) bool {

    if isStoreOpenSkippedCommand(c) {
        return false
    }
    if isStoreRequiredCommand(c) {
        return true
    }
    if isAPIOrOpsCommand(c) {
        if resolved.Auth == "api-token" && env.CacheBypass {
            return false  // ファストパス: 完全 stateless
        }
        return true
    }
    return false
}

func isStoreOpenSkippedCommand(c *cobra.Command) bool { /* path 判定 */ }
```

**Cleanup 責務の単一化**:

Close 所有者は **各サブコマンドの `RunE` 冒頭の `defer store.CloseIfOwner(ctx)` ただ一つ**。`Execute` ラッパー側の defer は廃止し、二系統混在を解消する。

```go
// 全サブコマンドの RunE 共通パターン
RunE: func(c *cobra.Command, args []string) error {
    defer store.CloseIfOwner(c.Context())
    // ... コマンド本体
},
```

`store.CloseIfOwner(ctx)` は context に注入された Container を取り出し `sync.Once` で 1 度だけ Close する。

**Exit criteria**（カバーすべきケース）:

| ケース | Open / Close 挙動 |
|-------|------------------|
| `PersistentPreRunE` 成功 → `RunE` 成功 | Open → defer で Close |
| `PersistentPreRunE` 成功 → `RunE` error 返却 | Open → defer で Close（error は Cobra が伝搬） |
| `PersistentPreRunE` 成功 → `RunE` 内 panic | Open → defer で Close → panic rethrow |
| `PersistentPreRunE` 失敗 | Open しない（`needsStore=true` でも Open エラーで panic 前に return）→ Close 不要 |
| `cmd.Execute()` 直接呼び出し（test） | 上記と同一（`PersistentPreRunE` + `RunE` defer の責務は変わらない） |
| `cmd.ExecuteContext(ctx)` SIGTERM 受信 | ctx cancel → RunE が graceful return → defer Close |
| 将来の追加 `PreRunE` でエラー | `PersistentPreRunE` 後・`RunE` 前に失敗するため defer 未起動。`PersistentPreRunE` 内で Close 責務を伴う場合は **同 hook 内で defer する**（root の `PersistentPreRunE` 自体に `defer func() { if err != nil { container.Close() } }()` を入れる） |

**実装契約**:
- 既存 `internal/cli/root.go::ExecuteWith(ctx)` **外側ラッパは維持し、ここに Close 責務も集中**させる:
  - error → JSON envelope を stdout に書く（既存責務）
  - `defer ExecuteWith` で Container.Close を呼ぶ（追加責務、`ctx` から取り出して `sync.Once` で 1 回だけ Close）
- root の `PersistentPreRunE`: Container Open + ctx 注入のみ（Close は `ExecuteWith` の defer に委譲）
- 各サブコマンドの `RunE` 冒頭の `defer store.CloseIfOwner(ctx)` は**廃止**（ExecuteWith 集中管理で個別 defer 仕込み漏れリスクを排除）
- `cmd.PersistentPostRunE` は使わない（RunE 失敗時に走らないため）
- main は `cli.ExecuteWith(ctx, NewRootCmd())` を呼ぶ（既存契約のまま）
- **既存テスト経路の扱い**:
  - `cmd.Execute()` 直接呼出（root_test 等）: `ExecuteWith` を経由しないため Close されない。これは test scope の責任（test 関数内で `defer container.Close(ctx)` を呼ぶか、t.Cleanup で hook 経由のテスト用ヘルパが Close する）
  - `ExecuteLoginWith` / `ExecuteStatusWith` 等のサブコマンド ExecuteXxxWith helper: 内部で `cli.ExecuteWith` を呼ぶ実装に変更（既存挙動を保ちつつ Close 責務を統一）
- `mcp serve` のような長時間プロセス: signal で ctx cancel → RunE が graceful return → ExecuteWith の defer が Close

**Close 所有権の単一エントリ**: 本番経路では `ExecuteWith` のみが Close する。「複数の実行経路があるが Close 仕込み漏れリスクなし」を保証。

**Test seam**: rootCommand struct を test ファイルから直接参照可能に（`internal/cli/root_test.go` で field 注入してテスト）。package var を持たないので `t.Parallel()` でも汚染しない。

`api/ops/auth/cache/mcp` 各サブコマンドは `c.Context()` から `store.ContainerFromContext(ctx)` で Container を取得し、必要な sub-store アクセサを呼ぶ。MCP serve のような長時間プロセスは `runHTTP` 完了 = signal 受信時に Container を close する flow を `cli/mcp/serve.go` 内で実装（`PostRun` までいかないため）。

**`needsStore` 決定マトリクス（exit criteria）**:

| コマンド | auth | cache_bypass | needsStore |
|---------|------|--------------|-----------|
| `version` / `completion` / `config *` / **`store init dynamodb`** | * | * | false |
| `auth login/status/logout` | * | * | true |
| `cache stats/clear` | * | * | true |
| `api ... / ops ...` | api-token | 1 | **false**（ファストパス） |
| `api ... / ops ...` | api-token | 0 | true |
| `api ... / ops ...` | oauth | * | true |
| `mcp serve` `--auth=oidc` | * | * | true（必ず Container が必要）|
| `mcp serve` `--auth=none --authz=api-token` | * | 0 | true（Cache が必要）|
| `mcp serve` `--auth=none --authz=api-token` | * | 1 | **false**（完全 stateless ファストパス）|
| `mcp serve` `--auth=none --authz=oauth` | * | * | true（TokenStore が必要）|

このマトリクス全行を `internal/cli/root_test.go` で網羅テスト。

### Cache API 形状の最終形

`Container.Cache()` ではなく **`Container.CacheForDecorator() (CacheStore, error)` と `Container.CacheForAdmin() (CacheStore, error)` の 2 アクセサに分割**する。これは Section "Cache の fail-open ポリシー" で決めた経路別動作を Phase 1 の interface 定義レベルで強制するため。

- `CacheForDecorator()`: 接続失敗時 `nil, nil` 返却（fail-open、API decorator 経路用）
- `CacheForAdmin()`: 接続失敗時 `nil, error` 返却（fail-fast、`cache stats / cache clear` の 2 経路でのみ利用、`store init` は Container を経由しないため対象外）

Phase 1 の `internal/store/container.go` interface 定義はこの 2 メソッドを公開し、`Cache()` という汎用名は提供しない（誤用防止）。`Tokens()` / `SigningKey()` / `IDProxyStore()` は分離不要（fail-fast 一択）。

### サブコマンド単体テスト seam

既存テストは `ExecuteLoginWith` / `ExecuteStatusWith` / `ExecuteLogoutWith` / `climcp.NewCmd().Execute()` のように **root を経由せず**サブコマンドだけ実行する設計。これを Container 注入の root 前提に強制移行すると既存テストが大量に壊れる。

**方針**: **両系統を維持**する。
1. **本番経路**: root の `PersistentPreRunE` で `globalContainer` を作り、`store.WithContainer(ctx)` で context に注入。サブコマンドは `store.ContainerFromContext(ctx)` で取得
2. **テスト経路**: 既存の `Set*Fn` hook を維持（`SetOpenContainerFn(func() (store.Container, error))` を `cli/auth/helpers.go` 等に追加）。test hook が non-nil なら root を経由せずに直接 Container を返す
3. サブコマンドは「context にあれば優先、なければ hook を呼ぶ」順序で Container を取得
4. `t.Setenv("KINTONE_STORE_BACKEND", "memory")` も同等に効くが、hook の方がテストロジックを root に依存させない

これにより既存 22+α パッケージのテストファイルを改修する必要は最小限（hook 名前更新のみ）に留まる。

**unit test**: `t.Setenv("KINTONE_STORE_BACKEND", "memory")` + Cobra command の `ExecuteContext(ctx)` で root のライフサイクルを通すテストが書ける。サブコマンド単体テストは hook 注入で完結。leak detection は `goleak.VerifyNone(t)` を全 CLI test の `TestMain` に組み込み。

### Close 所有権モデル

upstream `idproxy` の各 store 実装は内部に cleanup goroutine（5 分周期）を持ち、`Close()` で停止する設計。「sub-store Close を一律 no-op」とすると goroutine leak、毎回呼ぶと共有 client を二重 close する矛盾が起きる。**backend ごとに所有権境界を明示的に分ける**。

| Backend | upstream idproxy.Store.Close() | kintone 自前 store.Close() | shared resource | Container.Close(ctx) で行う処理 |
|---------|-------------------------------|---------------------------|-----------------|-------------------------------|
| memory  | **必ず呼ぶ**（cleanup goroutine 停止） | 必ず呼ぶ（同上）| なし | 1) idproxy.Close() 2) kintone tokens/cache/signingkey の Close() |
| sqlite  | **必ず呼ぶ**（cleanup goroutine + 内部 *sql.DB[idproxy 用] を upstream が close）| no-op（kintone 用 *sql.DB は Container が直接管理）| 別ファイル（共有なし）| 1) idproxy.Close() 2) kintone *sql.DB.Close() |
| redis   | wrapper の Close を呼ぶ（cleanup goroutine 停止のみ）| no-op | `redis.UniversalClient` を kintone と idproxy が**共有** | 1) idproxy wrapper.Close() (client は閉じない) 2) kintone wrapper.Close() (同) 3) Container が `client.Close()` を 1 度だけ呼ぶ |
| dynamodb | wrapper の Close を呼ぶ（aws-sdk-go-v2 client は明示 Close 不要）| no-op | `dynamodb.Client` 共有（Close 不要）| 1) idproxy wrapper.Close() 2) kintone wrapper.Close() 3) Container は client を Close しない（GC まかせ） |

**redis の特殊対応**: upstream `idpredis.NewWithClient(client, prefix)` が返す store の `Close()` は内部で `client.Close()` を呼ぶ仕様の場合、kintone は **client を閉じない wrapper Close を持つアダプタ**を `internal/store/redis/idproxy_adapter.go` で 1 段噛ませる（wrapper が `client.Close()` を抑制し、cleanup goroutine だけ止める）。Phase 0 で upstream の Close 挙動を確認し、必要なら adapter 実装を確定。

**実装ルール**:
- `Container.Close(ctx)` は逆依存順（idproxy → kintone tokens/cache/signingkey → shared client）で直列に閉じる
- `sync.Once` で client Close を冪等化（二重呼び出しでも panic しない）
- 各 sub-store の Close は upstream cleanup goroutine の停止責務のみを担い、shared client は触らない
- Lazy 未 init なリソースは Close でも触らない（`if !initialized { return nil }`）
- `goleak.VerifyNone(t)` で goroutine leak ゼロをテストで保証

---

## 4. 論理スキーマと分離戦略

### SQLite（同一ディレクトリ・2 ファイル分離）

**kintone.db**（kintone 用 `*sql.DB` が `CREATE TABLE IF NOT EXISTS` で構築）

```sql
CREATE TABLE IF NOT EXISTS kintone_oauth_tokens (
    domain        TEXT NOT NULL,
    principal_id  TEXT NOT NULL,
    auth_type     TEXT NOT NULL,
    api_token     TEXT NOT NULL DEFAULT '',
    access_token  TEXT NOT NULL DEFAULT '',
    refresh_token TEXT NOT NULL DEFAULT '',
    expires_at    INTEGER NOT NULL DEFAULT 0,
    updated_at    INTEGER NOT NULL,
    PRIMARY KEY (domain, principal_id, auth_type)
);

CREATE TABLE IF NOT EXISTS kintone_kv_cache (
    key        TEXT PRIMARY KEY,
    value      BLOB NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kintone_kv_cache_expires ON kintone_kv_cache(expires_at);

CREATE TABLE IF NOT EXISTS kintone_signing_keys (
    id         TEXT PRIMARY KEY,
    pem        TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
```

**idproxy.db**（idproxy v0.4.2 内部で `New(path)` 経由で構築・スキーマは upstream 管理）

両ファイルは同じ親ディレクトリ（`KINTONE_STORE_SQLITE_DIR` または default `~/.local/state/kintone/`）に配置される。

PRAGMA: `journal_mode=WAL` + `busy_timeout=5000` + `synchronous=NORMAL`。modernc.org/sqlite の DSN 構文（例: `?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)`）に従う（Phase 0 で確定）。

### Redis（単一 client、key prefix で分離）

```
# kintone 自前
kintone:tokens:{domain}:{principal_id}:{auth_type}    HSET hash
kintone:cache:{cache_key}                              SET string + EX ttl
kintone:signingkey:current                             SET string (PEM, no expire)

# idproxy（idpredis.NewWithClient(client, "idproxy:")）
idproxy:session:{id}        JSON
idproxy:authcode:{code}     JSON
idproxy:accesstoken:{jti}   JSON
idproxy:refreshtoken:{id}   JSON + Lua CAS (consume.lua)
idproxy:client:{client_id}  JSON
idproxy:familyrevoked:{id}  String
```

トップレベル namespace は `kintone:` と `idproxy:` の 2 つのみ。

### DynamoDB（単一テーブル + GSI1 + GSI2）

**テーブル定義（事前作成・kintone は init コマンドでチェック）**:

> **属性名規約**: upstream `github.com/youyo/idproxy` v0.4.2 が DynamoDB store で使用する属性名と完全互換にするため、**全て lowercase を採用**: `pk` / `data` / `ttl` / `gsi1pk` / `gsi1sk` / `gsi2pk` / `gsi2sk`。kintone 側で `PK` 大文字を使うと upstream `GetItem`/`PutItem` の AttributeName と整合しない。Phase 0 で upstream 実装の属性名を最終確認し、不一致時は計画側を upstream に揃える方針で固定。

```
TableName: <KINTONE_STORE_DYNAMODB_TABLE>
KeySchema: pk (HASH, S)
AttributeDefinitions:
  - pk (S)
  - gsi1pk (S)         # kintone token のみ
  - gsi1sk (S)         # kintone token のみ
  - gsi2pk (S)         # kintone cache のみ（cache 一括操作用）
  - gsi2sk (S)         # kintone cache のみ（key prefix 検索用）
GlobalSecondaryIndexes:
  - IndexName: gsi1
    KeySchema: gsi1pk (HASH), gsi1sk (RANGE)
    Projection: ALL
  - IndexName: gsi2
    KeySchema: gsi2pk (HASH), gsi2sk (RANGE)
    Projection: KEYS_ONLY     # entry_count / clear --key prefix のみ参照
TimeToLiveSpecification:
  AttributeName: ttl
  Enabled: true
BillingMode: PAY_PER_REQUEST
```

**TTL 属性命名規則（backend 共通）**:
- 全 backend で **`expires_at` (epoch nanoseconds, INTEGER)** をアプリケーション層の真実とする（読み出し時チェック / SQLite index）
- DynamoDB 専用に **`ttl` (epoch seconds, NUMBER)** を併記し DynamoDB Auto-TTL を有効化
- `ttl = floor(expires_at / 1e9)` で kintone 側書き込み時に同期。アプリ層は常に `expires_at` を見る（DynamoDB の eventual delete 遅延を吸収）
- TTL なしのアイテム（`kintone_signing_keys`, `clients` 等）は `expires_at=0`、`ttl` 属性を書かない

**Item pk 命名（衝突回避マトリクス）**:

| 用途 | pk 値 | gsi1 | gsi2 | ttl 属性 | 備考 |
|------|-------|------|------|---------|------|
| kintone OAuth Token | `kintone:tokens:{domain}:{principal_id}:{auth_type}` | `gsi1pk=<domain>` `gsi1sk=<principal_id>:<auth_type>` | なし | なし | `data` 属性に JSON。`Get` 時 `expires_at < now()` チェック必須 |
| kintone Cache | `kintone:cache:{key}` | なし | `gsi2pk='kintone:cache'` `gsi2sk={key}` | `expires_at` (ns) + `ttl` (sec) | `data` 属性に bytes。`Get` 時 `expires_at` チェック必須 |
| kintone SigningKey | `kintone:signingkey:current` | なし | なし | なし | `pem` 属性 |
| idproxy session | `session:{id}` | なし | なし | `expires_at` + `ttl` | upstream 既存 |
| idproxy auth_code | `authcode:{code}` | なし | なし | `expires_at` + `ttl` | upstream 既存 |
| idproxy access_token | `accesstoken:{jti}` | なし | なし | `expires_at` + `ttl` | upstream 既存 |
| idproxy refresh_token | `refreshtoken:{id}` | なし | なし | `expires_at` + `ttl` (CAS) | upstream 既存。Phase 0 で expired チェック有無確認 |
| idproxy client | `client:{client_id}` | なし | なし | なし | upstream 既存 |
| idproxy family_revoked | `familyrevoked:{id}` | なし | なし | `expires_at` + `ttl` | upstream 既存 |

`internal/store/dynamodb/keys.go` に PK prefix 定数を const 化、kintone 側で誤って `session:` 等を書かないことを lint テストで保証。

**Query 戦略（Scan 不使用）**:
- `ListByDomain` (CLI auth status / logout --all): `Query on GSI1` (`gsi1pk = :domain`)
- `Cache.Stats() entry_count`: `Query on GSI2` (`gsi2pk = 'kintone:cache'`) → KEYS_ONLY で件数取得（読み出し時 `expires_at` チェックは別途 Get で実施）
- `Cache.Clear(--key <prefix>)`: `Query on GSI2` (`gsi2pk = 'kintone:cache' AND begins_with(gsi2sk, :prefix)`) → 取得 PK 群に対して `BatchWriteItem` で削除（25 件ずつバッチ）
- `expired_count` の正確な算出は DynamoDB 単発 Query では不可のため、**stats は entry_count + （ベストエフォート）expired_count** とし、後者は GSI2 全 Scan + filter での近似値（テーブル規模が大きい場合は省略可能で `null` を返す）

**Throttling 対策**: `BatchWriteItem` 連発で `ProvisionedThroughputExceededException` 時は exponential backoff + jitter（aws-sdk-go-v2 デフォルトの retry middleware を活用、max 3 retries）。

**Pagination 要件**: DynamoDB Query は 1 リクエスト 1MB 上限のため、以下の Query 経路では **`LastEvaluatedKey` を辿る完全ページング**を必須化:
- `Cache.Stats() entry_count`: GSI2 を全ページ Scan し件数を合計（途中エラーは fail-fast、部分集計値は返さない）
- `Cache.Clear(--scope|--key)`: GSI2 で対象 PK を全ページ列挙してから `BatchWriteItem` で 25 件ずつ削除。`UnprocessedItems` がある場合は再送（max 3 retries）
- `TokenStore.ListByDomain`: GSI1 を全ページ Query。返却前にメモリで sort して安定順序を保証

unit test で「2 ページ以上にまたがる結果セットでも entry_count / clear / list が正確に動く」ことを mock client で検証。

### Memory（別インスタンス）

別 map / 別 mutex。プロセス内なので prefix 不要。Cleanup は **lazy delete + 5 分間隔の goroutine**（停止は `Container.Close(ctx)` で `ctx.Done` を listen）。

---

## 5. 環境変数の新体系（完全表）

### 新設

| 環境変数 | デフォルト | 用途 |
|---------|-----------|------|
| `KINTONE_STORE_BACKEND` | `sqlite` | バックエンド選択（`memory` / `sqlite` / `redis` / `dynamodb`）|
| `KINTONE_STORE_SQLITE_DIR` | `~/.local/state/kintone/` | SQLite ファイル群を置くディレクトリ。中に `kintone.db` と `idproxy.db` を配置 |
| `KINTONE_STORE_REDIS_URL` | （未指定時 backend=redis ならエラー）| Redis URL（DB index 含む、`redis://` or `rediss://`）|
| `KINTONE_STORE_REDIS_TLS` | `0` | **`rediss://` は常に TLS 有効（downgrade 不可）**。本フラグは `redis://` を使うとき `1` で TLS 強制 upgrade のみ。`rediss://` × `_TLS=0` 指定は無視（TLS 維持） |
| `KINTONE_STORE_REDIS_INSECURE_PLAINTEXT` | `0` | **非 localhost（`localhost` / `127.0.0.1` / `::1` 以外）に対する `redis://` 平文接続を `1` で明示的に許可**。デフォルト `0` では非 localhost への `redis://` は startup を拒否（`STORE_PLAINTEXT_FORBIDDEN`、新エラー code 一覧の 10 番目に正式追加）。dev 環境の手動 override 用 |
| `KINTONE_STORE_REDIS_PASSWORD` | （省略可） | Redis AUTH パスワード（URL に含めない場合）|
| `KINTONE_STORE_CACHE_BYPASS` | `0` | `1` で Cache 機能のみ無効化（CachingAPI を素通しにする）。TokenStore/SigningKey/idproxy 状態は通常通り。旧 `KINTONE_CACHE_DISABLE` の意味的代替 |
| `KINTONE_STORE_DYNAMODB_TABLE` | （未指定時 backend=dynamodb ならエラー）| DynamoDB テーブル名（全 backend が標準ビルドに同梱されているため build tag 不要）|
| `KINTONE_STORE_DYNAMODB_REGION` | （`aws.Config` のリージョン解決にフォールバック）| DynamoDB リージョン |
| `KINTONE_MCP_SIGNING_KEY_PEM` | （省略可）| PKCS#8 PEM 文字列。設定時は最優先（Storage を読まない）|

### 削除（即削除、ユーザー0 確定）

| 旧環境変数 | 削除理由 |
|-----------|---------|
| `KINTONE_TOKENS_PATH` | `KINTONE_STORE_SQLITE_DIR/kintone.db` に統合 |
| `KINTONE_CACHE_PATH` | 同上 |
| `KINTONE_CACHE_DISABLE` | **意味的に等価な代替を新設**: `KINTONE_STORE_CACHE_BYPASS=1`（cache のみ無効、TokenStore/SigningKey は通常通り）。旧フラグは即削除、breaking change として CHANGELOG に明記 |

### 設定範囲: env-only + 正本一元化

**判断**: `KINTONE_STORE_*` は **env のみ**で受け取り、`config.toml` の `[profiles.*]` セクションには **storage 関連を一切載せない**。

理由:
1. Storage 設定は K8s Secret / Lambda env / Vault からの注入が主要ユースケース。config.toml に書くと secrets が誤ってリポジトリにコミットされやすい
2. profile（`default` / `dev` / `prod`）は kintone domain と認証方式に紐づくが、Storage backend は実行環境（local / k8s / Lambda）に紐づくため意味論が異なる
3. profile 切替で Storage が切り替わると、token 共有可否が直感に反する挙動になる

**Storage 設定の正本は `internal/store/env.go` 一箇所に限定**。`internal/config/env.go` / `internal/config/resolver.go` には storage 関連フィールドを**追加しない**。実装と表示の二重化を避けるため:

- `kintone config show` の storage 表示は **`internal/cli/config.go` 専用アダプタで組み立てる**。具体的には `cli/config.go` 内で `store.LoadEnvFromGetenv(getenv)` を呼ぶ専用関数を作り、`config.Load` が返す `Resolved` に手動で merge する。`internal/config/config.go` 側には storage を持ち込まない（`config -> store` 結合点を作らない）。テスト時は `cli/config.go` の getenv 注入で storage env を制御
- `config show` 出力に追加されるのは **読み取り専用フィールド** `storage: {backend, location, source}`（既存 `cache_path` 削除と入れ替え）
- backend / location は `config show` でしか表示されず、`Resolved` 構造体には伝搬しない（caller は context の Container 経由で必要時だけ取得）

これにより「実際に使う backend」と「`config show` の表示」が同一ソースから来ることが保証される。

### Location サニタイズ規約

`Container.LocationString()` / `cache stats` / `STORE_CONNECTION_FAILED.details.location` / `kintone config show` 等で接続先文字列を露出する箇所では、**必ず以下のサニタイズを通す**:

| 入力 | サニタイズ後 | 規則 |
|------|-------------|------|
| `redis://user:pass@host:6379/0` | `redis://user:***@host:6379/0` | userinfo の password を `***` |
| `rediss://:pw@host/0` | `rediss://:***@host/0` | 空 user + password も同様 |
| `redis://host:6379/0?password=pw` | `redis://host:6379/0?password=***` | クエリ string |
| `dynamodb://table-name` | そのまま | 機密ではない |
| `sqlite:///home/user/.local/state/kintone/kintone.db` | そのまま | path はパスワードを含まない |

**完全に出力しないもの**（JSON / エラー詳細 / ログ）:
- `KINTONE_OAUTH_CLIENT_SECRET`
- `KINTONE_MCP_COOKIE_SECRET`
- `KINTONE_MCP_SIGNING_KEY_PEM`（**PEM 内容**は一切出さない、行頭・行末も含めて）
- `KINTONE_API_TOKEN`
- `KINTONE_STORE_REDIS_PASSWORD`

サニタイズ関数 `internal/output/sanitize.go::SanitizeURL(string) string` を新設し、unit test で以上のマトリクスを網羅。

**Backend エラーのサニタイズ規約**: Redis/DynamoDB クライアント由来の `err.Error()` には接続 URL / 認証情報断片が含まれる可能性がある。よって:

- `internal/cli/errors.go::output()` で `STORE_CONNECTION_FAILED` 系エラーは `details.cause` を出さず、`details.cause_class` (`network`/`auth`/`timeout`/`unknown`) のみを出力
- `internal/output/sanitize.go::ClassifyBackendError(err) string` で error → cause_class へ分類（`net.OpError` → `network`、`*redis.Error{Code: NOAUTH}` → `auth`、`context.DeadlineExceeded` → `timeout`、その他 → `unknown`）
- 元の error message は **stderr の slog.Error にだけ詳細ログ**として残す（オペレータは journald / kubectl logs 経由で見る、エンドユーザー JSON には出ない）
- これにより Redis URL `redis://user:secret@host` 由来の error が外部 JSON に漏れない

### 維持（Storage 体系には統合しない）

| 環境変数 | 維持理由 |
|---------|---------|
| `KINTONE_MCP_COOKIE_SECRET` | MCP HTTP 専用かつ固有の 32-byte hex 値。意味論が Storage と異なり、K8s Secret として独立に管理されるべき。README で MCP secret 2 種（CookieSecret / SigningKey）の役割を明記 |
| `KINTONE_MCP_OIDC_*`, `_EXTERNAL_URL` | OIDC プロバイダ設定は Storage 直交 |
| `KINTONE_OAUTH_CLIENT_ID/SECRET/...` | upstream OAuth 設定は Storage 直交 |

### 設定例（4 backend）

```bash
# Memory（dev / test）
export KINTONE_STORE_BACKEND=memory

# SQLite（host / single-instance、デフォルト）
export KINTONE_STORE_BACKEND=sqlite
export KINTONE_STORE_SQLITE_DIR=$HOME/.local/state/kintone

# Redis（k8s / Fargate scale-out）
export KINTONE_STORE_BACKEND=redis
export KINTONE_STORE_REDIS_URL=rediss://prod-redis.example.com:6380/0
export KINTONE_STORE_REDIS_PASSWORD=$REDIS_PASSWORD

# DynamoDB（Lambda / serverless）
export KINTONE_STORE_BACKEND=dynamodb
export KINTONE_STORE_DYNAMODB_TABLE=kintone-prod
export KINTONE_STORE_DYNAMODB_REGION=ap-northeast-1
```

---

## 6. SigningKey 永続化

### 判定軸の訂正: `auth=oidc` で SigningKey 必須

**判定軸の根拠**: SigningKey は idproxy の OIDC 認証層（JWT 発行 / cookie 署名検証等）が使うものであり、upstream kintone への AuthZ 方式（api-token / oauth）とは独立。`auth=oidc + authz=api-token` でも SigningKey なしでは idproxy セッション JWT が再起動で全失効する。

したがって **fail-fast 判定軸は `auth` モード**（`none` / `oidc`）であり、`authz` モードではない。

`BuildAuth(ctx, env, container)` の引数に解決済みの `authMode` / `authZMode` を明示的に渡し、内部で `authMode == "oidc"` を見て fail-fast を判定する（CLI フラグ + env から事前解決した結果を使う）。

### 解決順序（`internal/idproxy/signingkey.go::resolveSigningKey`）

判定の優先順位を**禁止組合せの先取りチェック**に変更し、`STORE_MEMORY_OIDC_FORBIDDEN` と `SIGNING_KEY_REQUIRED` の競合を解消する:

```
0. 【最優先】validateBackendForAuthMode(authMode, env.StoreBackend):
   - authMode == "oidc" かつ env.StoreBackend == "memory" → ErrMemoryOIDCForbidden（STORE_MEMORY_OIDC_FORBIDDEN）
   - 他の禁止組合せ（将来追加分）も同様にここで先取り
1. KINTONE_MCP_SIGNING_KEY_PEM があれば PKCS#8 PEM パース → 採用
   （PEM 不正時は fail-fast、エラーメッセージは行頭・行末のみ表示し中身を leak しない）
2. なければ container.SigningKey().LoadOrCreate(ctx) を呼ぶ
   - Storage にあればロード
   - なければ ecdsa.GenerateKey(P256) → PKCS#8 PEM → put → 使う
   - **memory backend の場合、ここに到達した時点で既に Step 0 で ErrMemoryOIDCForbidden を返しているため、auth=oidc 経路では呼ばれない**
3. 上記のどちらでも取得できず、かつ authMode == "oidc" の場合
   → fail-fast で startup を拒否（exit code 非 0、JSON エラー code=SIGNING_KEY_REQUIRED）
4. authMode == "none" で SigningKey が解決できなかった場合のみ ephemeral 生成 + slog.Warn
```

**契約の正本**: 上記擬似コードを `internal/idproxy/signingkey.go` の docstring にそのまま転記し、TDD 手順 / 検証例 / エラー一覧 / リスク表は**この順序を共有する**。Phase 3 TDD で「memory + auth=oidc → STORE_MEMORY_OIDC_FORBIDDEN」「sqlite + auth=oidc + env なし + storage なし → SIGNING_KEY_REQUIRED」「memory + auth=none + env なし → ephemeral + WARN」の 3 ケースを必ず網羅。

### Fail-fast の根拠

WARN-only は本番運用で見落とされ、rolling restart で全 OIDC JWT が一斉失効するサイレント障害を生む。multi-replica 構成では複数 replica で signing key が分かれて常に 401 となる。

`auth=oidc` は MCP の OIDC 認証モードを意味し、SigningKey が必須機能（authz が api-token であっても idproxy セッション JWT を発行する）。ephemeral では本番運用が破綻する。

### Memory backend × auth=oidc は **全面禁止**

`KINTONE_STORE_BACKEND=memory` + `--auth oidc` の組み合わせは **env で SigningKey を与えても**、idproxy の session / auth_code / refresh_token state が memory のままなので：
- プロセス再起動で OIDC セッションが全失効
- multi-replica で session が replica ごとに孤立 → ログインしたインスタンスと違うインスタンスにアクセスすると 401

これは本計画の「分散環境対応」目的に根本的に反する。よって：

**fail-fast 条件**: `auth=oidc` かつ `backend=memory` → **env の有無に関わらず**、startup を拒否。`SIGNING_KEY_REQUIRED` ではなく専用エラー code `STORE_MEMORY_OIDC_FORBIDDEN` を返し、`details: {hint: "memory backend は scale-out / 再起動耐性を持ちません。auth=oidc 用には sqlite/redis/dynamodb を使用してください"}` を含める。

dev/test で `auth=oidc` の動作確認をしたい場合は `backend=sqlite` (`KINTONE_STORE_SQLITE_DIR=$(mktemp -d)`) を推奨。README に開発手順として明記する。

### スキーマ

- SQLite: `kintone_signing_keys (id TEXT PRIMARY KEY, pem TEXT NOT NULL, created_at INTEGER)` 行 `id='current'`
- Redis: `kintone:signingkey:current` PEM 文字列（永続のため EXPIRE なし）
- DynamoDB: PK=`kintone:signingkey:current`, attr `pem` (S)

`id` カラムは将来の `current/next` 切替（rotation, M13+）の余地を残すため **複数行設計**。

### SigningKey 自動生成の運用方針

`auth=oidc` での自動生成 → Storage 平文永続化は accidental secret creation のリスクが高いため、**v0.1 から外部供給必須**にする。

**方針（startup policy、最終形）**:
- **`auth=oidc` 経路**: `KINTONE_MCP_SIGNING_KEY_PEM` env が**必須**（or `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` で明示 opt-in）。どちらも無ければ **fail-fast**（`SIGNING_KEY_REQUIRED` エラー）
- **明示 opt-in（dev/test 用）**: `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` をユーザーが意識して設定した場合のみ Storage 自動生成 + 平文永続化を許可。startup で `slog.Warn` ログ
- **`auth=none` 経路**: SigningKey を使わないため env 不要

これにより「気づかぬうちに long-term 鍵が平文保存される」運用事故を**設計レベルで防止**。

README で「本番では `KINTONE_MCP_SIGNING_KEY_PEM` を外部供給すること、dev で簡便に試したいときは `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` を明示すること」を明示。

### Principal ID 規約（CLI ↔ MCP 整合性）

CLI の `kintone auth login --oauth --principal-id <id>` で保管される token と、MCP HTTP `auth=oidc` で OIDC から得られる principal は **同じ TokenStore キー**を共有する必要がある。

**規約**:

| 経路 | principal_id 形式 | 採用ルール |
|------|------------------|-----------|
| CLI 単体運用（auth=none / api-token のみ）| 任意文字列（例: `oauth:alice`、`local:dev`）| ユーザーが自由に命名 |
| MCP HTTP `auth=oidc` | `<issuer>:<sub>` 自動生成 | `internal/idproxy/principal.go` の `principalFromUser()` で OIDC claims から導出 |
| **CLI と MCP の token を共有したい運用**（同一ユーザーが両方を使う）| `<issuer>:<sub>` を CLI でも明示指定 | `kintone auth login --oauth --principal-id "https://accounts.google.com:1234567890"` のように OIDC claims に揃える |

**ドキュメント運用**: README に「multi-user MCP を使う場合、CLI で provisioning login する際は `--principal-id` を `<issuer>:<sub>` 形式で渡す」を明記。簡略化のため、`kintone auth login --oauth --oidc-issuer URL --oidc-sub SUB` のような shorthand フラグの追加は M13+ で検討（本マイルストーンでは導入しない）。

**alias / link コマンドの将来余地**: `kintone auth alias <from-principal> <to-principal>` で principal_id をリンクする alias テーブル設計を Storage interface に温存（M13+）。本マイルストーンでは regex 検証（`^[a-zA-Z][a-zA-Z0-9+\-:.@/_]*$`）のみ実装。

### Threat Model（簡易）

**保護対象**:
- OAuth refresh_token / API Token（kintone TokenStore）
- OIDC SigningKey PEM（kintone SigningKeyStore）
- idproxy session / cookie / refresh_token

**想定脅威**:
1. **DB ファイル盗難**（SQLite ローカル）: ファイル権限 0o600 + ホスト OS 制御で対処。AES-GCM 暗号化は M13+
2. **Redis 接続乗っ取り**: TLS 必須化（`rediss://`）+ AUTH password + ACL 最小権限 (`+@read +@write` のみ、`+@admin` 禁止)
3. **DynamoDB 不正アクセス**: 最小権限 IAM Policy（`PutItem` / `GetItem` / `DeleteItem` / `Query` / `BatchWriteItem` / `DescribeTable` のみ、`Scan` 禁止）+ KMS at rest 暗号化
4. **kintone OAuth 攻撃**: PKCE S256（実装済）+ state CSRF 検証 + refresh_token rotation（idproxy 側）
5. **SigningKey 漏洩**: KMS / Vault からの env 注入を推奨、Storage 平文保管は最終手段

**先送り（M13+）**: アプリケーション層 envelope encryption（AWS KMS / GCP KMS / HashiCorp Vault 連携）。本マイルストーンでは「at rest 暗号化はインフラ層に委譲」する設計判断を docs/decisions に記録。

**Redis ACL 最小権限例（README 記載）**:

```
# kintone CLI 用ユーザーは kintone:* / idproxy:* キーへの読み書きのみ、admin 系不可
ACL SETUSER kintone on >password \
    ~kintone:* ~idproxy:* \
    +@read +@write +@string +@hash +@scripting -@admin -@dangerous
```

`+@all` は使わない。

---

## 7. 実装フェーズ分割（11 フェーズ・推定 16.5 日）

### Phase 0 — 事実確認 + 設計確定（**新設**）

**目標**: idproxy v0.4.2 の実装と modernc.org/sqlite の挙動を Phase 1 着手前に検証し、設計判断を確定する。

**作業**:
1. `idproxy v0.4.2` の `store/sqlite/store.go` を読み、`New(path)` 以外の API（`NewWithDB` 相当）が存在しないことを最終確認
2. `idproxy v0.4.2` の `store/redis/store.go` を読み、`consume.lua` の実体を取得 → miniredis の Lua サポート範囲と突合
3. `idproxy v0.4.2` の `store.go`（DynamoDB 実装）を読み、以下を確認:
   - `Get*` 系が **読み出し時 expires_at チェック**を持つか（なければ kintone 側で defensive wrapper を入れる方針を確定）
   - 計画 §4 のテーブル定義（**PK=HASH(S) + GSI1（gsi1pk/gsi1sk）+ GSI2（gsi2pk/gsi2sk）+ ttl 属性**）が upstream `idpdynamodb.NewDynamoDBStoreWithClient(client, table)` の必須 KeySchema / AttributeDefinitions / TTL 属性名と整合するか
   - 不整合（例: upstream が PK 以外に SK を必須化、別 attr 名）の場合の対応案を 2 案以上立てる（テーブル分離 / upstream PR / 計画スキーマ調整）
4. modernc.org/sqlite の Pool 並行挙動を 50 行スクリプトで検証（同 path 2 Pool に goroutine 100 で BEGIN IMMEDIATE × 1000 回）。結果を `docs/decisions/0001-sqlite-pool.md` に記録
5. modernc.org/sqlite の DSN 構文（`_pragma=busy_timeout(5000)`）を確定し schema 適用テストコードに反映
6. **新 store interface 形状を確定**（Phase 1 の TokenStore / CacheStore / SigningKeyStore のメソッドシグネチャを `docs/decisions/0003-interface-compat.md` に固定）。旧 `tokenstore.Store` / `cache.Store` との method 形状一致を grep で確認し mock 互換性を担保

**Deliverable**:
- `docs/decisions/0001-sqlite-pool.md` — 「2 ファイル分離を採用」の根拠
- `docs/decisions/0002-idproxy-store-fact-finding.md` — DynamoDB expired チェック有無、Redis Lua の miniredis 互換性、**upstream DynamoDB store と計画テーブル定義（GSI1+GSI2）の整合性検証結果**
- `docs/decisions/0003-interface-compat.md` — 新旧 interface 形状の差分表（mock 影響範囲）
- 上記の結論で本計画書を必要に応じてアップデート

**Exit criteria（Phase 1 着手前に全部 PASS 必須）**:
- [ ] idproxy v0.4.2 の SQLite Store が `New(path)` のみと確認できた
- [ ] idproxy v0.4.2 の Redis Store の `consume.lua` が miniredis で動くか / 動かないなら CI に actions services redis を追加する判断が確定
- [ ] idproxy v0.4.2 の DynamoDB Store が **本計画 §4 のテーブル定義（PK=HASH(S) + GSI1 + GSI2 + ttl）で動作**することを `dynamodb-local` で実証、または不整合時の代替案が確定
- [ ] modernc.org/sqlite の DSN 構文と 2 Pool 並行挙動のテスト結果が pass
- [ ] 新 interface 形状が確定し、既存 `tokenstore.Store` / `cache.Store` との method 互換性が担保されている

**所要**: 1.5 日（exit criteria 1 件でも fail なら +0.5-1 日 buffer）

**Phase 0 を完了するまで Phase 1 着手禁止**

### Phase 0 結果分岐

Phase 0 の事実確認結果が前提と異なった場合の対応を後続フェーズで明示しておく:

| 確認項目 | 想定通り | 想定外時の代替案 |
|---------|---------|-----------------|
| idproxy SQLite Store API | `New(path)` のみ | upstream `NewWithDB` PR を打つ（追加工数 +3 日、kintone v0.5.0 の前提） / または kintone 側で fork |
| idproxy Redis Lua の miniredis 互換 | 動く | 動かない場合 CI に actions services redis を追加（追加工数 +0.5 日）/ unit test は miniredis のサポート範囲内に絞る |
| idproxy DynamoDB 実装 | `idpstore.NewDynamoDB(client, table)` 等の API + 計画 §4 のスキーマで動く | (a) attr 名前不一致 → kintone 側が upstream に揃える計画スキーマ更新（追加工数 +0.5 日）/ (b) GSI/SK 必須化要求 → 単一テーブル共有を諦めてテーブル分離（追加工数 +1.0 日）/ (c) 読み出し時 expires_at チェックなし → kintone 側で defensive wrapper 必須（追加工数 +0.5 日） |
| modernc.org/sqlite 2 Pool 並行 | 安定動作 | 不安定なら一段階厳格化（kintone 側でも `MaxOpenConns=1` 強制、追加工数 +0.5 日） |
| 新旧 interface 形状の互換 | 完全一致 | 不一致箇所をエンドツーエンドで grep + 影響テスト書き換え（追加工数 +1.0 日） |

最大 +6.5 日のバッファを Phase 0 結果次第で取る。本計画の 15 日工数はベストケース、想定外多数なら 20 日強。

---

### Phase 1 — internal/store パッケージ骨格 + Memory backend + Container/Factory

**変更ファイル一覧（新規）**:
- `internal/store/doc.go`
- `internal/store/container.go` — `Container` interface + `Close(ctx)` 所有権定義
- `internal/store/env.go` — `Config` struct + `LoadFromEnv()`
- `internal/store/factory.go` — `OpenFromEnv()` / `OpenFromConfig()` ディスパッチ（memory のみ実装、他は `ErrUnsupportedBackend`）
- `internal/store/tokens.go` — `TokenStore` interface + `Token` struct（旧 `tokenstore` から移植 + **`ListByDomain(ctx, domain) ([]Token, error)` を正式 interface 昇格**）
- `internal/store/cache.go` — `CacheStore` interface + **backend 非依存 `Stats` struct**（後述）
- `internal/store/signingkey.go` — `SigningKeyStore` interface（`LoadOrCreate(ctx)`）

**Logging 基盤**:

現コードベースは `log/slog` を使っていない（`internal/output` の JSON envelope のみ）。`auth=none + ephemeral SigningKey` の WARN 出力や `auto-generated key persisted in plaintext` 等の通知のため、最小 logging 基盤を Phase 1 で導入:

- `internal/output/logger.go` — `slog.New(slog.NewTextHandler(os.Stderr, ...))` を返す package-level logger（stdout は JSON 契約のため触らない、stderr のみ）
- 環境変数 `KINTONE_LOG_LEVEL=debug|info|warn|error`（既定: `warn`）で出力レベル制御
- 全 store / idproxy / signingkey 系 WARN ログは `output.Logger().Warn(...)` 経由で発火
- 単純な `log.Printf` も `Logger()` 経由に移行（既存 `internal/cli/root.go` の `log.Println` も改修）

これにより warning 依存の acceptance criteria（`SigningKey ephemeral`, `Cache fail-open`, `Redis TLS downgrade 拒否`, `auto-generated key persisted in plaintext`）が現実に動作する。

**移設対象の定数 / 関数**:

旧 `internal/cache/ttl.go` の `TTLApps` / `TTLFields` / `TTLListApps` 定数、旧 `internal/cache/path.go` の `DefaultCachePath`、旧 `internal/cache/store.go` のキー prefix 契約（`apps:` / `fields:` / `list_apps:`）は新パッケージ `internal/store/cache.go` 内に **同名・同値で移設**:

```go
// internal/store/cache.go
const (
    TTLApps     = 365 * 24 * time.Hour  // apps メタの TTL
    TTLFields   = 365 * 24 * time.Hour  // fields メタの TTL
    TTLListApps = 365 * 24 * time.Hour  // ListApps レスポンスの TTL
)

const (
    KeyPrefixApps     = "v1:app:"        // 既存 internal/cache 実装に合わせる
    KeyPrefixFields   = "v1:fields:"
    KeyPrefixListApps = "v1:list_apps:"
)

// CacheKey は scope と論理 key から SQLite/Redis/DynamoDB 共通の key を組み立てる
func CacheKey(scope, id string) string { return KeyPrefixOf(scope) + id }
```

`internal/service/api/caching.go` は import を `cache` → `store` に置換するだけで動くよう、定数名と値は完全互換。`cache clear --scope <name>` の scope→prefix 変換も `internal/store/cache.go::PrefixOfScope(scope) string` を介して同じテーブル参照する。

**TokenStore interface（最終形）**:

```go
type TokenStore interface {
    Get(ctx context.Context, domain, principalID string, authType AuthType) (*Token, error)
    Put(ctx context.Context, t Token) error
    Delete(ctx context.Context, domain, principalID string, authType AuthType) error
    // 既存 internal/tokenstore/sqlite.go::SQLiteStore.ListByDomain と完全同一シグネチャ。
    // 型アサーションで使われていた契約をそのまま正式 interface に昇格（mock 互換性維持）。
    // Token は pointer slice で返す（既存 *tokenstore.Token と同等の構造体）。
    ListByDomain(ctx context.Context, domain string, authType AuthType) ([]*Token, error)
    Close() error  // 各 backend の所有権ポリシーは Section 3 を参照
}
```

**重要**: `ListByDomain` は **既存実装シグネチャに完全一致**させた。Phase 0 exit criteria の「method 形状一致」前提が成立する。

**CacheStore.Stats（backend 非依存スキーマ）**:

```go
type Stats struct {
    Backend         string         `json:"backend"`            // "memory" / "sqlite" / "redis" / "dynamodb"
    Location        string         `json:"location"`           // sqlite:///path / redis://host / dynamodb://table
    Reachable       bool           `json:"reachable"`          // backend 接続済かつ初期化済み（SQLite: ファイル+OPEN, Redis: PING, DynamoDB: DescribeTable, Memory: 常 true）
    EntryCount      int64          `json:"entry_count"`        // 全 backend 必須
    ExpiredCount    *int64         `json:"expired_count"`      // null 可（DynamoDB は近似 or 省略）
    BackendSpecific map[string]any `json:"backend_specific"`   // backend 固有メタ (sqlite: {db_size_bytes:N}, redis: {memory_used_bytes:N}, dynamodb: {table_size_bytes:N or null})
}
```
- `internal/store/memory/{tokens,cache,signingkey,idproxy_adapter}.go`
- `internal/store/storetest/conformance.go` — backend を受け取り全ケース回す共通関数
- `internal/store/factory_test.go` — backend dispatch のテスト

**TDD 手順**:
1. **Red**: `OpenFromConfig({Backend: "memory"})` が non-nil Container + `Close(ctx)` 冪等
2. **Green**: memory backend を実装、`Tokens/Cache/SigningKey/IDProxyStore` が non-nil
3. **Red**: `storetest.Conformance(t, container)` で TokenStore CRUD サイクル
4. **Green**: memory tokens 実装
5. **Red**: Cache TTL 経過後 `ErrCacheMiss`
6. **Green**: memory cache 実装（lazy delete + cleanup goroutine、`Close(ctx)` 連携）
7. **Red**: SigningKey LoadOrCreate 初回生成・2 回目再ロード一致
8. **Green**: memory signingkey 実装
9. **Red**: `goleak.VerifyNone(t)` で goroutine leak ゼロ
10. **Green**: cleanup goroutine の `ctx.Done` listen 実装
11. **Refactor**: `Container.Close` の直列順序、エラーラップ統一

**リスク**: なし（純粋追加、既存に影響なし）

**所要**: 1.0 日

---

### Phase 2 — SQLite backend（kintone.db のみ）

**変更ファイル一覧（新規）**:
- `internal/store/sqlite/open.go` — `*sql.DB` を WAL+busy_timeout+synchronous=NORMAL で開きスキーマ適用、新規ファイルは 0o600
- `internal/store/sqlite/schema.sql` — 3 テーブル DDL
- `internal/store/sqlite/tokens.go` — 旧 `tokenstore/sqlite.go` を移植・テーブル名 `kintone_oauth_tokens` に変更
- `internal/store/sqlite/cache.go` — 旧 `cache/sqlite.go` 移植
- `internal/store/sqlite/signingkey.go`
- `internal/store/sqlite/store_test.go` — `storetest.Conformance` を SQLite backend で

**変更ファイル一覧（既存修正）**:
- `internal/store/factory.go` — backend=sqlite ケース追加（idproxy_adapter は Phase 3 で）

**TDD 手順**:
1. **Red**: `Conformance(t, OpenSQLite(t.TempDir()+"/kintone.db"))`
2. **Green**: 旧 sqlite.go 2 つを移植
3. **Red**: 新規ファイル 0o600 / 既存ファイルパーミッション維持
4. **Green**: `os.Stat → os.Chmod` ロジック移植
5. **Red**: 100 goroutine × 1000 BEGIN IMMEDIATE で SQLITE_BUSY が `busy_timeout` 内に解決される
6. **Green**: PRAGMA 設定確認
7. **Refactor**: Open/OpenIfExists の責務、CGO 禁止 import 重複除去

**リスク**:
- WAL の `-shm` / `-wal` ファイルが NFS 上で安定しない → README で「ローカル FS 必須」明示

**所要**: 1.0 日

---

### Phase 3 — SigningKey 永続化 + idproxy.Store 統合（**前倒し**）

**前倒し理由**: SigningKey は backend 横断の cross-cutting concern。Phase 4-5（Redis/DynamoDB）の前に SQLite で永続化を確立しておくと、Redis/DynamoDB は同 interface 実装するだけで済む。

**変更ファイル一覧（新規）**:
- `internal/idproxy/signingkey.go` — `resolveSigningKey(ctx, env, authMode, container) (*ecdsa.PrivateKey, error)` 切り出し
- `internal/store/memory/idproxy_adapter.go` — `idstore.NewMemoryStore()` ラップ
- `internal/store/sqlite/idproxy_adapter.go` — `idproxysqlite.New(idproxyPath)` を kintone.db とは**別ファイル** `idproxy.db` で開く

**変更ファイル一覧（既存修正）**:
- `internal/idproxy/config.go` — `BuildAuth` シグネチャに `authMode string, authZMode string, container store.Container` を追加、`resolveSigningKey` を呼ぶ
- `internal/store/container.go` — `IDProxyStore() (idproxy.Store, error)` メソッド追加（lazy init 対応）
- `internal/store/factory.go` — backend ごとに idproxy.Store も生成可能に（sqlite は別ファイル、lazy）
- `internal/store/env.go` — `KINTONE_MCP_SIGNING_KEY_PEM` 解決追加
- `internal/store/sqlite/open.go` — `OpenIDProxy(dir)` で `<dir>/idproxy.db` を別 `*sql.DB` で開く

**TDD 手順**（判定軸 auth=oidc）:
1. **Red**: `BuildAuth(ctx, env, "oidc", "oauth", container)` で env 設定時は Storage を読まずに渡された PEM を使う
2. **Green**: env 優先実装
3. **Red**: env なし + sqlite backend で初回 LoadOrCreate → 鍵生成 + 永続化、2 回目で同じ PEM
4. **Green**: PKCS#8 marshal/parse 実装
5. **Red**: env なし + memory backend + **`auth=oidc`** → Step 0 の `validateBackendForAuthMode` で fail-fast（exit code 1、**stdout に既存 CLI 契約通り JSON エラー envelope** `{"ok":false,"error":{"code":"STORE_MEMORY_OIDC_FORBIDDEN",...}}`）
6. **Green**: `authMode == "oidc"` チェックを `resolveSigningKey` 起動時に追加
7. **Red**: env なし + memory + **`auth=none`** → ephemeral + slog.Warn（テストロガーで検証）
8. **Green**: ephemeral fallback 実装
9. **Red**: env なし + sqlite + **`auth=oidc + authz=api-token`**（authz は api-token でも署名鍵必須）→ Storage から正常ロード、fail-fast にならない
10. **Green**: authz は判定軸に含めない（Copilot 指摘）
11. **Red**: `container.IDProxyStore().SetSession(...)` → `idproxy.db` ファイルに書かれ、kintone.db には書かれない（テーブル分離確認）
12. **Green**: 2 ファイル分離実装
13. **Red**: `Container.Close(ctx)` で両 sql.DB が閉じる、二重 Close で panic しない、lazy 未 init なリソースに触らない
14. **Green**: `sync.Once` で Close 冪等化
15. **Refactor**: idproxy パスの設定を `Container.Config` から引く

**リスク**:
- PEM フォーマット差異（PKCS#1 vs PKCS#8、EC vs PRIVATE KEY） → `x509.ParsePKCS8PrivateKey` 一次、`x509.ParseECPrivateKey` フォールバック、ドキュメントは PKCS#8 推奨
- 鍵がディスクに平文で残る → README で「SQLite ファイルは 0o600 / Redis/DynamoDB は KMS at rest を推奨」明記、M13 で keyring/KMS 統合検討

**所要**: 1.5 日

---

### Phase 4 — Redis backend

**変更ファイル一覧（新規）**:
- `internal/store/redis/open.go` — `redis.UniversalClient` を `KINTONE_STORE_REDIS_URL` から構築。**`rediss://` は常に TLS（downgrade 不可）**、`redis://` のとき `_TLS=1` で TLS 強制 upgrade。`rediss://` × `_TLS=0` は警告ログを出して TLS 維持
- `internal/store/redis/tokens.go` — `HSET kintone:tokens:{d}:{p}:{a}` フィールド単位
- `internal/store/redis/cache.go` — `SET kintone:cache:{key} EX ttl`
- `internal/store/redis/signingkey.go` — `SET kintone:signingkey:current` (no expire)
- `internal/store/redis/idproxy_adapter.go` — upstream `github.com/youyo/idproxy/store` パッケージの Redis 構築関数（**正確な API 名は Phase 0 で確認**、暫定的に `idpstore.NewRedisWithClient(client, "idproxy:")` として記載）

**go.mod 変更**: `github.com/redis/go-redis/v9` を直接依存へ昇格。

**CI 変更**: GitHub Actions の `services: redis: image: redis:7-alpine` を追加。`go test -tags=integration ./...` で actions services を使う。miniredis は unit test 用、actions services は integration 用。

**TDD 手順**:
1. **Red**: `miniredis/v2` 起動 → `OpenRedis(addr)` → Conformance（Phase 0 で miniredis 動作確認済の範囲）
2. **Green**: redis backend 実装
3. **Red**: kintone + idproxy 双方が同じ miniredis に書き、`KEYS *` で `kintone:` と `idproxy:` のみ存在しキー数衝突なし
4. **Green**: prefix 設計どおり実装
5. **Red**: TLS フラグ `KINTONE_STORE_REDIS_TLS=1` で `redis://` を TLS upgrade
6. **Green**: `redis.ParseURL` ベース構築
7. **Red**: `rediss://...` URL に対して `_TLS=0` を指定しても TLS が維持される（downgrade 不可）+ slog.Warn 出力
8. **Green**: `rediss://` 検出時の `_TLS=0` 無視 + WARN
9. **Integration test (actions services)**: 実機 Redis で idproxy refresh CAS が通る、`consume.lua` がエラーなく実行される

**リスク**:
- miniredis の Lua 制限 → Phase 0 で確認済。動かなかった部分は actions services で integration test
- Cluster mode 非対応 → Day 1 で UniversalClient + hash tag 戦略をドキュメント化、Cluster は v0.5+

**所要**: 1.5 日

---

### Phase 5 — DynamoDB backend

**変更ファイル一覧（新規）**:
- `internal/store/dynamodb/open.go` — `aws.Config` ロード + **2 段階検証**:
  - **Open 時**: テーブル存在 + `pk` 属性 + ttl 属性のみ確認（mandatory: `auth=oidc` でも auth=oauth/api-token でも token 操作と signing key 保存に必要）
  - **`CacheForAdmin()` 初回呼び出し時**: GSI2 + `gsi2pk`/`gsi2sk` 属性を追加で確認（cache 操作で使うため）。`CacheForDecorator()` 経路は GSI 不要（`Get` のみ、PK 直接アクセス）
  - **`Tokens()` 初回呼び出し時**: GSI1 + `gsi1pk`/`gsi1sk` 属性を追加で確認（ListByDomain で使うため）
  - 各段階で不足があれば `STORE_GSI_MISSING` エラーを return（fail-fast）。`store init --capability` でユーザーが事前検証できる
- `internal/store/dynamodb/keys.go` — PK / GSI1 / GSI2 prefix 定数 + 衝突防止アサート lint test
- `internal/store/dynamodb/tokens.go` — PutItem/GetItem/DeleteItem + GSI1 attr 付与 + `Query on GSI1` (ListByDomain、Scan 不使用)
- `internal/store/dynamodb/cache.go` — `expires_at` (ns) + `ttl` (sec) 両方付与 + GSI2 attr 付与 + `Query on GSI2`（Stats / Clear --key）+ 読み出し時 `expires_at` チェック必須
- `internal/store/dynamodb/signingkey.go`
- `internal/store/dynamodb/idproxy_adapter.go` — upstream `github.com/youyo/idproxy/store` パッケージの DynamoDB 構築関数を呼ぶ（**正確な API 名は Phase 0 で確認、本計画では暫定的に `idpstore.NewDynamoDB(client, table)` 等として記載するが Phase 0 の事実確認結果で確定**）+ defensive wrapper（必要なら）

**go.mod 変更**: `github.com/aws/aws-sdk-go-v2/service/dynamodb` + `aws-sdk-go-v2/config` を直接依存へ昇格（**全 backend を標準ビルドに同梱**、build tag は使わない）。

**配布変更**: GoReleaser config は単一構成のまま。Homebrew / GitHub Releases binary / ghcr.io Docker すべてに DynamoDB を含む。バイナリサイズは ~25MB → ~45MB クラスに増えるが、ユーザー要件「Memory + SQLite + Redis + DynamoDB の 4 つすべて」を厳密に満たす。

**`kintone version` 出力**: 既存契約を**維持**（`{"data":{"version":"0.1.0"}}` のみ）。「対応 backend 一覧を含める」案は撤回。backend 一覧が必要な場合は `kintone store list-backends` のような専用コマンドを将来検討（M12 のスコープ外）。

**TDD 手順**:
1. **Red**: `dynamodb-local` testcontainer 起動 → テーブル + **GSI1 + GSI2** 作成 → `OpenDynamoDB(table)` → Conformance
2. **Green**: dynamodb backend 実装
3. **Red**: kintone + idproxy が同テーブルに書き、`Scan` で全 PK 取得して prefix が `kintone:` / 6 idproxy prefix のみ
4. **Green**: prefix 戦略 + lint test
5. **Red**: TTL 削除遅延中の expired item を `Get` が拾わない（読み出し時 `expires_at < now()` チェック）
6. **Green**: defensive wrapper 実装
7. **Red**: ListByDomain が GSI1 Query で動き、Scan を発行しない（aws SDK middleware で `ScanInput` 検出 → fail）
8. **Green**: GSI1 Query 実装
9. **Red**: `Cache.Stats() entry_count` が **GSI2 Query**（KEYS_ONLY）で取得できる（Scan 不使用）
10. **Green**: GSI2 Query 実装
11. **Red**: `Cache.Clear(--key prefix)` が **GSI2 Query** + `BatchWriteItem` 25 件ずつ削除
12. **Green**: BatchWriteItem ループ + retry 実装
13. **Red**: GSI 不在のテーブルで `OpenDynamoDB` が `STORE_GSI_MISSING` を返す
14. **Green**: `DescribeTable` 結果から GSI1/GSI2 検証
15. **Integration test**: localstack または dynamodb-local で full flow（login → list → cache stats → cache clear --key → refresh → logout）

**リスク**:
- DynamoDB Local の TTL は eventual delete（実機より遅延）→ 読み出し時チェックで対処
- IAM 権限不足 → 起動時 `DescribeTable` で疎通確認 + README に最小 IAM ポリシー JSON（`Query on Index/gsi1` `Query on Index/gsi2` 必須）
- バイナリサイズ膨張（~25MB → ~45MB）→ 受容（ユーザー要件「全部」遵守）。README で配布サイズを明示し DynamoDB 不要ユーザーへの妥当性を説明
- BatchWriteItem の ProvisionedThroughputExceededException → aws-sdk-go-v2 retry middleware で exponential backoff (max 3 retries)

**所要**: 2.5 日

---

### Phase 6 — CLI / MCP 配線変更（**Wave 別 PR**）

**Wave A**: 方針再考

旧パッケージ (`internal/tokenstore` / `internal/cache`) の公開 API は interface（`Store` / `Token` / `AuthType` / `ErrNotFound`）に加えて、`Open` / `OpenIfExists` / `DefaultCachePath` / `TTLApps` / `TTLFields` / `TTLListApps` / `Stats` 構造体（`DBPath` / `DBExists` 等）など多面的に使われている。**「薄い shim」では完全互換が現実的でない**。

**判断**: shim を諦めて **同 wave でまとめて移行**する方針に変更。これにより Wave A は「新 Storage interface 完全実装」に専念し、Wave B で**全 caller を 1 PR で移行**、旧パッケージは Phase 7 で削除（変更ファイルは多いが、ビルド可能性ギャップは生まない）。

- Wave A（PR 1）: `internal/store` を完全実装（Phase 1-5 で完成済み）。旧パッケージへの変更なし。
- Wave B（PR 2、~700-1000 行）: 全 caller を一括で `store` 経由に切り替え。**旧 import を残さない**。CI で `grep -r "internal/tokenstore\|internal/cache\b" --include='*.go' .` がヒットゼロを確認

**Wave B**（1 PR、~700-1000 行）— **全 caller を一括で `store` 経由に切替**

shim を作らない方針のため、import 切替・ライフサイクル統合・新エラー envelope を 1 PR にまとめる。レビュー量は増えるが、ビルド不能な中間状態を作らないメリットが上回る。

含む変更:
- `internal/cli/auth/{login,status,logout,helpers}.go` を Storage 経由に。`store.ContainerFromContext(ctx)` で root 提供 Container を取得し `Tokens()` のみ呼ぶ
- `internal/cli/cache/{clear,stats,helpers}.go` を Storage 経由に
  - `Stats` 出力を新スキーマ（`backend`, `location`, `reachable`, `entry_count`, `expired_count`, `backend_specific`）に変更
  - `Cache.Clear(--key prefix)` を backend 別に実装: SQLite=`DELETE WHERE key LIKE prefix%`、Redis=`SCAN MATCH prefix*` + UNLINK、DynamoDB=`Query GSI2` + BatchWriteItem
- `internal/cli/{api,ops}/helpers.go` を Storage 経由に。`auth=api-token` + `KINTONE_STORE_CACHE_BYPASS=1` のときは Container を Open しないファストパス
- **MCP の per-principal API 解決を完成させる**
  - **既存実装の活用**: `facade.ToolDeps.Factory APIResolver` フィールドは既に `internal/mcp/facade/facade.go` に存在、`PrincipalAPIFactory` も `internal/service/api/principal.go` に実装済、`AUTH_REQUIRED` エラーマッピングも `internal/mcp/facade/errors.go` に既存。M12 の作業は **これらの既存パーツを Storage 経由に配線するだけ**（新規追加ではない）
  - **Factory 構築は `internal/cli/mcp/serve.go` に閉じる**。`internal/mcp/server` は引数で受け取った `facade.ToolDeps` を facade に渡す transport 専用層を維持
  - `internal/cli/mcp/serve.go` の `runHTTP` で `authzMode == "oauth"` のとき `service/api.NewPrincipalAPIFactory(container.Tokens(), ...)` を構築 → 既存 `facade.ToolDeps{API: nil, Factory: factory}` を組み立て → `mcpserver.NewWithDeps(deps)` に渡す
  - `runStdio` は static `API` を `ToolDeps.API` に注入（authz=oauth は HTTP のみで意味を持つため Factory 不要、既存挙動）
  - `internal/cli/mcp/{helpers,idproxy_glue}.go` を Storage 経由に変更（`tokenstore` import を `store` に置換）
  - `BuildAuth(ctx, env)` → `BuildAuth(ctx, env, authMode, authZMode, container)` シグネチャ変更
  - `internal/mcp/server/server.go` に `NewWithDeps(deps facade.ToolDeps) *Server` を追加（既存 `New(api)` は薄い wrapper として残し最終的に削除）
  - `internal/idproxy/config.go::BuildAuth` で `authMode == "oidc"` 軸の SigningKey 解決（Phase 3 で確立済）

  **既実装の作業差分と独立サブステップ（Phase 6 工数再ベースライン化）**: 既に存在する `Factory` interface / `PrincipalAPIFactory` / `AUTH_REQUIRED` mapping を再実装する必要はない。**ただし配線は単純ではない**ので以下を独立サブステップに分解:

| サブステップ | 内容 | 工数 |
|-------------|------|------|
| B-MCP-1 | `internal/mcp/server/server.go` に `NewWithDeps(deps facade.ToolDeps) *Server` 追加（既存 `New(api)` は wrap） | 0.3 日 |
| B-MCP-2 | `internal/cli/mcp/serve.go` の `runHTTP` で authzMode 分岐し `NewPrincipalAPIFactory(Store, Base, Fallback)` を構築（`PrincipalAPIFactory` は `Base` と `Fallback` を要求するため、これらの解決ロジックも追加） | 0.5 日 |
| B-MCP-3 | `internal/cli/mcp/{helpers,idproxy_glue}.go` を Storage 経由に変更 + `BuildAuth` シグネチャ変更 | 0.4 日 |
| B-MCP-4 | 既存 `internal/cli/mcp/serve_test.go` / `internal/mcp/server/server_test.go` を `NewWithDeps` 互換に更新 | 0.3 日 |
| B-MCP-5 | `internal/idproxy/config.go::BuildAuth` で `authMode == "oidc"` 軸の SigningKey 解決配線 | 0.3 日 |

合計 1.8 日（当初 0.5 日と過小評価していた）。Wave B 全体は 1.5 日 → 2.5 日に修正。
- root command の `PersistentPreRunE` で Container Open + 各 RunE 内 `defer store.CloseIfOwner(ctx)` で必ず Close（前述 Section 3、`PersistentPostRunE` は使わない）
- `kintone store init` root 登録（Phase 8 と Wave B で同期、後述）
- 新エラー code 10 種を `internal/store/errors.go`（**唯一の正本**）に sentinel error として定義（`var ErrTableNotFound = errors.New(...)` 形式）。`internal/cli/errors.go` と `internal/mcp/facade/errors.go::MapError` は **sentinel → output code mapping のみ**を保持し、code 文字列の重複定義はしない

**Wave C → Phase 7 に統合**（L14 反映）: shim 削除を独立フェーズに昇格（後述 Phase 7）。Wave C は Phase 6 から削除。

**新規エラー code 追加（正本一覧で統一）**:

| Code | 意味 | details スキーマ |
|------|------|-----------------|
| `STORE_TABLE_NOT_FOUND` | DynamoDB テーブル未作成 | `{table, region}` |
| `STORE_GSI_MISSING` | DynamoDB GSI1 / GSI2 不足 | `{missing_indexes: []string, missing_attributes: []string, suggested_ddl: string}` |
| `STORE_TTL_DISABLED` | DynamoDB TTL 未有効 | `{table, suggested_command: "aws dynamodb update-time-to-live ..."}` |
| `STORE_BACKEND_UNAVAILABLE` | 指定 backend が build 環境に含まれない（将来の build tag 復活時用、現状予約のみ）| `{backend}` |
| `STORE_CONNECTION_FAILED` | Redis / DynamoDB 接続失敗 | `{backend, location, cause_class}`（`cause_class` は upstream error の **クラス分類**: `network`/`auth`/`timeout`/`unknown`、生 `err.Error()` は出力しないで資格情報漏洩防止）|
| `STORE_MEMORY_OIDC_FORBIDDEN` | `auth=oidc` × `backend=memory` の禁止組合せ | `{hint}` |
| `SIGNING_KEY_REQUIRED` | `auth=oidc` + SigningKey 解決失敗 | `{searched_sources: ["env", "storage"]}` |
| `STORE_CACHE_BYPASS_INVALID` | `KINTONE_STORE_CACHE_BYPASS` の値が `0`/`1` 以外 | `{value}` |
| `RESOLVER_PRINCIPAL_NOT_FOUND` | MCP per-principal Factory が指定 principal_id の Token を引けない | `{principal_id, domain, hint}` |
| `STORE_PLAINTEXT_FORBIDDEN` | 非 localhost への `redis://` 平文接続を `KINTONE_STORE_REDIS_INSECURE_PLAINTEXT=1` 無しで試みた | `{url_host}` |

これらは `internal/cli/errors.go` と `internal/mcp/facade/errors.go::MapError` の**両方に正本として登録**し、README / spec / CHANGELOG / テストはこの 9 項目を共通リストとして参照する（FALSE な追加・命名揺れを禁止）。

`MapError` のマッピング:
- `errors.Is(err, store.ErrTableNotFound)` → `STORE_TABLE_NOT_FOUND`
- `errors.Is(err, store.ErrGSIMissing)` → `STORE_GSI_MISSING`
- `errors.Is(err, store.ErrTTLDisabled)` → `STORE_TTL_DISABLED`
- `errors.Is(err, store.ErrConnectionFailed)` → `STORE_CONNECTION_FAILED`
- `errors.Is(err, store.ErrMemoryOIDCForbidden)` → `STORE_MEMORY_OIDC_FORBIDDEN`
- `errors.Is(err, idproxy.ErrSigningKeyRequired)` → `SIGNING_KEY_REQUIRED`
- `errors.Is(err, store.ErrPrincipalNotFound)` → `RESOLVER_PRINCIPAL_NOT_FOUND`

**変更ファイル一覧（最終的に修正される）**:

**追加で棚卸しされたテストファイル（既存 `internal/cache` / `internal/tokenstore` 依存）**:
- `internal/service/api/caching_test.go` — `cache.Store` mock を `store.CacheStore` mock へ
- `internal/auth/oauth/provider_test.go` — `tokenstore.Store` mock を `store.TokenStore` mock へ
- `internal/cli/auth/login_test.go` / `status_test.go` / `logout_test.go` — `tokenstore` import を `store` へ、mock も追従
- `internal/cli/cache/cache_test.go` — db_path/db_exists の JSON 検証は既掲載
- `internal/auth/oauth/refresh_test.go` (もしあれば) — 同上



**ストア層・サービス層**:
- `internal/auth/oauth/{provider,refresh}.go` — import 切替（`tokenstore` → `store`）
- `internal/service/api/principal.go` — `Store tokenstore.Store` → `Store store.TokenStore`
- `internal/service/api/principal_test.go` — Store mock を新 interface に追従
- `internal/service/api/caching.go` — import 切替 + `KINTONE_STORE_CACHE_BYPASS` フラグ対応

**CLI 層**:
- `internal/cli/auth/helpers.go` — `defaultOpenTokenStore` を `store.OpenFromEnv` 経由に
- `internal/cli/auth/{login,status,logout}.go` — **root の `--profile` / `--config` flag を尊重する**ように修正。`getCLIOptions(c)` を root に追加し、auth サブコマンドが root の persistent flag を取得できるようにする
- `internal/cli/auth/{login,logout,status}.go` — Store 経由に切替（既存 `ListByDomain(ctx, domain, authType)` シグネチャ維持）
- `internal/cli/cache/{clear,stats,helpers}.go` — Storage 経由 + 新 Stats schema
- `internal/cli/cache/cache_test.go` — **db_path/db_exists の JSON 検証を新 schema (backend/location/reachable) に置換**
- `internal/cli/{api,ops}/helpers.go` — Storage 経由
- `internal/cli/mcp/{helpers,serve,idproxy_glue}.go` — Storage 経由 + per-principal Factory 注入
- `internal/cli/mcp/serve_test.go` — `mcpserver.NewWithDeps` への追従
- `internal/cli/api/helpers.go` — `KINTONE_CACHE_DISABLE` 分岐削除 → `KINTONE_STORE_CACHE_BYPASS` 対応
- `internal/cli/errors.go` — 新エラー code 追加（M13）
- **`internal/cli/config.go`** — `cache_path` JSON フィールドの扱いを変更（**`location` フィールドへリネーム** or **削除**、後者なら `config show` テスト群に breaking change として反映）
- `internal/cli/config_test.go` — 新 schema に追従

**MCP 層**:
- `internal/mcp/server/server.go` — `New(api)` の隣に `NewWithDeps(deps facade.ToolDeps)` を追加（transport 専用層に store 解決ロジックは入れない）
- `internal/mcp/server/http.go` — **変更なし**（transport 専用、Factory ロジックは `cli/mcp/serve.go` 側が担う）
- `internal/mcp/facade/facade.go` — `RegisterTools(srv, deps)` 経由で `Factory != nil` のとき per-principal 解決を行う既存実装の活用
- `internal/mcp/facade/errors.go` — MapError 拡張（M13）

**設定層**:
- `internal/config/config.go` — **`Resolved.CachePath` のみ削除**。`KINTONE_STORE_*` 関連は一切追加しない（Storage 設定の正本は `internal/store/env.go`）
- `internal/config/env.go` — `KINTONE_CACHE_PATH` のみ削除。`KINTONE_STORE_*` は store パッケージ側のみ
- `internal/config/env_test.go` — `CachePath` 関連テスト削除のみ
- `internal/config/resolver.go` — `CachePath` propagate を削除のみ（`StoreBackend` 等は追加しない）
- `internal/config/resolver_test.go` — `TestResolve_CachePathPropagated` を削除
- **`internal/cli/config.go`** の `config show` 出力は内部で `store.LoadEnvFromOS()` を呼んで storage view を組み立てる（read-only 表示専用、`Resolved` 経由ではない）

**idproxy 層**:
- `internal/idproxy/config.go` — `BuildAuth` シグネチャ変更
- `internal/idproxy/signingkey.go`（Phase 3 で新設、Phase 6 で配線）

### Unit test seam の維持方針

既存コードは `openTokenStoreFn` (`internal/cli/auth/helpers.go::SetOpenTokenStoreFn`) や `NewAPIBuilder` (`internal/cli/api/helpers.go`) や `defaultNewStoreBuilder` (`internal/cli/cache/helpers.go::SetNewStoreBuilder`) で **テスト hook** を提供しており、これにより auth/mcp/api/cache 系の unit test は real DB / real config に依存せずに通っている。

**新 storage 層でも同等の seam を維持**:
- `internal/cli/auth/helpers.go::SetOpenStoreFn(func() (store.Container, error))` — 新規 hook、既存と同等の差し替え点
- `internal/cli/cache/helpers.go::SetNewContainerBuilder(...)` — Container 注入用 hook
- `internal/cli/{api,ops,mcp}/helpers.go::NewAPIBuilder` — Container を引数に取れるよう拡張
- `internal/mcp/server/server.go::NewWithDeps(deps)` — deps を直接注入できるため hook 不要、テストは構造体を直接渡せる

**ガイドライン**:
- 新 hook は既存と命名規則を揃える（`Set*` プレフィックス）
- hook の zero-value はプロダクション実装を返す default function
- hook を使わない integration test は `t.Setenv("KINTONE_STORE_BACKEND", "memory")` で memory backend を強制起動

**hook と context の優先順位**:

サブコマンドが Container を取得する順序を以下に**固定**:

```go
func getContainer(c *cobra.Command) (store.Container, error) {
    // 1. context から取得（root の PersistentPreRunE が注入したケース）
    if container := store.ContainerFromContext(c.Context()); container != nil {
        return container, nil
    }
    // 2. test hook（context が空 = root を経由しないテスト経路）
    if openFn := getTestOpenFn(); openFn != nil {
        return openFn()
    }
    // 3. 直接 Open（lib 経由で呼ばれた場合のフォールバック）
    return store.OpenFromEnv()
}
```

**ルール**: context があれば hook を無視（context 優先）。これにより本番経路（root 経由）と test 経路（hook 経由）が同居しても挙動がぶれない。auth/cache/api/mcp/root の各系統で **少なくとも 1 本ずつ「context 注入経路で hook が無視される」テストを必須化**。

**Container Close 所有権の確定表**:

| 取得経路 | 誰が Open するか | 誰が Close するか | leak 防止テスト |
|---------|-----------------|------------------|----------------|
| context 注入（本番、`ExecuteWith` 経由）| root の PersistentPreRunE | **`ExecuteWith` の defer のみ**（一箇所に集中、サブコマンド側 defer は廃止）| `internal/cli/root_test.go::TestExecuteWithClosesContainer` |
| test hook（hook 経由）| test 関数本体 | test 関数本体（`defer container.Close(ctx)`）| 各 hook 呼び出しのテストで `goleak.VerifyNone(t)` |
| direct open（lib 経由のフォールバック）| caller | caller | `internal/store/storetest/lifecycle_test.go::TestDirectOpenLeak` |
| `cmd.Execute()` 直接呼出（root_test）| test 関数で hook 注入 | hook 注入時の test 責任（`t.Cleanup` で Close）| 既存 test pattern を維持 |

`ExecuteWith` 内で `sync.Once` で 1 度だけ Close。「複数のサブコマンドの defer に依存して仕込み漏れする」リスクを設計上排除。

これにより既存の 22+α パッケージの unit test を integration test 化させずに移行できる。Phase 0 で hook の現存箇所を grep し `docs/decisions/0003-interface-compat.md` に列挙、Wave A で seam を新 store 層に移植してから Wave B で caller を切替える順序を厳守。

**TDD 手順** (各 Wave で):
1. **Red**: 新 Storage 経由で `kintone auth login` のテストが pass
2. **Green**: helpers.go を Storage 経由に切替（lazy init で Tokens() のみ呼ぶ）
3. **Red**: `kintone mcp serve --listen :0 --auth oidc --authz oauth` で `NewPrincipalAPIFactory` が構築され `ToolDeps.Factory` に注入される
4. **Green**: `mcpserver.NewWithDeps` 実装 + serve.go 配線
5. **Red**: per-request に Bearer JWT 検証 → Principal 抽出 → `Factory.ForContext(ctx)` で当該 principal_id の Tokens を引いて API 構築
6. **Green**: PrincipalAPIFactory.ForContext を MCP middleware から呼ぶ実装
7. **Red**: `kintone cache stats` の出力が新スキーマ `{"backend":"sqlite","location":"sqlite:///...","entry_count":12,...}`
8. **Green**: backend 非依存 Stats schema 実装
9. **Red**: `KINTONE_STORE_CACHE_BYPASS=1` で `kintone api records get` が CachingAPI を経由せず upstream を直接呼ぶ
10. **Green**: `service/api.NewCachingAPIIfNotBypassed` 等の bypass フラグ実装
11. **Red**: DynamoDB GSI 不足のテーブルで `kintone mcp serve --auth oidc` が `STORE_GSI_MISSING` で fail-fast
12. **Green**: `DescribeTable` 結果の検証 + エラー envelope
13. **Refactor**: `KINTONE_CACHE_DISABLE` 削除に伴うコード簡素化

**リスク**:
- mock 互換性 → Phase 0 で interface 形状を `docs/decisions/0003-interface-compat.md` に確定
- Wave 間で旧パッケージが残る期間 → CI ゲートで `go vet ./...` + `go build ./...` + 全テスト pass を必須に
- lazy init: read-only 系（`version`, `completion`, `config show`）は `store.OpenFromEnv` を呼ばない or 呼んでも sub-store のアクセサを呼ばない → DB ファイル副作用ゼロ
- `mcpserver.NewWithDeps` シグネチャ変更によるテストファイル修正範囲 → Phase 0 で grep 確認

**所要**: 3.5 日（Wave A: 1日、Wave B × 4: 2日、Phase 6 全体の Factory 注入分 +0.5日）

---

### Phase 7 — 旧パッケージ削除（旧 Wave C を統合・L14 反映）

**Phase 6 完了後に独立 PR で実施**。Wave C を Phase 7 として明示的に分離することで、Phase 6 の caller 移行と shim 削除を異なる PR として review 容易にする。

**変更ファイル一覧（削除）**:
- `internal/tokenstore/` ディレクトリ完全削除（shim を含む全ファイル）
- `internal/cache/` ディレクトリ完全削除（shim を含む全ファイル）
- 関連テストファイル

**TDD 手順**:
1. **Red**: `grep -r "internal/tokenstore\|internal/cache\b" --include='*.go' .` でヒットゼロ（shim も含めて完全消滅）
2. **Green**: 残存 import を `store` 経由に書き換え
3. **Red**: `go build ./...` が通る
4. **Green**: 全 import 書き換え漏れを修正
3. **Red**: `go test -race ./...` 全パッケージ pass
4. **Green**: テストの Open 系を全て `store.OpenFromConfig` に置換
5. **Refactor**: 重複ヘルパーの統合、go vet / gofmt / golangci-lint クリア

**所要**: 0.5 日

---

### Phase 8 — `kintone store init` コマンド（DynamoDB 専用テーブルチェック）

**変更ファイル一覧（新規）**:
- `internal/cli/store/init.go` — `kintone store init dynamodb --table NAME --region REGION` で DescribeTable + 完全な runtime 契約検証
- `internal/cli/store/store.go` — store サブコマンド root

**検証対象（runtime 契約と完全に一致、capability 単位）**:

`store init` は `--capability {full,token,cache,signingkey,idproxy}` フラグで検証範囲を切替（既定: `full`）。これにより、運用上「token のみ使う auth login が cache 用 GSI2 不備で起動失敗する」を回避。

| capability | 必須スキーマ |
|-----------|-------------|
| `token` | `pk`, `gsi1pk`, `gsi1sk`, GSI1（**ttl 属性は不要**、token item は TTL を持たない）|
| `cache` | `pk`, `gsi2pk`, `gsi2sk`, GSI2, ttl 属性 |
| `signingkey` | `pk` のみ |
| `idproxy` | `pk`, ttl 属性 |
| `full` | 上記すべて（GSI1 + GSI2 + ttl 属性）|

**runtime での挙動**: `Container.OpenFromEnv` 時は **mandatory な capability のみ**を検証（`auth=oidc + cache_bypass=1` なら `signingkey + idproxy + token` のみ）。cache の検証は Cache() が初めて呼ばれた時に lazy 実行し、不足時は **fail-open**（warn ログ + nil 返却）。

不足時のエラーは `STORE_GSI_MISSING` とし、`details: {capability: "cache", missing_indexes: ["gsi2"], missing_attributes: ["gsi2pk"], suggested_ddl: "..."}` で `aws dynamodb update-table` 用の DDL 提案を含める。

**TDD 手順**:
1. **Red**: `kintone store init dynamodb --table missing --region us-east-1` で `STORE_TABLE_NOT_FOUND` エラー JSON
2. **Green**: DescribeTable 呼び出し + エラーマップ
3. **Red**: GSI1 のみのテーブルで `STORE_GSI_MISSING` エラー（`details.missing_indexes=["gsi2"]`）+ DDL 提案
4. **Green**: GSI1 + GSI2 同時検証実装
5. **Red**: TTL 未有効テーブルで `STORE_TTL_DISABLED` エラー + 有効化コマンド提案
6. **Green**: TTL 検証実装
7. **Red**: 属性定義不足（`gsi2pk` 欠落）で `STORE_GSI_MISSING` の details に該当属性が列挙される
8. **Green**: AttributeDefinitions 検証実装

**root 登録**: `internal/cli/root.go` に `cmd.AddCommand(store.NewCmd())` を追加。`store` サブコマンドのトップレベルからは `init dynamodb` のみ提供（Phase 8 では他 backend には init 不要のためサブコマンド追加なし。将来 redis init の必要が出たら拡張）。

**`store init` の経路**: `store init dynamodb` は **Container を経由しない**（`needsStore=false`、ブートストラップ用途のため）。代わりに、`internal/cli/store/init.go` 内で **直接 `dynamodb.NewFromConfig()` でクライアントを構築し** `DescribeTable` / GSI/TTL 検証を行う。既存 `Container.CacheForAdmin()` の利用先表記から `store init` を除外する（誤記訂正）。`CacheForAdmin()` は `cache stats / cache clear` の 2 経路でのみ利用される。

**所要**: 0.5 日

---

### Phase 8.5 — E2E ハーネス整備（OIDC stub + Makefile + CI matrix）

**新設フェーズ**: Section 12 で必須にした `tools/oidcstub/`、`make test-e2e`、CI matrix 拡張を独立タスクとして実装フェーズ化する。これがないと CI 上で OIDC + 4 backend の e2e 検証が実行できない。

**E2E スコープの再定義**: kintone OAuth Server / kintone REST API は実機・モックいずれも CI で起動するコストが高いため、**E2E の対象は MCP OIDC 層 + Storage 層に限定**する。

**E2E backend マトリクス（memory 除外）**:

| Backend | E2E 対象 | 理由 |
|---------|---------|------|
| memory | × | `auth=oidc` で startup 拒否、E2E シナリオが成立しない |
| sqlite | ○ | 主要 dev/single-instance シナリオ |
| redis | ○ | scale-out シナリオ |
| dynamodb | ○ | serverless シナリオ |

**OAuth login の CI 自動化**: 現行 `kintone auth login --oauth` はブラウザ起動 + loopback callback の対話フローのため、CI ではそのままでは動かない。E2E 用に **Token seed 専用 seam** を新設:

**E2E 実行形態**: E2E は **in-process Go test** で実装する（外部バイナリやシェルスクリプトではない）。

- 配置: `internal/store/storetest/seed.go`（build tag なしの public ヘルパ、ただし `// E2E 専用、本番コードから呼ばないこと` を godoc で明記）
- API: `func SeedTokenForE2E(container store.Container, t Token) error`
- E2E test: `internal/cli/mcp/serve_e2e_test.go`（build tag `e2e`）が in-process で `mcpserver.NewWithDeps(...)` を立ち上げ、`net/http/httptest` で OIDC stub と kintone-fake にバインドし、`SeedTokenForE2E` で refresh_token を seed してから `POST /mcp` を撃つ
- Makefile: `test-e2e: go test -race -tags=e2e ./internal/cli/mcp/...`
- 外部バイナリ（`go run ./cmd/kintone`）は使わない。CI matrix の services でも redis/dynamodb-local を起動するだけで、kintone CLI は test バイナリ内に含まれる

実機 OAuth ブラウザフローは `docs/manual-tests/M12.md` で manual 検証。

**CI E2E シナリオ（3 backend × 1 シナリオ）**:
1. **OIDC stub** で MCP の Bearer JWT 認証経路を検証
2. **kintone-fake サーバ**（最小 mock）で `/oauth2/token` `/k/v1/records.json` 等の限定エンドポイントを返す
3. シナリオ: `SeedTokenForE2E()` で refresh_token を直接 seed → `mcp serve --auth oidc --authz oauth` 起動 → `POST /mcp` Bearer JWT 付きで `records_query` tool を呼ぶ → kintone-fake が 401 を返す → kintone CLI が refresh トリガー → kintone-fake が新しい access_token を返す → records_query 再試行成功 → Storage に新 refresh_token が永続化されている

**変更ファイル一覧（新規）**:
- `internal/testsupport/oidcstub/oidcstub.go` — **in-process** 最小 OIDC プロバイダ（`net/http/httptest.NewServer` で起動。discovery / jwks / authorize / token / userinfo、RSA 鍵をテスト用にハードコード、Docker 化なし、外部バイナリなし）
- `internal/testsupport/kintonefake/kintonefake.go` — **in-process** kintone OAuth + REST API の最小 mock（`httptest.NewServer` 起動、refresh_token rotation 対応）
- `Makefile` — `test-quick` / `test-integration` / `test-e2e` ターゲットのみ
- `docs/manual-tests/M12.md` — **実機 kintone OAuth + 実 Redis Cluster + 実 AWS DynamoDB** のチェックリスト（CI E2E でカバーされない範囲）

**変更ファイル一覧（修正）**:
- `.github/workflows/ci.yml` — unit ジョブに加えて integration ジョブと e2e ジョブ（main push 時のみ）を追加。Section 12 の YAML テンプレートに従う
- `.github/workflows/oidcstub.yml`（新規）— oidcstub イメージビルド + ghcr.io push（kintone と独立リリース）

**TDD 手順**:
1. **Red**: `tools/oidcstub` 起動 → discovery / jwks / token endpoint が OIDC 仕様準拠の JSON を返す
2. **Green**: 最小実装
3. **Red**: `make test-e2e` で OIDC stub + sqlite backend + MCP HTTP サーバを立ち上げ、`POST /mcp` を Bearer JWT 付きで呼び 200 が返る
4. **Green**: e2e ハーネス実装
5. **Red**: 4 backend 全部で同じ e2e が pass
6. **Green**: matrix 拡張

**所要**: 1.0 日

---

### Phase 9 — ドキュメント更新

**変更ファイル一覧（修正）**:
- `README.md` / `README.ja.md` — Storage Backend セクション追加（4 backend 表）/ 旧 ENV 削除 / Lambda・Fargate 設定例 / DynamoDB 最小 IAM ポリシー / Redis ACL コマンド例
- `docs/specs/kintone_spec.md` — データストア節を「3 系統分散」から「単一 Storage」に書き換え
- `plans/kintone-roadmap.md` — Meta `ステータス` を `M12 進行中` → 完了時 `[x]` に / Progress に M12 セクション追加 / Architecture Decisions に「同 backend 別論理共存方針」「2 ファイル分離（SQLite）」「GSI1 設計（DynamoDB）」を追加 / Changelog エントリ
- `docs/decisions/0001-sqlite-pool.md` — Phase 0 成果物（既に存在）
- `docs/decisions/0002-idproxy-store-fact-finding.md` — Phase 0 成果物
- `docs/decisions/0003-interface-compat.md` — Phase 6 用
- `CHANGELOG.md`（新規 or 既存修正）— `## [BREAKING] M12 Unified Storage` セクション

**README 新規セクション例**:

```markdown
### Storage Backend

kintone CLI/MCP は認証情報・キャッシュ・OIDC 状態を 1 つの Storage バックエンドに保存します。

| Backend  | 用途                          | 主要 ENV                                              |
|----------|-------------------------------|------------------------------------------------------|
| memory   | dev / test                    | `KINTONE_STORE_BACKEND=memory`                       |
| sqlite   | host / single-instance（既定）| `KINTONE_STORE_SQLITE_DIR=...`                       |
| redis    | k8s / Fargate scale-out       | `KINTONE_STORE_REDIS_URL=rediss://...`               |
| dynamodb | Lambda / serverless           | `KINTONE_STORE_DYNAMODB_TABLE=...`                   |

全 backend で kintone TokenStore / Cache / OIDC SigningKey / idproxy session・refresh_token が
1 接続で共存します（key prefix で論理分離。SQLite のみ同ディレクトリ・2 ファイル分離）。

**MCP secret**: `KINTONE_MCP_COOKIE_SECRET`（cookie 暗号化）と `KINTONE_MCP_SIGNING_KEY_PEM`（OIDC JWT 署名鍵）は Storage と独立に管理してください。**`auth=oidc`** で SigningKey が解決できない場合 startup を拒否します（authz の値に依らない）。
```

**所要**: 0.5 日

---

### 推定総工数

| Phase | 内容 | 工数 |
|-------|------|------|
| 0 | 事実確認 + 設計確定（DynamoDB schema 検証含む）| 1.5 日 |
| 1 | 骨格 + memory + ListByDomain interface 昇格 | 1.0 日 |
| 2 | sqlite | 1.0 日 |
| 3 | SigningKey + idproxy 統合（auth=oidc 軸 fail-fast）| 1.5 日 |
| 4 | redis | 1.5 日 |
| 5 | dynamodb（GSI1 + GSI2）| 2.5 日 |
| 6 | CLI/MCP 配線（Wave A/B、Factory 注入は既存活用 + B-MCP/B-CACHE サブステップ独立化、エラー envelope、Container ライフサイクル）| 4.5 日 |
| 7 | 旧パッケージ削除（旧 Wave C 統合）| 0.5 日 |
| 8 | store init コマンド + root 登録 | 0.5 日 |
| 8.5 | E2E ハーネス（OIDC stub + kintone-fake + Makefile + GHCR workflow + CI matrix）| 1.5 日 |
| 9 | docs | 0.5 日 |
| **合計** | | **16.5 日** |

---

## 8. シーケンス図

### (a) CLI login → kintone.db に refresh_token 保存

```mermaid
sequenceDiagram
    participant U as User (browser)
    participant CLI as kintone auth login --oauth
    participant SF as store.OpenFromEnv()
    participant C as Container (sqlite)
    participant OF as oauth.Login()
    participant K as kintone OAuth Server

    CLI->>SF: OpenFromEnv()
    SF-->>CLI: Container{tokens, cache, signingkey, idproxy}
    Note right of C: kintone.db only opened<br/>(idproxy.db not touched in CLI)
    CLI->>OF: Login(domain, clientID, ...)
    OF->>U: open browser → authorize URL
    U->>K: 同意
    K->>OF: redirect with code
    OF->>K: POST /oauth2/token (code+verifier)
    K-->>OF: {access_token, refresh_token, expires_in}
    OF-->>CLI: Result{principalID, AccessToken, RefreshToken, ExpiresAt}
    CLI->>C: container.Tokens().Put(Token{Domain, PrincipalID, OAuth, ...})
    C->>C: INSERT INTO kintone_oauth_tokens ON CONFLICT DO UPDATE
    C-->>CLI: ok
    CLI->>U: stdout {"ok":true,"data":{"principal_id":"..."}}
    CLI->>C: container.Close(ctx)
```

### (b) MCP serve --auth oidc → SigningKey 解決と Storage 共有

```mermaid
sequenceDiagram
    participant CLI as kintone mcp serve
    participant SF as store.OpenFromEnv()
    participant C as Container
    participant Idp as idproxy.BuildAuth()
    participant SK as resolveSigningKey()
    participant Auth as idproxy.Auth
    participant Mux as http.ServeMux

    CLI->>SF: OpenFromEnv()
    SF-->>CLI: Container (kintone.db + idproxy.db both opened)
    CLI->>Idp: BuildAuth(ctx, env, container)
    Idp->>SK: env.SigningKeyPEM ?
    alt env present
        SK-->>Idp: parsed PKCS#8 key
    else env absent, container.SigningKey() succeeds
        SK->>C: container.SigningKey().LoadOrCreate(ctx)
        C-->>SK: persisted PEM
        SK-->>Idp: parsed key
    else env absent, auth=oidc
        SK-->>CLI: fail-fast: exit(1)
        Note right of CLI: stdout: {"ok":false,"error":<br/>{"code":"SIGNING_KEY_REQUIRED"}}<br/>(既存 CLI 契約)
    else env absent, AuthZ=api-token
        SK-->>Idp: ephemeral key + slog.Warn
    end
    Idp->>Idp: Config{Store: container.IDProxyStore(),<br/>OAuth.SigningKey: pk}
    Idp-->>CLI: *upstream.Auth
    CLI->>Mux: middleware = Auth.Wrap(PrincipalMiddleware(...))
    CLI->>Mux: ServeHTTP(:8080)
    Note right of Mux: 以後リクエストごとに<br/>idproxy.Store が idproxy.db に<br/>session/auth_code/refresh_token を読み書き
```

### (c) アクセストークン失効時のリフレッシュフロー

```mermaid
sequenceDiagram
    participant Req as MCP HTTP Request
    participant Mw as idproxy middleware
    participant Fac as PrincipalAPIFactory.ForContext
    participant Auth as oauth.Authenticator
    participant Tok as container.Tokens()
    participant Ref as oauth.Refresher
    participant K as kintone OAuth Server

    Req->>Mw: Bearer JWT
    Mw->>Mw: Validate (signed by container.SigningKey)
    Mw->>Fac: ctx with Principal
    Fac->>Tok: Get(domain, principalID, OAuth)
    Tok-->>Fac: Token{access, refresh, expires_at}
    alt access_token expired (skew 60s)
        Fac->>Ref: Refresh(ctx, refresh_token)
        Ref->>K: POST /oauth2/token (grant=refresh_token)
        K-->>Ref: {access_token', refresh_token'?, expires_in}
        Ref-->>Fac: TokenResp
        Fac->>Tok: Put(Token{...更新...})
        Tok->>Tok: ON CONFLICT DO UPDATE
    else valid
        Note right of Auth: そのまま使う
    end
    Fac-->>Mw: API ready
    Mw->>Req: tool dispatch
```

---

## 9. リスク評価表

| # | カテゴリ | リスク | 重大度 | 確率 | 対策 |
|---|---------|-------|--------|------|------|
| 1 | SQLite | 旧案: 同一ファイル 2 Pool で SQLITE_BUSY 連発 | C | 中 | **2 ファイル分離に変更**（`kintone.db` + `idproxy.db`）。Phase 0 で modernc 並行挙動を再確認 |
| 2 | SigningKey | ephemeral + WARN-only でサイレント認証障害 | C | 高 | **`auth=oidc`** で fail-fast（authz は問わず）。memory backend × 本番禁止 |
| 3 | DynamoDB | ListByDomain Scan でコスト爆発 | C | 高 | GSI1 sparse を Day 1 で導入、Scan 撤廃 |
| 4 | DynamoDB | TTL eventual delete で expired item 流通 | C | 中 | 読み出し時 expires_at チェックを必須化、Phase 0 で idproxy 側の同等チェックを確認 |
| 5 | Redis | miniredis Lua 制限で `consume.lua` 不動作 | H | 中 | Phase 0 で Lua 読解、CI に actions services redis を追加 |
| 6 | Phase 6 | Big Bang 配線変更で revert 困難 | H | 高 | Wave A: store 実装のみ / Wave B: 全 caller 一括移行（1 PR）。shim 案（旧 Wave C）は廃案 |
| 7 | SQLite | 起動時 schema migration 競合 | H | 中 | `Container.Init` で kintone migration 先 → idproxy.New 後の直列化、`sync.Once` 保護 |
| 8 | principal_id | CLI と MCP で principal_id 形式混在 | H | 中 | 規約明文化（`oauth:` は CLI fallback、MCP は `<provider>:<sub>`）、regex 検証 |
| 9 | Container.Close | idproxy cleanup goroutine leak | H | 中 | `Close(ctx)` で goroutine 終了待ち、`goleak.VerifyNone` テスト |
| 10 | default backend | sqlite default で副作用 DB 作成 | H | 高 | lazy init（read-only 系は store を Open しない）|
| 11 | binary size | DynamoDB SDK で配布バイナリ 25MB → 45MB | M | 高 | 受容（ユーザー要件「全部」遵守、配布チャネル単一化のメリット）。README にサイズ明記 |
| 12 | 移行 | tokenstore.Store → store.TokenStore リネームで mock 大量変更 | M | 高 | Phase 0 で interface 形状確定、Wave B で全 caller を 1 PR 一括移行（shim は採用しない） |
| 13 | Redis Cluster | hash slot 跨ぎで Lua 不動作 | M | 低 | hash tag `{prefix}` で同 slot 強制、ドキュメントで Cluster は v0.5+ |
| 14 | テスト | integration tag が CI / レビューで形骸化 | M | 中 | `make test-integration` でワンコマンド起動、PR template でチェックボックス必須 |
| 15 | DynamoDB | IAM 権限不足で AccessDeniedException | M | 中 | 起動時 DescribeTable 疎通確認、README に最小 IAM JSON |
| 16 | SigningKey | PEM フォーマット差異でパース失敗 | M | 中 | PKCS#8 一次・EC PRIVATE KEY フォールバック、エラー時に PEM 中身を leak しない |
| 17 | Docker | WAL ファイルが volume に未チェックポイント残存 | M | 低 | コンテナ起動時 recover、README で「graceful shutdown 推奨」明記 |
| 18 | Cookie | `KINTONE_MCP_COOKIE_SECRET` rotation でセッション全切断 | L | 低 | dual-secret support の設計余地を ENV 表に残す（M13+）|
| 19 | Redis | URL 内に `@` を含むパスワードで parse 失敗 | L | 低 | `KINTONE_STORE_REDIS_PASSWORD` を別 ENV 化（実装済）|
| 20 | DynamoDB | clock skew で TTL 境界誤判定 | L | 低 | NTP 前提、margin 60s |
| 21 | DynamoDB | GSI2 不在テーブルで cache stats / clear --key prefix が失敗 | H | 中 | `kintone store init dynamodb` で事前検証、起動時 `DescribeTable` で GSI1+GSI2 不足時に `STORE_GSI_MISSING` エラー envelope |
| 22 | Cache | `KINTONE_STORE_CACHE_BYPASS=1` 設定時に CachingAPI を素通しする設計 | M | 低 | 既存 `service/api.NewCachingAPI` に bypass 引数追加、unit test で経路を確認 |
| 23 | MCP | `auth=oidc + authz=oauth` で per-principal API 解決の Factory 注入失敗 | C | 低 | Phase 6 Wave B4 で `mcpserver.NewWithDeps` シグネチャ変更 + integration test で per-principal 経路を network レベルで確認 |
| 24 | principal_id | CLI 保存 `oauth:alice` と MCP OIDC `https://accounts.google.com:1234567890` のキー不整合で MCP 経由のリクエストが Token を引けない | H | 中 | README で `<issuer>:<sub>` 形式の運用を強く推奨。M13+ で principal alias テーブル導入余地。Phase 6 で regex 検証 + 不一致時の `RESOLVER_PRINCIPAL_NOT_FOUND` エラー envelope |
| 25 | Lazy init | read-only コマンド (`version`/`completion`) で Container を Open する経路が混入する | M | 中 | root の `needsStore()` が **read-only コマンドでは Container そのものを Open しない**（accessor 制限ではなく Open 自体をスキップ）。テストで version / completion / config show / store init を実行して DB ファイル作成ゼロを確認 |

C=Critical / H=High / M=Medium / L=Low

---

## 10. 品質レビュー 5 観点 27 項目チェックリスト

### 観点 A: 実装実現可能性と完全性（5項目）
- [ ] A1. 11 フェーズ（Phase 0 / 1 / 2 / 3 / 4 / 5 / 6 / 7 / 8 / 8.5 / 9）の手順に抜け漏れがなく end-to-end で完結している
- [ ] A2. 各フェーズが具体的（実装者が迷わない詳細度）で TDD 手順が記載されている
- [ ] A3. 依存関係が明示（Phase 0 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9）
- [ ] A4. 変更対象ファイルが網羅されている（パッケージ構成・Wave 別 PR で全リスト化）
- [ ] A5. 影響範囲（22 パッケージ中 7-8 パッケージへの import 書き換え）を Wave 分割で受け止めている

### 観点 B: TDD テスト設計の品質（6項目）
- [ ] B1. 正常系テストケースが Conformance test で網羅（4 backend × 17 メソッド）
- [ ] B2. 異常系テストケース（ErrCacheMiss / ErrUnsupportedBackend / SIGNING_KEY_REQUIRED）が定義されている
- [ ] B3. エッジケース（TTL 経過後・並行 100 goroutine・fail-fast 経路・goroutine leak）を考慮
- [ ] B4. 入出力が具体的（PEM パース、JSON envelope、PK 文字列形式）
- [ ] B5. Red→Green→Refactor が各 Phase で守られている
- [ ] B6. miniredis / dynamodb-local の差異を見越した integration test を分離（`-tags=integration` + actions services / testcontainer）

### 観点 C: アーキテクチャ整合性（5項目）
- [ ] C1. 既存の命名規則に従う（`internal/store/`、`KINTONE_STORE_*` ENV、Conventional Commits 日本語）
- [ ] C2. 設計パターンが一貫（Container パターン、factory パターン、interface 駆動）
- [ ] C3. モジュール分割が適切（backend 実装は subdir に閉じる、conformance test は storetest に集約）
- [ ] C4. 依存方向が正しい（cli → store, idproxy → store、循環なし）
- [ ] C5. 類似機能（旧 tokenstore / cache）と統一（同 interface 形状で全 caller を Wave B 一括移行、shim は採用しない）

### 観点 D: リスク評価と対策（6項目）
- [ ] D1. リスク評価表で Critical / High / Medium / Low が網羅
- [ ] D2. 対策が具体的（GSI1 設計、Wave 別 PR、fail-fast、lazy init 等）
- [ ] D3. フェイルセーフ（lazy init、`Container.Close` 直列、goleak テスト）
- [ ] D4. パフォーマンス影響評価（バイナリサイズ、Scan vs Query、Pool 並行性）
- [ ] D5. セキュリティ観点（PEM leak 防止、0o600 ファイル、KMS 推奨明記）
- [ ] D6. ロールバック計画（Wave A は単独 revert 可能、Wave B は単一 PR の revert で旧コードに完全復帰、Phase 7 は逆方向 commit で復元）

### 観点 E: シーケンス図の完全性（5項目）
- [ ] E1. 正常フロー（CLI login、MCP serve、refresh）を Mermaid で記述
- [ ] E2. エラーフロー（fail-fast、SIGNING_KEY_REQUIRED）が SigningKey フローに含まれる
- [ ] E3. ユーザー / kintone CLI / idproxy / kintone OAuth Server の相互作用が明確
- [ ] E4. タイミング・同期処理（lazy init、Container.Close 順序）が明記
- [ ] E5. リトライ・タイムアウト（busy_timeout、refresh skew 60s、cleanup goroutine 5min）が含まれる

合計: **27 項目**（**Leader 校正: FP 推定 0 件、重み付け妥当性: OK**）

---

## 11. ドキュメント更新計画

### `README.md` / `README.ja.md`

- 「環境変数」表から `KINTONE_TOKENS_PATH` / `KINTONE_CACHE_PATH` / `KINTONE_CACHE_DISABLE` を削除（後者は `KINTONE_STORE_CACHE_BYPASS` への移行案内を必須記載）
- 新規セクション「Storage Backend」（4 backend 表）
- 4 backend 別の `docker run` / `kubectl set env` / Lambda env サンプル
- DynamoDB 最小 IAM ポリシー（JSON、`Query on Index/gsi1` `Query on Index/gsi2` `BatchWriteItem` 含む）+ 配布バイナリサイズの注記（~45MB、全 backend 同梱）
- **Redis ACL 最小権限サンプル**（`+@all` 不可、`-@admin -@dangerous` 必須）:
  ```
  ACL SETUSER kintone on >password \
      ~kintone:* ~idproxy:* \
      +@read +@write +@string +@hash +@scripting -@admin -@dangerous
  ```
- SigningKey の運用ガイド（KMS で復号 → env 注入 / または Storage 永続化、**`auth=oidc` は fail-fast**）
- MCP secret 2 種（CookieSecret / SigningKey）の役割と管理方針
- **principal_id 規約**: CLI と MCP の token 共有時は `<issuer>:<sub>` 形式で `--principal-id` を揃える運用ルール
- **Cache Stats 出力スキーマ**（backend 非依存、新フォーマット）の説明
- **Threat Model**: at-rest 暗号化はインフラ層委譲、envelope encryption は M13+ の旨明記

### `docs/specs/kintone_spec.md`

データストア節を以下構造に書き換え：

```
## データストア
kintone CLI/MCP は認証情報・キャッシュ・OIDC 状態を単一の Storage に保管する。

### バックエンド種別
（4 backend 表）

### キー名前空間
- kintone 側: `kintone:tokens:` / `kintone:cache:` / `kintone:signingkey:`
- idproxy 側: upstream 既定 prefix（`session:` / `authcode:` / `accesstoken:` / `refreshtoken:` / `client:` / `familyrevoked:`）
- 衝突防止規則: `kintone:` で始まらない PK/key を kintone 自前ストアが書くことを禁止（lint test で保証）

### SQLite の配置
- 同一ディレクトリに 2 ファイル（`kintone.db` + `idproxy.db`）
- `idproxy.New(path)` の API 制約により 1 ファイル 2 Pool は採用しない

### DynamoDB の GSI1
- ListByDomain は GSI1 sparse Query（kintone token のみに gsi1pk/gsi1sk 付与）
- idproxy アイテムからのコスト隔離

### SigningKey
- env > Storage > ephemeral(warn) の優先順序
- **`auth=oidc`** で SigningKey 未解決時は fail-fast（authz の値に依らない）
```

### `plans/kintone-roadmap.md`

- Meta `最終更新` を本日付に
- `ステータス` を `M12 進行中` に
- Progress に `### M12: 統合 Storage バックエンド` セクション追加（[ ] チェックリスト 10 項目 = Phase 0-9）
- Architecture Decisions 追加（3 行）:
  - `7 | 同一 backend で kintone と idproxy を論理分離（テーブル名/キー prefix）| 工数最小・upstream 追従容易 | 2026-04-30`
  - `8 | SQLite は 2 ファイル分離（idproxy New(path) 制約）| 2 Pool 並行は modernc で未踏領域 | 2026-04-30`
  - `9 | DynamoDB は GSI1 sparse で kintone token のみ index | idproxy アイテムとのコスト isolation | 2026-04-30`
- Changelog エントリ追加

### `CHANGELOG.md`

```markdown
## [Unreleased]

### BREAKING CHANGES — M12 Unified Storage
- 削除した環境変数: `KINTONE_TOKENS_PATH`, `KINTONE_CACHE_PATH`, `KINTONE_CACHE_DISABLE`
- 追加した環境変数: `KINTONE_STORE_BACKEND`, `KINTONE_STORE_SQLITE_DIR`,
  `KINTONE_STORE_REDIS_URL`, `KINTONE_STORE_REDIS_TLS`, `KINTONE_STORE_REDIS_PASSWORD`,
  `KINTONE_STORE_DYNAMODB_TABLE`, `KINTONE_STORE_DYNAMODB_REGION`,
  `KINTONE_STORE_CACHE_BYPASS`,
  `KINTONE_MCP_SIGNING_KEY_PEM`
- ファイル配置の変更: 既定の SQLite ファイルが `~/.cache/kintone/{tokens,cache}.db` から
  `~/.local/state/kintone/{kintone,idproxy}.db` に変更
- internal API: `tokenstore` / `cache` パッケージは `store` に統合・削除
- 配布: 全 backend（Memory + SQLite + Redis + DynamoDB）を標準ビルドに同梱（バイナリサイズ ~25MB → ~45MB）
- 動作変更: **`auth=oidc`** かつ SigningKey が解決できない場合 startup を拒否（authz の値に依らない）
- 動作変更: `KINTONE_STORE_BACKEND=memory` × `--auth oidc` の組合せは env で SigningKey を与えても全面禁止（startup 拒否、`STORE_MEMORY_OIDC_FORBIDDEN`）
- CLI 契約: `cache clear --scope <apps|fields|all>` は維持（既存ユーザー互換）+ `--key <prefix>` を新規追加（高度デバッグ用、後方互換）
- CLI 契約変更: `cache stats` の JSON schema を変更（旧 `db_path` / `db_exists` / `size_bytes` → 新 `backend` / `location` / `reachable` / `entry_count` / `expired_count` / `backend_specific`）
- CLI 契約変更: `config show` の `cache_path` フィールドを廃止し、`storage: {backend, location, source}` フィールドを新設
- 新エラー code: `STORE_TABLE_NOT_FOUND` / `STORE_GSI_MISSING` / `STORE_TTL_DISABLED` / `STORE_BACKEND_UNAVAILABLE` / `STORE_CONNECTION_FAILED` / `STORE_MEMORY_OIDC_FORBIDDEN` / `STORE_CACHE_BYPASS_INVALID` / `SIGNING_KEY_REQUIRED` / `RESOLVER_PRINCIPAL_NOT_FOUND`
```

---

## 12. 検証手順

### テスト階層（CI 自動化レイヤ、Copilot Medium #12 反映）

| 階層 | コマンド | 含むもの | CI 必須 | ローカル所要 |
|------|---------|---------|---------|-------------|
| unit | `go test -race -cover ./...` | memory + sqlite backend、既存 22+α パッケージ全テスト、miniredis ベースの redis unit | YES（PR 必須）| 30 秒 |
| integration | `go test -race -tags=integration ./internal/store/...` | actions services redis + dynamodb-local + 全 backend conformance + idproxy upstream Lua 実機検証 | YES（PR 必須）| 2-3 分 |
| e2e | `make test-e2e` | OIDC stub + kintone-fake + **3 backend (sqlite/redis/dynamodb) × seed seam フロー**（seed → records_query → refresh trigger → records_query 再試行 → seed 確認）+ MCP HTTP `--auth oidc --authz oauth` per-principal 解決 | YES（main 必須・PR 任意）| 5 分 |
| manual | `docs/manual-tests/M12.md` のチェックリスト | 実機 kintone OAuth + 実 Redis Cluster + 実 AWS DynamoDB | NO（リリース時のみ）| 30-60 分 |

**CI 必須フロー**: PR 作成時に unit + integration を必ず通す。main マージ時に e2e を追加実行。失敗したら revert または fix forward。

**OIDC stub の実装**:
- `tools/oidcstub/` 配下に最小 OIDC プロバイダを実装（既知の RSA 鍵で署名 / discovery / jwks endpoint / authorize / token / userinfo を提供）
- `KINTONE_MCP_OIDC_ISSUER=http://oidcstub:9090` で kintone を起動できる
- これにより本物の Google / Azure に依存せず full flow を CI で再現

**CI matrix（GitHub Actions）**:

```yaml
jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - run: go test -race -cover ./...
  integration:
    runs-on: ubuntu-latest
    services:
      redis:
        image: redis:7-alpine
        ports: ['6379:6379']
      dynamodb:
        image: amazon/dynamodb-local
        ports: ['8000:8000']
    env:
      KINTONE_STORE_REDIS_URL: redis://localhost:6379/0
      AWS_ENDPOINT_URL: http://localhost:8000
      AWS_ACCESS_KEY_ID: dummy
      AWS_SECRET_ACCESS_KEY: dummy
    steps:
      - run: go test -race -tags=integration ./internal/store/...
  e2e:
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    needs: [unit, integration]
    runs-on: ubuntu-latest
    services:
      redis:
        image: redis:7-alpine
        ports: ['6379:6379']
      dynamodb:
        image: amazon/dynamodb-local
        ports: ['8000:8000']
      oidcstub:
        image: ghcr.io/youyo/kintone/oidcstub:latest
        ports: ['9090:9090']
    steps:
      - run: make test-e2e
```

### 単体・パッケージレベル

```bash
# 標準実行（memory + sqlite + 既存 22 パッケージ）
go test -race -cover ./...

# integration（redis + dynamodb backend）— actions services / docker
go test -race -tags=integration ./internal/store/...

# e2e（OIDC stub + kintone-fake + 3 backend × seed seam フロー）
make test-e2e

# lint
golangci-lint run ./...

# vet + fmt
go vet ./...
gofmt -l . | grep -v vendor

# goroutine leak 検出
go test -race -tags=integration -run TestContainer ./internal/store/...
```

### エンドツーエンド動作確認

#### Backend = memory（CI default）

```bash
export KINTONE_STORE_BACKEND=memory
export KINTONE_DOMAIN=example.cybozu.com
export KINTONE_OAUTH_CLIENT_ID=...
export KINTONE_OAUTH_CLIENT_SECRET=...
export KINTONE_OAUTH_REDIRECT_URL=http://127.0.0.1:0/callback

go run ./cmd/kintone auth login --oauth --principal-id alice
go run ./cmd/kintone auth status --principal-id alice
go run ./cmd/kintone auth logout --principal-id alice

# auth=oidc + memory backend で fail-fast 確認（authz の値に依らず SigningKey 必須）
go run ./cmd/kintone mcp serve --listen :8080 --auth oidc --authz api-token
# → 期待: exit code 1, **stdout** に既存 CLI 契約通り JSON envelope `{"ok":false,"error":{"code":"STORE_MEMORY_OIDC_FORBIDDEN",...}}`
```

#### Backend = sqlite

```bash
export KINTONE_STORE_BACKEND=sqlite
export KINTONE_STORE_SQLITE_DIR=$HOME/.local/state/kintone

go run ./cmd/kintone auth login --oauth --principal-id alice
ls -la $KINTONE_STORE_SQLITE_DIR
# → kintone.db (kintone 用)、idproxy.db (mcp serve 後に作成される)

sqlite3 $KINTONE_STORE_SQLITE_DIR/kintone.db '.tables'
# → kintone_oauth_tokens kintone_kv_cache kintone_signing_keys
sqlite3 $KINTONE_STORE_SQLITE_DIR/kintone.db 'SELECT principal_id FROM kintone_oauth_tokens;'

# MCP HTTP + OIDC + 同一ディレクトリの 2 ファイル
export KINTONE_MCP_OIDC_ISSUER=https://accounts.google.com
export KINTONE_MCP_OIDC_CLIENT_ID=...
export KINTONE_MCP_EXTERNAL_URL=http://localhost:8080
export KINTONE_MCP_COOKIE_SECRET=$(openssl rand -hex 32)

go run ./cmd/kintone mcp serve --listen :8080 --auth oidc --authz oauth &
sqlite3 $KINTONE_STORE_SQLITE_DIR/idproxy.db 'SELECT id FROM sessions LIMIT 1;'
# → idproxy が idproxy.db に session を書いていることを確認
sqlite3 $KINTONE_STORE_SQLITE_DIR/kintone.db 'SELECT id FROM kintone_signing_keys;'
# → 'current' 1 行（永続化された SigningKey）
```

#### Backend = redis

```bash
docker run -d --name kintone-redis -p 6379:6379 redis:7-alpine
export KINTONE_STORE_BACKEND=redis
export KINTONE_STORE_REDIS_URL=redis://localhost:6379/0

go run ./cmd/kintone auth login --oauth --principal-id alice
redis-cli KEYS 'kintone:*'
# → kintone:tokens:example.cybozu.com:google:alice:oauth, kintone:signingkey:current

go run ./cmd/kintone mcp serve --listen :8080 --auth oidc --authz oauth &
redis-cli KEYS 'idproxy:*'
# → idproxy:session:..., idproxy:client:... など

# refresh CAS の動作確認
hey -n 100 -c 10 -H "Authorization: Bearer $JWT" http://localhost:8080/mcp/...
redis-cli LRANGE consume.lua.log 0 -1   # 100 並行で 1 回だけ rotate されたか
```

#### Backend = dynamodb（dynamodb-local）

```bash
docker run -d --name kintone-ddb -p 8000:8000 amazon/dynamodb-local
aws --endpoint-url http://localhost:8000 dynamodb create-table \
  --table-name kintone-test \
  --attribute-definitions \
    AttributeName=pk,AttributeType=S \
    AttributeName=gsi1pk,AttributeType=S \
    AttributeName=gsi1sk,AttributeType=S \
    AttributeName=gsi2pk,AttributeType=S \
    AttributeName=gsi2sk,AttributeType=S \
  --key-schema AttributeName=pk,KeyType=HASH \
  --global-secondary-indexes '[
    {"IndexName":"gsi1","KeySchema":[{"AttributeName":"gsi1pk","KeyType":"HASH"},{"AttributeName":"gsi1sk","KeyType":"RANGE"}],"Projection":{"ProjectionType":"ALL"}},
    {"IndexName":"gsi2","KeySchema":[{"AttributeName":"gsi2pk","KeyType":"HASH"},{"AttributeName":"gsi2sk","KeyType":"RANGE"}],"Projection":{"ProjectionType":"KEYS_ONLY"}}
  ]' \
  --billing-mode PAY_PER_REQUEST
aws --endpoint-url http://localhost:8000 dynamodb update-time-to-live \
  --table-name kintone-test --time-to-live-specification 'Enabled=true,AttributeName=ttl'

export KINTONE_STORE_BACKEND=dynamodb
export KINTONE_STORE_DYNAMODB_TABLE=kintone-test
export KINTONE_STORE_DYNAMODB_REGION=us-east-1
export AWS_ENDPOINT_URL=http://localhost:8000
export AWS_ACCESS_KEY_ID=dummy
export AWS_SECRET_ACCESS_KEY=dummy

go run ./cmd/kintone store init dynamodb --table kintone-test --region us-east-1
# → 期待: ok（GSI1 含む）

go run ./cmd/kintone auth login --oauth --principal-id alice
aws --endpoint-url http://localhost:8000 dynamodb scan --table-name kintone-test \
  --query 'Items[].pk.S' --output text
# → kintone:tokens:..., kintone:signingkey:current
aws --endpoint-url http://localhost:8000 dynamodb query --table-name kintone-test \
  --index-name gsi1 \
  --key-condition-expression 'gsi1pk = :d' \
  --expression-attribute-values '{":d":{"S":"example.cybozu.com"}}' \
  --query 'Items[].gsi1sk.S'
# → ListByDomain 相当（Scan 不要）
```

---

## 13. Critical Files for Implementation

実装上最も重要な 5 ファイル（絶対パス）:

- `/Users/youyo/src/github.com/youyo/kintone/internal/idproxy/config.go` — `BuildAuth` のシグネチャと `Store` 注入点（Phase 3, 6）
- `/Users/youyo/src/github.com/youyo/kintone/internal/service/api/principal.go` — `PrincipalAPIFactory` が `tokenstore.Store` を参照する箇所（Phase 6）
- `/Users/youyo/src/github.com/youyo/kintone/internal/cli/auth/helpers.go` — `defaultOpenTokenStore` のリファレンス実装（Phase 6 Wave B1）
- `/Users/youyo/src/github.com/youyo/kintone/internal/cli/mcp/serve.go` + `idproxy_glue.go` — Container ライフサイクルと middleware 構築（Phase 6 Wave B4）
- `/Users/youyo/src/github.com/youyo/kintone/go.mod` — redis/dynamodb 直接依存への昇格（Phase 4, 5）

加えて Phase 0 で参照必須:

- idproxy v0.4.2 の `store/sqlite/store.go`（`New(path)` のみで `NewWithDB` 不在の確認）
- idproxy v0.4.2 の `store/redis/consume.lua`（miniredis 互換性確認）
- idproxy v0.4.2 の `store/dynamodb.go`（読み出し時 expires_at チェックの有無）
- modernc.org/sqlite v1.50.0 の DSN 構文（`_pragma=...` 形式）

---

## 14. 主要設計判断（最終確定一覧）

15 巡のレビューサイクルを経た最終決定 30 項目。実装はこの表を **唯一の正本** として参照する。撤回された途中案は本表から除外済み。

| # | カテゴリ | 確定内容 |
|---|---------|---------|
| 1 | 統合スコープ | 同一バックエンド・別論理（テーブル / キー prefix で kintone と idproxy を分離） |
| 2 | バックエンド | Memory + SQLite + Redis + DynamoDB の 4 種を **1 binary に同梱**（build tag 不採用、バイナリ ~45MB を許容） |
| 3 | SQLite 配置 | **2 ファイル分離**（`<dir>/kintone.db` + `<dir>/idproxy.db`）、modernc.org/sqlite、WAL + busy_timeout=5000 |
| 4 | DynamoDB スキーマ | 単一テーブル、属性名 lowercase（`pk` / `data` / `ttl` / `gsi1pk` / `gsi1sk` / `gsi2pk` / `gsi2sk`）、**GSI1（token sparse）+ GSI2（cache sparse）**、`expired_count` は **常に `null` 固定**、Query は **全ページ走査必須**（LastEvaluatedKey）、BatchWriteItem の UnprocessedItems 再送 |
| 5 | TTL 命名 | アプリ層真実は `expires_at` (epoch nanoseconds)。DynamoDB のみ `ttl` (epoch seconds) を併記して Auto-TTL を有効化 |
| 6 | Cache key prefix | 既存実装の `v1:app:` / `v1:fields:` / `v1:list_apps:` 形式を完全継承。新パッケージ `internal/store/cache.go` に同名・同値で移設 |
| 7 | Cache CLI 契約 | `--scope <apps\|fields\|list_apps\|all>` は維持（後方互換）+ `--key <prefix>` を高度デバッグ用に追加（内部で `kintone:cache:` namespace を補完）。`cache stats` は backend 中立 schema（`backend` / `location` / `reachable` / `entry_count` / `expired_count` / `backend_specific`） |
| 8 | Cache 経路別アクセサ | `CacheForDecorator()` (fail-open: 接続失敗時 `nil, nil`) / `CacheForAdmin()` (fail-fast: 接続失敗時 `nil, err`) の 2 アクセサ。`cache stats` / `cache clear` は後者、API decorator 経路は前者。**`store init` は Container を経由しない**（chicken-and-egg 回避） |
| 9 | CachingAPI per-request | 起動時 1 回構築 → per-request の `cacheProvider()` lazy resolution に変更。長寿命 MCP プロセスでも cache 接続を再評価して自動回復 |
| 10 | TokenStore.ListByDomain | 既存 `(ctx, domain string, authType AuthType) ([]*Token, error)` シグネチャをそのまま正式 interface 昇格 |
| 11 | SigningKey 判定軸 | **`auth=oidc`** 軸で fail-fast（`authz` の値に依らない）。解決順序: Step 0 `validateBackendForAuthMode` で禁止組合せ先取り → env (`KINTONE_MCP_SIGNING_KEY_PEM`) → Storage → fail-fast |
| 12 | Memory + auth=oidc | env で SigningKey を与えても**全面禁止**（session 等の memory state が scale-out 不可）。`STORE_MEMORY_OIDC_FORBIDDEN` で startup 拒否 |
| 13 | SigningKey 外部供給 | `auth=oidc` では `KINTONE_MCP_SIGNING_KEY_PEM` または `KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1` のいずれかを**必須**。暗黙の Storage 自動生成は禁止 |
| 14 | MCP per-principal | 既存 `facade.ToolDeps.Factory` / `PrincipalAPIFactory` / `AUTH_REQUIRED` mapping を活用。`mcpserver.NewWithDeps(deps facade.ToolDeps)` を新設し、`cli/mcp/serve.go::runHTTP` で `authz=oauth` 時に Factory を構築・注入。`internal/mcp/server` は transport 専用層を維持 |
| 15 | principal_id 規約 | CLI と MCP の token 共有時は `<issuer>:<sub>` 形式で `--principal-id` を揃える。alias / link は M13+ |
| 16 | MCP secret | `KINTONE_MCP_COOKIE_SECRET` / `KINTONE_MCP_SIGNING_KEY_PEM` は Storage 体系から独立（K8s Secret として個別管理）|
| 17 | Storage 設定範囲 | `KINTONE_STORE_*` は env-only。正本は `internal/store/env.go` のみ。`internal/config/{env,resolver}.go` には storage 関連を一切追加しない |
| 18 | config show 表示 | `internal/cli/config.go` 専用アダプタで storage view を合成（`internal/config` に store 結合点を作らない）。`cache_path` フィールドを廃止し `storage: {backend, location, source}` を新設 |
| 19 | Container ライフサイクル | root の `PersistentPreRunE` で Open + ctx 注入。**Close は `internal/cli/root.go::ExecuteWith` の defer に集中**（既存 error→JSON envelope 責務に追加、`sync.Once` で冪等化）。RunE 個別 defer 案・`PersistentPostRunE` 案は不採用 |
| 20 | needsStore 単一判定 | `needsStore(cmd, resolved, env) bool` 関数 1 箇所で判定。read-only コマンド (`version`/`completion`/`config show`/`store init`) は false（DB 副作用ゼロ）。`auth=api-token + cache_bypass=1` は完全 stateless ファストパス |
| 21 | Container interface | `Tokens() (TokenStore, error)` / `CacheForDecorator() (CacheStore, error)` / `CacheForAdmin() (CacheStore, error)` / `SigningKey() (SigningKeyStore, error)` / `IDProxyStore() (idpstore.Store, error)`（upstream `github.com/youyo/idproxy/store.Store` を直接返却）/ `LocationString() string` / `Close(ctx context.Context) error`。Lazy init は mandatory が fatal、optional は再試行（1 秒 cooldown） |
| 22 | hook 優先順位 | context > test hook > direct open。本番は context 注入、test は hook（`SetOpenStoreFn` / `SetNewContainerBuilder` 等）、lib 経由は direct |
| 23 | rootCommand state | package var ではなく root command struct field に格納（test 並列汚染防止） |
| 24 | Wave 構成 | Wave A: store パッケージ実装のみ / Wave B: 全 caller を 1 PR (~700-1000 行) で一括移行 / Phase 7: 旧 `internal/tokenstore` `internal/cache` を完全削除。shim は採用しない |
| 25 | Phase 0 = 設計ゲート | Phase 1 着手前に exit criteria 全 PASS 必須。idproxy v0.4.2 の DynamoDB / Redis 構築 API 確認、modernc 2 Pool 並行挙動、Lua の miniredis 互換性、interface 形状確定。最大 +6.5 日のバッファあり |
| 26 | Phase 8.5 E2E | **in-process Go test 一本化**（`net/http/httptest.NewServer`）。`internal/testsupport/oidcstub/` + `internal/testsupport/kintonefake/` 配置、外部バイナリ・Docker 化なし。E2E 対象は **3 backend (sqlite/redis/dynamodb)** × seed seam フロー（memory 除外）。OAuth login 対話フローは `internal/store/storetest/seed.go::SeedTokenForE2E` で省略 |
| 27 | エラー正本 | `internal/store/errors.go` の sentinel error が **唯一の正本**（10 種: `STORE_TABLE_NOT_FOUND` / `STORE_GSI_MISSING` / `STORE_TTL_DISABLED` / `STORE_BACKEND_UNAVAILABLE` / `STORE_CONNECTION_FAILED` / `STORE_MEMORY_OIDC_FORBIDDEN` / `STORE_CACHE_BYPASS_INVALID` / `STORE_PLAINTEXT_FORBIDDEN` / `SIGNING_KEY_REQUIRED` / `RESOLVER_PRINCIPAL_NOT_FOUND`）。CLI / facade は sentinel→code mapping のみ。出力先は **stdout JSON envelope**（既存 CLI 契約） |
| 28 | エラーサニタイズ | `STORE_CONNECTION_FAILED.details` は `cause_class` (network/auth/timeout/unknown) のみ、生 `err.Error()` は外部出力しない（stderr slog にのみ詳細）。`internal/output/sanitize.go::SanitizeURL` で Redis URL の userinfo / クエリ password をマスク。SigningKey PEM は完全非露出 |
| 29 | Redis セキュリティ | `rediss://` は常に TLS（downgrade 不可）、`KINTONE_STORE_REDIS_TLS=1` は `redis://` の TLS upgrade のみ。**非 localhost への `redis://` は `KINTONE_STORE_REDIS_INSECURE_PLAINTEXT=1` がない限り startup 拒否**（`STORE_PLAINTEXT_FORBIDDEN`）。Redis ACL は最小権限（`+@read +@write +@string +@hash +@scripting -@admin -@dangerous`、`+@all` 禁止） |
| 30 | Logging 基盤 | `internal/output/logger.go` を Phase 1 で新設。`log/slog` を stderr 出力で初期化、`KINTONE_LOG_LEVEL=debug\|info\|warn\|error`（既定: warn）で制御。stdout は JSON envelope のため触らない |

これら以外の細部は Annotation Cycle で本ファイルにインライン注釈で随時調整可能。

---

## 15. Annotation Cycle の使い方

このプランファイル（`plans/binary-imagining-lemur.md`）に **直接インライン**で注釈を書き込んでください。例:

```markdown
### Phase 5 — DynamoDB backend

<!-- 注釈: build tag 分離は不要、全 backend 1 binary で OK -->
**変更ファイル一覧**:
...
```

その後 `/devflow:plan` を再実行すると、注釈を反映して計画のみ更新されます（実装はしません）。納得いくまで繰り返し、最終的に `/devflow:implement` または `/devflow:cycle` で実装を開始します。

---

## まとめ

本計画は kintone CLI/MCP の v0.1.0 リリース直前という極めてシンプルなタイミングを活かし、**3 系統に分散していた永続化層を 1 つの `internal/store/` パッケージに統合**する。バックエンドは Memory/SQLite/Redis/DynamoDB の 4 種を Day 1 から全て標準ビルドに同梱（バイナリ ~45MB を許容）し、Lambda/Fargate/k8s への展開を解禁する。idproxy v0.4.2 の公式 `Store` 実装をそのまま使うことで保守コストを最小化（Option C 修正版）、kintone 側は prefix 戦略で論理分離する。

v0.1.0 リリース直前という極めてシンプルなタイミングを活かし、3 系統に分散していた永続化層を 1 つの `internal/store/` パッケージに統合する。Memory + SQLite + Redis + DynamoDB の 4 backend を 1 binary で提供し、Lambda / Fargate / k8s への展開を解禁する。詳細な設計判断は **Section 14（最終確定一覧 30 項目）** を参照。

主要ハイライト:
- **SQLite**: 2 ファイル分離（`kintone.db` + `idproxy.db`）、modernc.org/sqlite + WAL
- **DynamoDB**: 単一テーブル + GSI1（token sparse）+ GSI2（cache sparse）、属性名 lowercase、Scan 撤廃 + 全ページ Query
- **Redis**: 単一 client 共有、`rediss://` 強制 TLS、非 localhost 平文拒否、最小権限 ACL
- **SigningKey**: `auth=oidc` 軸で fail-fast、外部供給必須（`KINTONE_MCP_SIGNING_KEY_PEM` または `_AUTO_GENERATE=1` opt-in）
- **MCP per-principal**: 既存 `facade.ToolDeps.Factory` を活用、`mcpserver.NewWithDeps` 新設、transport 層は変更なし
- **Container ライフサイクル**: `ExecuteWith` の defer に Close 集中、root の `PersistentPreRunE` で Open
- **CachingAPI**: per-request lazy resolution で長寿命プロセスの自動回復
- **エラー正本**: `internal/store/errors.go` の sentinel 10 種、CLI/facade は mapping のみ
- **CI**: in-process Go test 一本化（外部バイナリなし）、unit/integration/e2e 3 階層

Phase 0 を**設計ゲート**として新設（最大 +6.5 日バッファ）、Phase 6 は Wave A（store 実装）+ Wave B（全 caller 一括移行）+ Phase 7（旧 `internal/tokenstore` `internal/cache` 削除）。総工数 **16.5 日**。
