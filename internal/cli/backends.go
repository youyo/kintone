package cli

// このファイルは Storage backend を blank import で集約する。
//
// M12 Phase 6a の制約: `cli.ExecuteWith` が Container を Open する責務を持つため、
// `internal/cli` パッケージ自身で 4 backend (memory/sqlite/redis/dynamodb) を blank import する。
// これにより:
//
//  1. production binary (`cmd/kintone/main.go`) は cli パッケージを import するだけで
//     全 backend が利用可能（cmd/kintone 側の blank import は明示的宣言として残置）
//  2. test binary（`internal/cli/ops/...` 等）も `cli.ExecuteWith` を呼ぶ際に
//     全 backend が register された状態で動作（既存テストの後方互換性を維持）
//
// caller (auth/cache/api/ops/mcp) の Storage 経由化は Phase 6b/6c で実施するため、
// Phase 6a 時点では Container を Open しても use しないが、register は必須。
import (
	_ "github.com/youyo/kintone/internal/store/dynamodb"
	_ "github.com/youyo/kintone/internal/store/memory"
	_ "github.com/youyo/kintone/internal/store/redis"
	_ "github.com/youyo/kintone/internal/store/sqlite"
)
