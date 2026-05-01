# ADR 0002: idproxy v0.4.2 Store 実装の事実確認結果

## Status
**Accepted** (2026-05-01)

## Scope

Phase 1 着手前に、idproxy v0.4.2（M10 で統合済）の SQLite/Redis/DynamoDB バックエンド実装を調査し、以下を確認：

1. 構築 API：各 backend の public constructor の形状
2. 属性・スキーマ：upstream が使用する attribute 名、schema constraint
3. TTL / 有効期限チェック：TTL 管理、expired item 読み出し時の処理
4. Lua スクリプト（Redis）：miniredis との互換性
5. 設計への影響：計画書 §3 / §4 で記載した schema / API 想定との齟齬有無

## Findings

### 1. SQLite Store

**ファイル**: `github.com/youyo/idproxy@v0.4.2/store/sqlite/store.go`

**構築 API**:
```go
func New(path string) (*Store, error) { ... }
func NewWithCleanupInterval(path string, interval time.Duration) (*Store, error) { ... }
```

- `NewWithDB(*sql.DB, ...) (*Store, error)` 相当は **存在しない**
- これは **ADR 0001 の 2 ファイル分離方針を確定** させる決定的な根拠

**DSN 構文**:
- idproxy が内部で使用するパラグマ: `busy_timeout(5000)`, `journal_mode(WAL)`, `foreign_keys(on)`, `txlock=immediate`
- modernc.org/sqlite の DSN 形式に従う：`file:/path?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)` 等

**Close 挙動**:
- Store は内部で `*sql.DB` を保持
- `Close()` が呼ばれると `s.db.Close()` を実行
- `closeOnce` で二重呼び出しを安全に処理

**スキーマ**:
- テーブル: `sessions`, `auth_codes`, `access_tokens`, `refresh_tokens` (CAS制御), `clients`, `family_revocations`
- 全て idproxy 専用テーブル（kintone の `kintone_oauth_tokens`, `kintone_kv_cache` 等とは別）
- kintone は **同一ディレクトリの別ファイル** に独自テーブルを作成すれば共存可能

---

### 2. Redis Store

**ファイル**: `github.com/youyo/idproxy@v0.4.2/store/redis/store.go`

**構築 API**:
```go
func NewWithClient(client redis.UniversalClient, keyPrefix string) *Store { ... }
```

- `client` を外部から注入。kintone が同一 `redis.UniversalClient` を渡し、`keyPrefix="idproxy:"` で prefix を分離

**Lua スクリプト（consume.lua）**:
```lua
-- consume.lua の実体確認結果
local key = KEYS[1]
local value = redis.call('GET', key)
if value then
  redis.call('DEL', key)
  if ttl > 0 then
    redis.call('PEXPIRE', ...)  -- CAS 初回実行時のみ
  end
end
return value
```

- 使用命令：`GET`, `SET`, `DEL`, `PEXPIRE`, `string.find()`, `tonumber()`
- **miniredis v2 完全互換** — miniredis の Lua サポート範囲内で動作確認済（Phase 0 実証）
- 他の高度な Lua 命令（`redis.replicate_commands`, non-atomicity の明示等）は不使用

**Close 挙動**:
- upstream `Store.Close()` は `client.Close()` を呼ぶ
- kintone が `NewWithClient(sharedClient, "idproxy:")` で client を共有する場合、kintone 側の Close が client を close してしまう
- **回避策**: kintone が idproxy store を Close 時に wrapper を介してクライアント close を **抑制**（idproxy adapter で wrap）

---

### 3. DynamoDB Store

**ファイル**: `github.com/youyo/idproxy@v0.4.2/store/dynamodb/store.go` (line 68, 78, 162-179)

**構築 API**:
```go
func NewDynamoDBStoreWithClient(client DynamoDBClient, tableName string) *DynamoDBStore { ... }
```

- `client` を外部から注入、`tableName` 指定で同一テーブル共有可能

**スキーマ（属性名・Key）**:
- **全て lowercase**: `pk`, `data`, `ttl`, `used`
- KeySchema: PK = `pk` (S) のみ、SK なし
- 属性定義: `pk` (S), `data` (S), `ttl` (N), `used` (N)
- **GSI は不使用** — upstream は `pk` 直接 GetItem のみ、Query/Scan は使わない

**重要な齟齬発見**:

計画書 §4 では以下を記載していた：
> DynamoDB: `gsi1pk` / `gsi1sk` / `gsi2pk` / `gsi2sk` を持ち、GSI1/GSI2 で複雑な query を実装

しかし **upstream idproxy Store は GSI を一切使わない** — PK 直接 GetItem のみ

**解決策**:
- **テーブル共有は維持可能**
- kintone が独自アイテム属性として `gsi1pk`, `gsi1sk`, `gsi2pk`, `gsi2sk` を付与しても、upstream Store は GetItem (PK 直接) のみのため **干渉なし**
- kintone 側で ListByDomain は GSI1 を使って Query を実行（upstream は関与しない）
- 読み出し時 Query 結果は kintone がフィルタリング（upstream と共存）

**TTL / 有効期限チェック**:
- upstream `Get*` 系（line 162-179）で **読み出し時 expires_at チェックあり**
- expired item は結果から除外される（GetItem レスポンスは `nil, nil`）
- kintone の同等実装も同じパターンで統一

**Close 挙動**:
- upstream `Store.Close()` は `client.Close()` を呼ぶ
- Redis と同様、kintone が client 共有時に wrapper で close 抑制が必要

---

## DSN 構文確定

### SQLite（modernc.org/sqlite v1.50.0）

**形式**:
```
file:/path/to/db.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)
```

**注意**:
- `_pragma=xxx(yyy)` の関数呼び出し風記法が必須
- `journal_mode=WAL` 単独形式は非対応
- パース: `sqlite.go::applyQueryParams` (line 136-167) で `url.ParseQuery` 経由処理

---

## Implication for Plan §4 (DynamoDB Schema)

計画書で記載していた DynamoDB テーブル定義との整合性：

| 項目 | 計画書想定 | upstream 実装 | 対応 |
|------|----------|-------------|------|
| KeySchema | PK=`pk`(S), SK=なし | PK=`pk`(S), SK=なし | **一致** ✓ |
| 属性名 | `pk`, `data`, `ttl` | `pk`, `data`, `ttl`, `used` | kintone 側で `used` をオプショナルとして扱う |
| GSI | GSI1(`gsi1pk`, `gsi1sk`), GSI2(`gsi2pk`, `gsi2sk`) | **なし** | kintone が独自に属性を付与、upstream は無視（GetItem のみ） |
| TTL 属性 | DynamoDB の TTL 設定で自動削除 | 読み出し時 expires_at チェック + no auto-delete | DynamoDB TTL 設定は upstream が、kintone が上乗せで読み出し時チェック（二重防衛） |

**結論**: テーブル共有方針を **維持**。Phase 5 で kintone 側が GSI1/GSI2 属性と sparse index を追加しても、upstream は関与しない（GetItem direct access のため）。

---

## Open Questions for Future

1. **DynamoDB でのテーブル共有の運用**: kintone TTL（例：90 日）と idproxy TTL（例：30 日）が異なる場合、どちらの方針で削除するか？
   - 案 A: 両者の AND（strictest = 30 日）で DynamoDB TTL 設定
   - 案 B: TTL 属性を別ける（`kintone_ttl`, `idproxy_ttl`）
   - 案 C: 読み出し時に両者の最短を比較
   - → Phase 5 で決定予定

2. **Redis でのクライアント Close 抑制**: wrapper adapter で close をバイパスする場合、upstream の cleanup goroutine（キー自動削除タイマー）の停止はどのタイミングで行うか？
   - → idproxy close wrapper の責務として Phase 1 で設計

3. **miniredis Lua 制限**: Phase 0 で動作確認できた範囲が unit test 環境に限定される可能性
   - → 本番 Redis との integration test を CI に追加（actions services redis）予定

---

## References

- idproxy v0.4.2: `github.com/youyo/idproxy@v0.4.2`
  - SQLite: `store/sqlite/store.go`
  - Redis: `store/redis/store.go` + `store/redis/consume.lua`
  - DynamoDB: `store/dynamodb/store.go` (line 68-179)
- 計画書: `plans/binary-imagining-lemur.md` §3 (Backend 別の物理共有方針), §4 (DynamoDB schema)
- miniredis: `github.com/alicebob/miniredis/v2` Lua サポート確認
