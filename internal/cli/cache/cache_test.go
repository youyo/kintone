package cache_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	clicache "github.com/youyo/kintone/internal/cli/cache"
	"github.com/youyo/kintone/internal/store"
	_ "github.com/youyo/kintone/internal/store/memory"
)

// newMemoryContainer は test 用の memory backend Container を生成する。
// t.Cleanup で Close される。
func newMemoryContainer(t *testing.T) store.Container {
	t.Helper()
	c, err := store.OpenFromConfig(&store.Config{Backend: store.BackendMemory})
	if err != nil {
		t.Fatalf("OpenFromConfig(memory): %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// putCacheEntries は CacheStore に key/value を登録するヘルパ。
func putCacheEntries(t *testing.T, c store.Container, entries map[string]string) {
	t.Helper()
	cs, err := c.CacheForAdmin()
	if err != nil {
		t.Fatalf("CacheForAdmin: %v", err)
	}
	for k, v := range entries {
		if err := cs.Put(t.Context(), k, []byte(v), store.TTLApps); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}
}

// runStats は cache stats を test 用 entry で実行し、stdout を返す。
func runStats(t *testing.T, c store.Container, args ...string) string {
	t.Helper()
	restore := clicache.SetNewContainerBuilder(func() (store.Container, error) { return c, nil })
	t.Cleanup(restore)

	var sb strings.Builder
	_ = clicache.ExecuteCacheStatsWith(args, &sb, &sb)
	return sb.String()
}

// runClear は cache clear を test 用 entry で実行し、stdout を返す。
func runClear(t *testing.T, c store.Container, args ...string) string {
	t.Helper()
	restore := clicache.SetNewContainerBuilder(func() (store.Container, error) { return c, nil })
	t.Cleanup(restore)

	var sb strings.Builder
	_ = clicache.ExecuteCacheClearWith(args, &sb, &sb)
	return sb.String()
}

// CL-1: cache stats — 新 schema (backend / location / reachable / entry_count / expired_count / backend_specific)
func TestCacheStats_Schema(t *testing.T) {
	c := newMemoryContainer(t)
	putCacheEntries(t, c, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:app:example.cybozu.com:2":    `{"appId":"2"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := runStats(t, c)

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Backend         string         `json:"backend"`
			Location        string         `json:"location"`
			Reachable       bool           `json:"reachable"`
			EntryCount      int64          `json:"entry_count"`
			ExpiredCount    *int64         `json:"expired_count"`
			BackendSpecific map[string]any `json:"backend_specific"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true, got false\nout=%s", out)
	}
	if env.Data.Backend != "memory" {
		t.Errorf("backend: want memory, got %q", env.Data.Backend)
	}
	if env.Data.Location != "memory://" {
		t.Errorf("location: want memory://, got %q", env.Data.Location)
	}
	if !env.Data.Reachable {
		t.Errorf("expected reachable=true")
	}
	if env.Data.EntryCount != 3 {
		t.Errorf("entry_count: want 3, got %d", env.Data.EntryCount)
	}
	if env.Data.ExpiredCount == nil {
		t.Errorf("expired_count: expected non-null pointer for memory backend")
	}
}

// CL-2: cache stats — 空 backend では entry_count=0 / reachable=true（旧 db_exists=false の置換）
func TestCacheStats_Empty(t *testing.T) {
	c := newMemoryContainer(t)
	out := runStats(t, c)

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Backend    string `json:"backend"`
			Reachable  bool   `json:"reachable"`
			EntryCount int64  `json:"entry_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
		t.Fatalf("parse JSON: %v\nout=%s", err, out)
	}
	if !env.OK {
		t.Errorf("expected ok=true\nout=%s", out)
	}
	if env.Data.EntryCount != 0 {
		t.Errorf("entry_count: want 0, got %d", env.Data.EntryCount)
	}
	if !env.Data.Reachable {
		t.Errorf("reachable: want true (memory backend always reachable)")
	}
	if env.Data.Backend != "memory" {
		t.Errorf("backend: want memory, got %q", env.Data.Backend)
	}
}

// CL-3: cache clear --scope=apps — apps のみ削除、fields は残る
func TestCacheClear_ScopeApps(t *testing.T) {
	c := newMemoryContainer(t)
	putCacheEntries(t, c, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := runClear(t, c, "--scope=apps")

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
	cs, _ := c.CacheForAdmin()
	stats, _ := cs.Stats(t.Context())
	if stats.EntryCount != 1 {
		t.Errorf("remaining: want 1, got %d", stats.EntryCount)
	}
}

// CL-4: cache clear --scope=fields — fields のみ削除
func TestCacheClear_ScopeFields(t *testing.T) {
	c := newMemoryContainer(t)
	putCacheEntries(t, c, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := runClear(t, c, "--scope=fields")

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
	c := newMemoryContainer(t)
	putCacheEntries(t, c, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := runClear(t, c, "--scope=all")

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
	c := newMemoryContainer(t)

	out := runClear(t, c, "--scope=invalid")

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

// CL-7: cache clear 引数なし — デフォルト scope=all で全削除
func TestCacheClear_DefaultScopeAll(t *testing.T) {
	c := newMemoryContainer(t)
	putCacheEntries(t, c, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := runClear(t, c)

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

// CL-8: cache clear --key <prefix> — 任意プレフィックス削除
func TestCacheClear_KeyPrefix(t *testing.T) {
	c := newMemoryContainer(t)
	putCacheEntries(t, c, map[string]string{
		"v1:app:example.cybozu.com:1":    `{"appId":"1"}`,
		"v1:app:example.cybozu.com:2":    `{"appId":"2"}`,
		"v1:fields:example.cybozu.com:1": `{"properties":{}}`,
	})

	out := runClear(t, c, "--key", "v1:app:")

	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Key     string `json:"key"`
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
	if env.Data.Key != "v1:app:" {
		t.Errorf("key: want v1:app:, got %q", env.Data.Key)
	}
	// --key の場合 scope は出力されない（omitempty）
	if env.Data.Scope != "" {
		t.Errorf("scope: want empty (omitted), got %q", env.Data.Scope)
	}
	if env.Data.Deleted != 2 {
		t.Errorf("deleted: want 2, got %d", env.Data.Deleted)
	}
}

// CL-9: cache clear --scope と --key の両方指定 → USAGE エラー
func TestCacheClear_ScopeAndKeyMutuallyExclusive(t *testing.T) {
	c := newMemoryContainer(t)

	out := runClear(t, c, "--scope=apps", "--key", "v1:app:")

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

// CL-10: cache clear --key 空文字列 → USAGE エラー
func TestCacheClear_EmptyKey(t *testing.T) {
	c := newMemoryContainer(t)

	out := runClear(t, c, "--key", "")

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
