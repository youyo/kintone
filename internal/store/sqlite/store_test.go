package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/youyo/kintone/internal/store"
	sqlitestore "github.com/youyo/kintone/internal/store/sqlite"
	"github.com/youyo/kintone/internal/store/storetest"
)

// makeDB はテスト用の *sql.DB を準備し、cleanup で Close する。
func makeDB(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kintone.db")
	return path, func() {
		// no-op: t.TempDir が cleanup するため明示削除は不要
	}
}

func TestSQLiteTokenStore_Conformance(t *testing.T) {
	storetest.RunTokenStoreConformance(t, func() (store.TokenStore, func()) {
		path, _ := makeDB(t)
		db, err := sqlitestore.OpenDB(context.Background(), path)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		ts := sqlitestore.NewTokenStore(db)
		return ts, func() {
			_ = ts.Close()
			_ = db.Close()
		}
	})
}

func TestSQLiteCacheStore_Conformance(t *testing.T) {
	storetest.RunCacheStoreConformance(t, func() (store.CacheStore, func()) {
		path, _ := makeDB(t)
		db, err := sqlitestore.OpenDB(context.Background(), path)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		cs := sqlitestore.NewCacheStore(db, path)
		return cs, func() {
			_ = cs.Close()
			_ = db.Close()
		}
	})
}

func TestSQLiteSigningKeyStore_Conformance(t *testing.T) {
	storetest.RunSigningKeyStoreConformance(t, func() (store.SigningKeyStore, func()) {
		path, _ := makeDB(t)
		db, err := sqlitestore.OpenDB(context.Background(), path)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		sk := sqlitestore.NewSigningKeyStore(db)
		return sk, func() {
			_ = sk.Close()
			_ = db.Close()
		}
	})
}

func TestSQLiteStateStore_Conformance(t *testing.T) {
	storetest.RunStateStoreConformance(t, func() (store.StateStore, func()) {
		path, _ := makeDB(t)
		db, err := sqlitestore.OpenDB(context.Background(), path)
		if err != nil {
			t.Fatalf("OpenDB: %v", err)
		}
		ss := sqlitestore.NewStateStore(db)
		return ss, func() {
			_ = ss.Close()
			_ = db.Close()
		}
	})
}

func TestSQLiteSigningKeyStore_PersistsAcrossOpen(t *testing.T) {
	path, _ := makeDB(t)

	// 1) 初回 open + 鍵生成
	db1, err := sqlitestore.OpenDB(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenDB(1): %v", err)
	}
	sk1 := sqlitestore.NewSigningKeyStore(db1)
	k1, err := sk1.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatalf("LoadOrCreate(1): %v", err)
	}
	_ = sk1.Close()
	_ = db1.Close()

	// 2) 同 path を再 open し、同じ鍵が返る
	db2, err := sqlitestore.OpenDB(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenDB(2): %v", err)
	}
	defer func() { _ = db2.Close() }()
	sk2 := sqlitestore.NewSigningKeyStore(db2)
	defer func() { _ = sk2.Close() }()
	k2, err := sk2.LoadOrCreate(context.Background())
	if err != nil {
		t.Fatalf("LoadOrCreate(2): %v", err)
	}
	if !k1.PublicKey.Equal(&k2.PublicKey) {
		t.Fatal("signing key did not persist across reopen")
	}
}
