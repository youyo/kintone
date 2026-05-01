package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/youyo/kintone/internal/store"
	sqlitestore "github.com/youyo/kintone/internal/store/sqlite"
)

func TestContainer_LocationStringAndLazyInit(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	c, err := sqlitestore.NewContainer(dir)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if loc := c.LocationString(); loc != "sqlite:///"+abs {
		t.Fatalf("LocationString got=%q want=%q", loc, "sqlite:///"+abs)
	}

	// IDProxyStore に触らない経路では idproxy.db ファイルは作成されない
	if _, err := c.Tokens(); err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	if _, err := os.Stat(filepath.Join(abs, "idproxy.db")); err == nil {
		t.Fatal("idproxy.db should not be created when IDProxyStore() is not called")
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat idproxy.db: %v", err)
	}

	// kintone.db は Tokens 呼び出しで作成される
	if _, err := os.Stat(filepath.Join(abs, "kintone.db")); err != nil {
		t.Fatalf("kintone.db should be created: %v", err)
	}
}

func TestContainer_NewFilePermission0600(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	c, err := sqlitestore.NewContainer(dir)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	if _, err := c.Tokens(); err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	abs, _ := filepath.Abs(dir)
	fi, err := os.Stat(filepath.Join(abs, "kintone.db"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Fatalf("kintone.db perm got=%o want=0600", mode)
	}
}

func TestContainer_CloseIdempotent(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	c, err := sqlitestore.NewContainer(dir)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	if _, err := c.Tokens(); err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	if _, err := c.IDProxyStore(); err != nil {
		t.Fatalf("IDProxyStore: %v", err)
	}
	ctx := context.Background()
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(ctx); err != nil {
		t.Fatalf("Close (second) should be idempotent: %v", err)
	}
}

func TestContainer_KintoneAndIDProxyAreSeparateFiles(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	c, err := sqlitestore.NewContainer(dir)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	if _, err := c.Tokens(); err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	if _, err := c.IDProxyStore(); err != nil {
		t.Fatalf("IDProxyStore: %v", err)
	}
	abs, _ := filepath.Abs(dir)
	if _, err := os.Stat(filepath.Join(abs, "kintone.db")); err != nil {
		t.Fatalf("kintone.db should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(abs, "idproxy.db")); err != nil {
		t.Fatalf("idproxy.db should exist: %v", err)
	}
	// 同一パスでないこと
	if filepath.Join(abs, "kintone.db") == filepath.Join(abs, "idproxy.db") {
		t.Fatal("paths should be different")
	}
}

func TestContainer_OpenViaFactory(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	c, err := sqlitestore.Open(&store.Config{Backend: store.BackendSQLite, SQLiteDir: dir})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	abs, _ := filepath.Abs(dir)
	if !strings.HasSuffix(c.LocationString(), abs) {
		t.Fatalf("LocationString=%q should contain %q", c.LocationString(), abs)
	}
}
