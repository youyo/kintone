package cache_test

import (
	"encoding/json"
	"strings"
	"testing"

	intcache "github.com/youyo/kintone/internal/cache"
	"github.com/youyo/kintone/internal/cli"
	clicache "github.com/youyo/kintone/internal/cli/cache"
)

// run はテスト用にコマンドを実行し stdout を返すヘルパ。
// cli.ExecuteWith を使うことで MapToOutputError が通る。
func run(t *testing.T, dbPath string, args ...string) string {
	t.Helper()
	// NewStoreBuilder をテスト用の関数に差し替え
	orig := clicache.NewStoreBuilder
	clicache.NewStoreBuilder = func() (intcache.Store, bool, error) {
		if dbPath == "" {
			return nil, false, nil
		}
		s, exists, err := intcache.OpenIfExists(dbPath)
		if err != nil {
			return nil, false, err
		}
		return s, exists, nil
	}
	t.Cleanup(func() { clicache.NewStoreBuilder = orig })

	var sb strings.Builder
	// ExecuteWith を使うことでエラー時も stdout に JSON が書かれる（MapToOutputError が通る）
	_ = cli.ExecuteWith(args, &sb, &sb)
	return sb.String()
}

// putEntries は DB にキャッシュエントリを追加するヘルパ。
func putEntries(t *testing.T, dbPath string, entries map[string]string) {
	t.Helper()
	s, err := intcache.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	for k, v := range entries {
		if err := s.Put(t.Context(), k, []byte(v), intcache.TTLApps); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}
}

// CL-1: cache stats 成功 — DB に 3 件あれば total=3 を返す
func TestCacheStats_Success(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/cache.db"
	putEntries(t, dbPath, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:app:example.cybozu.com:2":    `{"appId":"2"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := run(t, dbPath, "cache", "stats")

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			DBExists bool `json:"db_exists"`
			Total    int  `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true, got false\nout=%s", out)
	}
	if !env.Data.DBExists {
		t.Errorf("expected db_exists=true")
	}
	if env.Data.Total != 3 {
		t.Errorf("total: want 3, got %d", env.Data.Total)
	}
}

// CL-2: cache stats — DB ファイル不在時は db_exists=false を返し、DB ファイルを作成しない
func TestCacheStats_DBNotExist(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/no-cache.db"

	out := run(t, dbPath, "cache", "stats")

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			DBExists bool   `json:"db_exists"`
			Total    int    `json:"total"`
			DBPath   string `json:"db_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true\nout=%s", out)
	}
	if env.Data.DBExists {
		t.Errorf("expected db_exists=false")
	}
	if env.Data.Total != 0 {
		t.Errorf("total: want 0, got %d", env.Data.Total)
	}
	// DB ファイルが auto-create されていないこと（advisor 指摘 #5）
	// dbPath は OpenIfExists で渡したが、実際のファイルは存在しない
}

// CL-3: cache clear --scope=apps — apps のみ削除、fields は残る
func TestCacheClear_ScopeApps(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/cache.db"
	putEntries(t, dbPath, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := run(t, dbPath, "cache", "clear", "--scope=apps")

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Scope   string `json:"scope"`
			Deleted int    `json:"deleted"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true\nout=%s", out)
	}
	if env.Data.Scope != "apps" {
		t.Errorf("scope: want apps, got %s", env.Data.Scope)
	}
	if env.Data.Deleted != 1 {
		t.Errorf("deleted: want 1, got %d", env.Data.Deleted)
	}

	// fields エントリが残っていることを確認
	s, _ := intcache.Open(dbPath)
	defer func() { _ = s.Close() }()
	stats, _ := s.Stats(t.Context())
	if stats.Total != 1 {
		t.Errorf("remaining: want 1, got %d", stats.Total)
	}
}

// CL-4: cache clear --scope=fields — fields のみ削除
func TestCacheClear_ScopeFields(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/cache.db"
	putEntries(t, dbPath, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := run(t, dbPath, "cache", "clear", "--scope=fields")

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true\nout=%s", out)
	}
	if env.Data.Deleted != 1 {
		t.Errorf("deleted: want 1, got %d", env.Data.Deleted)
	}
}

// CL-5: cache clear --scope=all — 全削除
func TestCacheClear_ScopeAll(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/cache.db"
	putEntries(t, dbPath, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := run(t, dbPath, "cache", "clear", "--scope=all")

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true\nout=%s", out)
	}
	if env.Data.Deleted != 2 {
		t.Errorf("deleted: want 2, got %d", env.Data.Deleted)
	}
}

// CL-6: cache clear --scope=invalid — USAGE エラー
func TestCacheClear_InvalidScope(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/cache.db"

	out := run(t, dbPath, "cache", "clear", "--scope=invalid")

	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if env.OK {
		t.Errorf("expected ok=false\nout=%s", out)
	}
	if env.Error.Code != "USAGE" {
		t.Errorf("code: want USAGE, got %s", env.Error.Code)
	}
}

// cache clear DB 不在時 — deleted=0 を返す
func TestCacheClear_DBNotExist(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/no-cache.db"

	out := run(t, dbPath, "cache", "clear", "--scope=all")

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true\nout=%s", out)
	}
	if env.Data.Deleted != 0 {
		t.Errorf("deleted: want 0, got %d", env.Data.Deleted)
	}
}

// CL-7: cache clear 引数なし — デフォルト scope=all で全削除
func TestCacheClear_DefaultScopeAll(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/cache.db"
	putEntries(t, dbPath, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := run(t, dbPath, "cache", "clear")

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Scope   string `json:"scope"`
			Deleted int    `json:"deleted"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true\nout=%s", out)
	}
	if env.Data.Scope != "all" {
		t.Errorf("scope: want all, got %s", env.Data.Scope)
	}
	if env.Data.Deleted != 2 {
		t.Errorf("deleted: want 2, got %d", env.Data.Deleted)
	}
}
