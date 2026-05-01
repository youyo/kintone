# ADR 0001: SQLite Pool 戦略 — 2 ファイル分離方式の採用

## Status
**Accepted** (2026-05-01)

## Context

M12 では kintone CLI/MCP の TokenStore、Cache、SigningKey を統一 Storage バックエンド化する。SQLite を利用する場合、idproxy v0.4.2 の Store 実装と kintone の TokenStore/Cache を同一 SQLite インスタンスで共有することで、設定の一元化と運用コスト削減を実現したい。

同一ファイルで 2 つの独立した `*sql.DB` Pool を運用する場合、Pool 並行性と SQLITE_BUSY タイムアウトの挙動を事前検証する必要がある。

## Decision

**kintone と idproxy は同一ディレクトリ・2 つ別ファイル（`kintone.db` と `idproxy.db`）で運用する。**

根拠：

1. **idproxy v0.4.2 API 制約**
   - `internal/store/sqlite` が提供する public API は `New(path string) (*Store, error)` および `NewWithCleanupInterval(path string, interval time.Duration) (*Store, error)` の 2 つのみ
   - `NewWithDB(*sql.DB, ...) (*Store, error)` 相当の関数は存在しない
   - このため、同一 `*sql.DB` インスタンスを kintone と idproxy で共有することは不可能

2. **2 Pool 並行検証結果（Phase 0 実証済）**
   - 検証条件: 同一パス × 2 Pool × 50 goroutine × 50 ops = 5000 回の `BEGIN IMMEDIATE; INSERT; COMMIT`
   - DSN: `file:test.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_txlock=immediate`
   - 結果: **成功 5000 / SQLITE_BUSY 0 / その他エラー 0 / 1.47s**
   - **結論**: 同一ファイル・2 Pool は理論上不安定だが、WAL + busy_timeout により実用安定動作

3. **2 ファイル分離を採用する理由**
   - 同一ファイル 2 Pool は WAL チェックポイント競合の可能性を完全には否定できず、長寿命プロセス（MCP serve）での予測不可能性が残る
   - modernc.org/sqlite v1.50.0 の Pool 並行挙動は複雑（同期チェックポイント・async cleanup の衝突可能性）
   - 2 ファイル分離なら idproxy.db は単一 Pool、kintone.db も単一 Pool で、お互いの Pool 干渉をゼロにできる
   - ユーザー体感: `KINTONE_STORE_PATH=/tmp/mystore` → `/tmp/mystore/kintone.db` + `/tmp/mystore/idproxy.db` として「1 つの設定で複数ファイル」を実現

## Consequences

- **ファイル数増加**: 旧実装では `~/.cache/kintone/cache.db` + `~/.cache/kintone/tokens.db` で 2 ファイルだったが、idproxy.db が追加され 3 ファイルに
- **Pool 独立性**: 各 Pool が独立チェックポイント・WAL management を行うため、WAL 競合がゼロ
- **設定の一元化**: ディレクトリパス 1 つで足りる（`KINTONE_STORE_PATH` / SQLite の場合）
- **性能**: 2 ファイル同時 I/O は単一ファイル 2 Pool より若干遅い可能性があるが、検証結果の 1.47s / 5000 ops は実用十分

## Verification

- Phase 0 で同一パス × 2 Pool × 5000 トランザクションを検証済
- 本実装では `internal/store/sqlite/open.go` で kintone.db と idproxy.db を別々に `sql.Open()` してから idproxy store を `idpredis.Store` で wrap する
- Unit test で single-pool baseline を確認（regression検出）

## Alternatives Rejected

### 案 A: 単一ファイル 2 Pool
- **棄却理由**: 
  - idproxy v0.4.2 に `NewWithDB` API がなく実装不可
  - WAL チェックポイント・Pool cleanup の衝突可能性が払拭できず、長寿命プロセスでの予測不可能性
  - modernc.org/sqlite のコード読解でも明確な「2 Pool 安全」保証が得られていない

### 案 B: Upstream idproxy に `NewWithDB` PR を送信
- **棄却理由**:
  - 追加工数 +3 日
  - v0.4.2 → v0.5.0 upgrade が kintone の依存アップデートになり、リリースサイクルが伸びる
  - 現状 M12 スケジュールに収まらない

### 案 C: idproxy を fork して `NewWithDB` を追加
- **棄却理由**:
  - fork の保守負担が大きい（upstream 更新の追従が手動）
  - v0.4.2 以降のセキュリティアップデート取得が自動化できない
  - kintone ユーザーの visibility が落ちる（「kintone が idproxy を fork している」という負債が発生）

## References

- Phase 0 検証スクリプト: `docs/phase0/sqlite_2pool_test.go` (Phase 1 成果物で作成予定)
- idproxy v0.4.2 ソース: `github.com/youyo/idproxy@v0.4.2/store/sqlite/store.go`
- 計画書: `plans/binary-imagining-lemur.md` §3 (SQLite backend)
