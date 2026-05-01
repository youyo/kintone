package sqlite_test

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	sqlitestore "github.com/youyo/kintone/internal/store/sqlite"
)

// TestConcurrent_TwoPoolsSameFile は 2 つの *sql.DB を同 path で開いて並行 BEGIN IMMEDIATE
// を実行しても busy_timeout=5000 で待ち合わさることを確認する。
//
// time-consuming のため -short では skip。
func TestConcurrent_TwoPoolsSameFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in short mode")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "kintone.db")
	ctx := context.Background()

	db1, err := sqlitestore.OpenDB(ctx, path)
	if err != nil {
		t.Fatalf("OpenDB(1): %v", err)
	}
	defer func() { _ = db1.Close() }()
	db2, err := sqlitestore.OpenDB(ctx, path)
	if err != nil {
		t.Fatalf("OpenDB(2): %v", err)
	}
	defer func() { _ = db2.Close() }()

	const goroutines = 50
	const opsPerGoroutine = 50
	var failures atomic.Int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			db := db1
			if i%2 == 1 {
				db = db2
			}
			for j := 0; j < opsPerGoroutine; j++ {
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					failures.Add(1)
					t.Errorf("BeginTx: %v", err)
					return
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO kintone_kv_cache(key,value,expires_at,created_at) VALUES(?,?,?,?)`,
					"v1:concurrent", []byte{0x01}, int64(1<<62), int64(0)); err != nil {
					_ = tx.Rollback()
					failures.Add(1)
					t.Errorf("Exec: %v", err)
					return
				}
				if err := tx.Commit(); err != nil {
					failures.Add(1)
					t.Errorf("Commit: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("got %d failures", failures.Load())
	}
}
