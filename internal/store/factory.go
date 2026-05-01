package store

import (
	"fmt"
	"sync"
)

// Opener は backend ごとに登録される Container 構築関数。
//
// 各 backend パッケージ（[internal/store/memory] 等）が init() 内で [RegisterOpener] を呼び、
// factory がここから dispatch する。これにより factory は backend パッケージへの
// 直接 import が不要となり、循環依存を回避しつつ Phase 別の incremental な追加を可能にする。
type Opener func(cfg *Config) (Container, error)

var (
	openersMu sync.RWMutex
	openers   = map[string]Opener{}
)

// RegisterOpener は backend 名と Opener の対応を登録する。
// 重複登録は最後の登録で上書きする（テスト用のため）。
func RegisterOpener(backend string, op Opener) {
	openersMu.Lock()
	defer openersMu.Unlock()
	openers[backend] = op
}

// OpenFromEnv は環境変数から Container を構築する。
func OpenFromEnv() (Container, error) {
	return OpenFromConfig(LoadFromEnv())
}

// OpenFromConfig は Config から Container を構築する。
//
// Phase 1 では memory backend のみ実装される。sqlite / redis / dynamodb は
// [ErrUnsupportedBackend] を返す。
func OpenFromConfig(cfg *Config) (Container, error) {
	if cfg == nil {
		return nil, fmt.Errorf("store: nil config")
	}
	openersMu.RLock()
	op, ok := openers[cfg.Backend]
	openersMu.RUnlock()
	if !ok {
		switch cfg.Backend {
		case BackendSQLite, BackendRedis, BackendDynamoDB:
			return nil, fmt.Errorf("%w: %q (Phase 1 supports memory only)", ErrUnsupportedBackend, cfg.Backend)
		default:
			return nil, fmt.Errorf("%w: %q", ErrUnsupportedBackend, cfg.Backend)
		}
	}
	return op(cfg)
}
