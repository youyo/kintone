# ADR 0003: 新旧 Storage Interface の互換性検証

## Status
**Accepted** (2026-05-01)

## Scope

M12 で `internal/tokenstore` と `internal/cache` を統合し、新パッケージ `internal/store` に移行する際、以下の互換性を事前確認：

1. **旧実装のメソッドシグネチャ**（`internal/tokenstore.Store`, `internal/cache.Store`）
2. **新インターフェース定義**（`internal/store.TokenStore`, `internal/store.CacheStore`, `internal/store.SigningKeyStore`）
3. **既存テストの mock 互換性**
4. **既存 caller の影響範囲**

## Findings

### 1. TokenStore Interface

#### 旧実装（`internal/tokenstore.Store`）

**ファイル**: `internal/tokenstore/store.go`

```go
type Store interface {
    Get(ctx context.Context, domain, principalID string, authType AuthType) (*Token, error)
    Put(ctx context.Context, t Token) error
    Delete(ctx context.Context, domain, principalID string, authType AuthType) error
    Close() error
}
```

**具象実装の非公式メソッド**:
- `SQLiteStore.ListByDomain(ctx context.Context, domain string, authType AuthType) ([]*Token, error)`
  - 計画書で「interface 化」として記載
  - 型アサーション `store.(*tokenstore.SQLiteStore).ListByDomain(...)` で呼び出している caller が存在

**使用箇所**（grep 確認予定）:
- `internal/cli/auth/{login,logout,status}_test.go` → mock にメソッド実装が必要
- `internal/auth/oauth/provider_test.go` → mock にメソッド実装が必要

#### 新インターフェース（`internal/store.TokenStore`）

```go
type TokenStore interface {
    Get(ctx context.Context, domain, principalID string, authType AuthType) (*Token, error)
    Put(ctx context.Context, t *Token) error
    Delete(ctx context.Context, domain, principalID string, authType AuthType) error
    ListByDomain(ctx context.Context, domain string, authType AuthType) ([]*Token, error)  // ← interface 化
    Close() error
}
```

#### 互換性評価

| メソッド | 旧シグネチャ | 新シグネチャ | 互換性 |
|---------|-----------|----------|--------|
| Get | `(ctx, domain, principalID, authType) → (*Token, error)` | 同じ | ✓ 完全互換 |
| Put | `(ctx, Token) → error` | `(ctx, *Token) → error` | ⚠️ receiver が value → pointer |
| Delete | `(ctx, domain, principalID, authType) → error` | 同じ | ✓ 完全互換 |
| ListByDomain | 非公式メソッド（interface 外） | **(interface 化)** | ✓ 完全一致（interface 昇格） |
| Close | `() → error` | 同じ | ✓ 完全互換 |

**Put の receiver 変更に伴う影響**:
- 旧: `Put(ctx, Token)` — Token を copy
- 新: `Put(ctx, *Token)` — Token pointer を参照
- mock は両方対応可能（interface として *Token を受け入れる）
- 既存 caller は `Put(ctx, token)` → `Put(ctx, &token)` に修正が必要（Phase 6 Wave B で一括）

**結論**: 既存 mock の `ListByDomain` メソッドをインターフェース実装として追加すれば、メソッド形状は完全互換。Put の receiver 変更は Phase 6 Wave B で全 caller を 1 PR で一括修正。

---

### 2. CacheStore Interface

#### 旧実装（`internal/cache.Store`）

**ファイル**: `internal/cache/store.go`

```go
type Store interface {
    Get(ctx context.Context, key string) (string, error)
    Put(ctx context.Context, key, value string, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    DeleteByPrefix(ctx context.Context, prefix string) error
    Close() error
}

type Stats struct {
    DBPath      string  // ← ファイルパス
    DBExists    bool
    DBSizeBytes int64
    Total       int64
    Expired     int64
    OldestStored *time.Time
}
```

#### 新インターフェース（`internal/store.CacheStore`）

```go
type CacheStore interface {
    Get(ctx context.Context, key string) (string, error)
    Put(ctx context.Context, key, value string, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
    DeleteByPrefix(ctx context.Context, prefix string) error
    Stats(ctx context.Context) (*Stats, error)  // ← stats 取得の interface 化
    Close() error
}

type Stats struct {
    Backend         string              // ← "sqlite" / "redis" / "dynamodb" / "memory"
    Location        string              // ← ファイルパス / redis URI / DynamoDB table:region
    Reachable       bool                // ← 接続可能か
    EntryCount      int64               // ← 全エントリ数
    ExpiredCount    *int64              // ← 期限切れエントリ数（pointer、backend により null）
    BackendSpecific map[string]interface{} // ← backend 固有統計
}
```

#### 互換性評価

| メソッド | 旧シグネチャ | 新シグネチャ | 互換性 |
|---------|-----------|----------|--------|
| Get | 同じ | 同じ | ✓ 完全互換 |
| Put | 同じ | 同じ | ✓ 完全互換 |
| Delete | 同じ | 同じ | ✓ 完全互換 |
| DeleteByPrefix | 同じ | 同じ | ✓ 完全互換 |
| Close | 同じ | 同じ | ✓ 完全互換 |
| **Stats** | **構造体フィールド** | **interface メソッド** | ❌ **Breaking Change** |

**Stats の breaking change**:

旧: `cache.Store` が stats を直接フィールド保持（アクセサメソッドなし）

新: `cache.Stats` は `Stats(ctx) (*Stats, error)` メソッドの戻り値

旧 Stats フィールド群:
- `DBPath`: SQLite ファイルパスのみ有意（Redis/DynamoDB には無意味）
- `DBExists`: ファイル存在有無（Redis/DynamoDB には無意味）
- `DBSizeBytes`: ファイルサイズ（Redis/DynamoDB には無意味）

新 Stats フィールド群:
- `Backend`: "sqlite" / "redis" / "dynamodb" / "memory"（backend 非依存）
- `Location`: 統一表現（"sqlite:///path" / "redis://..." / "dynamodb://..." / "memory"）
- `Reachable`: bool（接続状態を統一）
- `EntryCount`: 全エントリ数（全 backend サポート）
- `ExpiredCount`: 期限切れエントリ数（pointer のため Redis では null）
- `BackendSpecific`: map（backend 固有情報）

**既存 caller**:
- `internal/cli/cache/stats.go` — `cache.Stats` 構造体フィールドを JSON 出力
- `internal/service/api/caching_test.go` — mock が stats に直接アクセス（stats フィールド参照）

**影響範囲**:
- `kintone cache stats` の JSON 出力スキーマが変わる（**ユーザー向け breaking change**）
- CHANGELOG で「統合 Storage バックエンド化に伴う必然的な出力形式変更」として明記

**結論**: Stats は Breaking Change だが、計画書の「Phase 0 で確定、Wave B で全 caller を 1 PR で一括移行」方針で対応。shim（旧フィールド互換化）は採用しない（複雑度増加、運用コスト）。

---

### 3. SigningKeyStore Interface（新規）

**ファイル**: `internal/store/signingkey.go`（M12 で新設）

```go
type SigningKeyStore interface {
    // LoadOrCreate: key が存在すればロード、なければ生成して保存
    LoadOrCreate(ctx context.Context) (*ecdsa.PrivateKey, error)
    
    // DeleteAll: テスト cleanup 用（production では不要）
    DeleteAll(ctx context.Context) error
    
    Close() error
}
```

**旧実装なし**（M12 新規機能）。互換性検証対象外。

---

## Mock 互換性と影響範囲

### TokenStore Mock の影響

**既存テストファイル**（grep 対象）:
- `internal/cli/auth/login_test.go` — `tokenstore.Store` mock
- `internal/cli/auth/status_test.go` — `tokenstore.Store` mock
- `internal/cli/auth/logout_test.go` — `tokenstore.Store` mock
- `internal/auth/oauth/provider_test.go` — `tokenstore.Store` mock

**変更内容**:
```go
// 旧 mock
type MockTokenStore struct {
    mock.Mock
}
func (m *MockTokenStore) Get(...) (*tokenstore.Token, error) { ... }
func (m *MockTokenStore) Put(...) error { ... }
// ListByDomain は型アサーション経由で呼ぶため mock に実装不要（interface 外）

// 新 mock（内容同じ、interface が interface 化）
type MockTokenStore struct {
    mock.Mock
}
func (m *MockTokenStore) Get(...) (*store.Token, error) { ... }
func (m *MockTokenStore) Put(...) error { ... }
func (m *MockTokenStore) ListByDomain(...) ([]*store.Token, error) { ... }  // ← interface 昇格で明示的実装
```

**影響度**: 低（interface 定義変更のみ、実装ロジック不変）

### CacheStore Mock の影響

**既存テストファイル**:
- `internal/service/api/caching_test.go` — `cache.Store` mock + Stats 検証
- `internal/cli/cache/cache_test.go` — `cache.Store` mock + Stats 検証

**変更内容**:
```go
// 旧 mock
type MockCacheStore struct {
    Stats cache.Stats
}

// 新 mock
type MockCacheStore struct {
    stats *store.Stats
}
func (m *MockCacheStore) Stats(ctx context.Context) (*store.Stats, error) {
    return m.stats, nil
}
```

**影響度**: 中（Stats 構造体フィールド参照を `.Stats()` メソッド呼び出しに変更）

### 既存 Caller 影響

| ファイル | 変更対象 | 工数 |
|---------|--------|------|
| `internal/cli/api/helpers.go` | store.TokenStore 型に変更（Put の receiver を &token に） | 低 |
| `internal/cli/auth/helpers.go` | 同上 | 低 |
| `internal/cli/mcp/helpers.go` | 同上 | 低 |
| `internal/service/api/caching.go` | cache.CacheStore 型に変更（Stats() メソッド呼び出しに）| 低 |
| `internal/cli/cache/stats.go` | Stats 出力形式を新スキーマに変更 | 中 |
| 全 mock ファイル | interface メソッド追加 / Stats 参照を Stats() 呼び出しに | 中 |

**全体工数**: Phase 6 Wave B で 1 PR に統合（+1.0 日）

---

## Migration Strategy

### Phase 6 Wave B における 1 PR 一括移行

```
1. internal/store パッケージが Phase 5 で完成（全 backend + conformance test pass）
2. Wave B にて以下を 1 PR で実行：
   a. interface 型を旧（tokenstore / cache）→ 新（store）に置換
   b. 全 caller（cli / service / mcp）を新型に update
   c. 全 mock を新 interface に適応
   d. Stats 構造体参照を Stats() メソッド呼び出しに変更
   e. 統合テスト（cache stats JSON スキーマ）を新形式で検証
3. Wave B 終了後、Phase 7 で旧 internal/tokenstore / internal/cache パッケージを削除
```

**shim（互換層）を採用しない理由**:
- Stats の breaking change は本質的（backend 非依存化に必須）
- shim を介した型変換は confusing（caller が旧型を使う錯覚を招く）
- ユーザー側は既に v0.1.0 リリース前のため migration 負担がゼロ
- 1 PR 一括移行により git history が clear

---

## References

- 旧実装:
  - `internal/tokenstore/store.go`
  - `internal/cache/store.go`
- 新設計:
  - 計画書 `plans/binary-imagining-lemur.md` §1 (Phase 1 — TokenStore interface)
  - 計画書 §1 (Phase 1 — CacheStore.Stats backend 非依存スキーマ)
- Phase 6 Wave B 詳細: 計画書 §1 (Phase 6 — Wave B)
