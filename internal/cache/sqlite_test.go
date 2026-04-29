package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func newTempStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "cache.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

// C-4: 親ディレクトリ自動作成 + DB 作成.
func TestOpen_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nest", "cache.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("DB file not created: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("parent dir missing: %v", err)
	}
}

// C-17: DB ファイル権限 0o600（新規作成時）.
func TestOpen_FilePermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: file permission semantics differ")
	}
	_, path := newTempStore(t)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("file perm = %o, want 0o600", mode)
	}
}

// C-18: 親ディレクトリ権限 0o700.
func TestOpen_ParentDirPermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: dir permission semantics differ")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "newcache", "cache.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	fi, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o700 {
		t.Errorf("dir perm = %o, want 0o700", mode)
	}
}

// C-5: 既存 DB 再オープン.
func TestOpen_Reopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.db")
	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open #1: %v", err)
	}
	if err := s1.Put(context.Background(), "v1:k", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open #2: %v", err)
	}
	defer func() { _ = s2.Close() }()

	got, err := s2.Get(context.Background(), "v1:k")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if string(got) != "v" {
		t.Errorf("got %q, want v", got)
	}
}

// C-6: 書き込み不可なパス.
func TestOpen_InvalidPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows: path semantics differ")
	}
	// 存在しないルート配下の sub に書き込もうとする → root MkdirAll 失敗
	_, err := Open("/nonexistent_root_for_test/cache.db")
	if err == nil {
		t.Fatal("expected error for unwritable path")
	}
}

// C-7: Put → Get round trip.
func TestPutGet(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, "v1:foo", []byte("bar"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(ctx, "v1:foo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "bar" {
		t.Errorf("got %q, want bar", got)
	}
}

// C-8: 未存在キー.
func TestGet_Missing(t *testing.T) {
	s, _ := newTempStore(t)
	_, err := s.Get(context.Background(), "v1:nope")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("err = %v, want ErrCacheMiss", err)
	}
}

// C-9: 期限切れ.
func TestGet_Expired(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, "v1:exp", []byte("v"), time.Nanosecond); err != nil {
		t.Fatalf("Put: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	_, err := s.Get(ctx, "v1:exp")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("err = %v, want ErrCacheMiss", err)
	}
}

// C-10: Put 上書き.
func TestPut_Overwrite(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	_ = s.Put(ctx, "v1:k", []byte("old"), time.Hour)
	_ = s.Put(ctx, "v1:k", []byte("new"), time.Hour)
	got, _ := s.Get(ctx, "v1:k")
	if string(got) != "new" {
		t.Errorf("got %q, want new", got)
	}
}

// C-11: Delete.
func TestDelete(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	_ = s.Put(ctx, "v1:k", []byte("v"), time.Hour)
	if err := s.Delete(ctx, "v1:k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := s.Get(ctx, "v1:k")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("err = %v, want ErrCacheMiss", err)
	}
}

// C-12: DeleteByPrefix.
func TestDeleteByPrefix(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	_ = s.Put(ctx, "v1:app:foo:1", []byte("a"), time.Hour)
	_ = s.Put(ctx, "v1:app:foo:2", []byte("b"), time.Hour)
	_ = s.Put(ctx, "v1:fields:foo:1", []byte("c"), time.Hour)

	n, err := s.DeleteByPrefix(ctx, "v1:app:foo:")
	if err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}

	if _, err := s.Get(ctx, "v1:fields:foo:1"); err != nil {
		t.Errorf("fields entry should remain: %v", err)
	}
	if _, err := s.Get(ctx, "v1:app:foo:1"); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("app:foo:1 should be deleted: %v", err)
	}
}

// DeleteByPrefix で LIKE メタ文字（%）が誤マッチしないこと.
func TestDeleteByPrefix_EscapesLikeMeta(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	_ = s.Put(ctx, "v1:app:foo", []byte("a"), time.Hour)
	_ = s.Put(ctx, "v1:other", []byte("b"), time.Hour)

	// "%" を含む prefix は SQL 上で wildcard 化されないこと
	n, err := s.DeleteByPrefix(ctx, "v1:%")
	if err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
	if n != 0 {
		t.Errorf("escaped prefix matched %d rows, want 0", n)
	}

	// 通常の prefix は機能する
	n, _ = s.DeleteByPrefix(ctx, "v1:")
	if n != 2 {
		t.Errorf("v1: prefix matched %d rows, want 2", n)
	}
}

// C-13: Stats.
func TestStats(t *testing.T) {
	s, path := newTempStore(t)
	ctx := context.Background()
	_ = s.Put(ctx, "v1:a", []byte("x"), time.Hour)
	_ = s.Put(ctx, "v1:b", []byte("x"), time.Hour)
	_ = s.Put(ctx, "v1:c", []byte("x"), time.Nanosecond)
	time.Sleep(5 * time.Millisecond)

	st, err := s.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Total != 3 {
		t.Errorf("Total = %d, want 3", st.Total)
	}
	if st.Expired != 1 {
		t.Errorf("Expired = %d, want 1", st.Expired)
	}
	if st.DBPath != path {
		t.Errorf("DBPath = %q, want %q", st.DBPath, path)
	}
	if !st.DBExists {
		t.Error("DBExists should be true")
	}
	if st.DBSizeBytes <= 0 {
		t.Errorf("DBSizeBytes = %d, want > 0", st.DBSizeBytes)
	}
	if st.OldestStored.IsZero() {
		t.Error("OldestStored should be set")
	}
}

// C-15: 並行 Put（WAL + busy_timeout で破損なし）.
func TestConcurrentPut(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	const N = 50

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := "v1:concurrent"
			value := []byte{byte(i)}
			if err := s.Put(ctx, key, value, time.Hour); err != nil {
				t.Errorf("Put #%d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	// 最後の値かは保証されないが、何らかの値が読めればよい
	got, err := s.Get(ctx, "v1:concurrent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %v, want single byte", got)
	}
}

// C-16: Close 後の操作はエラー.
func TestOperationsAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := s.Get(context.Background(), "k"); err == nil {
		t.Error("Get after close should error")
	}
	if err := s.Put(context.Background(), "k", []byte("v"), time.Hour); err == nil {
		t.Error("Put after close should error")
	}
}

// OpenIfExists: 不在時.
func TestOpenIfExists_Missing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no.db")
	s, exists, err := OpenIfExists(path)
	if err != nil {
		t.Fatalf("OpenIfExists: %v", err)
	}
	if exists {
		t.Error("exists should be false")
	}
	if s != nil {
		t.Error("store should be nil")
		_ = s.Close()
	}
	// ファイルは作成されていないこと
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file created: %v", err)
	}
}

// OpenIfExists: 既存.
func TestOpenIfExists_Existing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = s.Close()

	s2, exists, err := OpenIfExists(path)
	if err != nil {
		t.Fatalf("OpenIfExists: %v", err)
	}
	if !exists {
		t.Error("exists should be true")
	}
	if s2 == nil {
		t.Fatal("store should not be nil")
	}
	defer func() { _ = s2.Close() }()
}

// Path(): DB パスが返ること.
func TestSQLiteStore_Path(t *testing.T) {
	s, path := newTempStore(t)
	if got := s.Path(); got != path {
		t.Errorf("Path: want %q, got %q", path, got)
	}
}

// C-11 Delete: 存在しないキーは no-op でエラーなし.
func TestDelete_Noop(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	if err := s.Delete(ctx, "no-such-key"); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

// DeleteByPrefix: prefix に一致しないキーは削除されない.
func TestDeleteByPrefix_NoMatch(t *testing.T) {
	s, _ := newTempStore(t)
	ctx := context.Background()
	if err := s.Put(ctx, "v1:app:foo:1", []byte("val"), time.Hour); err != nil {
		t.Fatalf("Put: %v", err)
	}
	n, err := s.DeleteByPrefix(ctx, "v1:fields:")
	if err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
	if n != 0 {
		t.Errorf("deleted: want 0, got %d", n)
	}
	// 元のエントリが残っていること
	if _, err := s.Get(ctx, "v1:app:foo:1"); err != nil {
		t.Errorf("entry should remain: %v", err)
	}
}
