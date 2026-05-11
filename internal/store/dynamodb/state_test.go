package dynamodb

import (
	"testing"

	"github.com/youyo/kintone/internal/store"
	"github.com/youyo/kintone/internal/store/storetest"
)

func TestDynamoDBStateStore_Conformance(t *testing.T) {
	storetest.RunStateStoreConformance(t, func() (store.StateStore, func()) {
		f := newFakeDDB(t)
		ss := newStateStore(f, f.tableName)
		return ss, func() { _ = ss.Close() }
	})
}
