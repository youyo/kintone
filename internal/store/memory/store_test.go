package memory_test

import (
	"testing"

	"github.com/youyo/kintone/internal/store"
	"github.com/youyo/kintone/internal/store/memory"
	"github.com/youyo/kintone/internal/store/storetest"
)

func TestMemoryTokenStore_Conformance(t *testing.T) {
	storetest.RunTokenStoreConformance(t, func() (store.TokenStore, func()) {
		s := memory.NewTokenStore()
		return s, func() { _ = s.Close() }
	})
}

func TestMemoryCacheStore_Conformance(t *testing.T) {
	storetest.RunCacheStoreConformance(t, func() (store.CacheStore, func()) {
		s := memory.NewCacheStore()
		return s, func() { _ = s.Close() }
	})
}

func TestMemorySigningKeyStore_Conformance(t *testing.T) {
	storetest.RunSigningKeyStoreConformance(t, func() (store.SigningKeyStore, func()) {
		s := memory.NewSigningKeyStore()
		return s, func() { _ = s.Close() }
	})
}

func TestMemoryStateStore_Conformance(t *testing.T) {
	storetest.RunStateStoreConformance(t, func() (store.StateStore, func()) {
		s := memory.NewStateStore()
		return s, func() { _ = s.Close() }
	})
}
