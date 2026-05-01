package memory

import (
	idproxy "github.com/youyo/idproxy"
	idstore "github.com/youyo/idproxy/store"
)

// newIDProxyMemoryStore は idproxy が要求する Store interface を満たす
// インメモリ実装を返す。idproxy 公式の MemoryStore を thin wrap している。
func newIDProxyMemoryStore() idproxy.Store {
	return idstore.NewMemoryStore()
}
