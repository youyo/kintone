package auth_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	cliauth "github.com/youyo/kintone/internal/cli/auth"
	"github.com/youyo/kintone/internal/store"
)

// AO-1: --principal-id 指定 → 該当のみ Delete
func TestLogout_WithPrincipalID(t *testing.T) {
	c := newMemoryContainer(t)
	ts, err := c.Tokens()
	if err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	future := time.Now().Add(1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain: "example.cybozu.com", PrincipalID: "oauth:alice",
		AuthType: store.AuthTypeOAuth, AccessToken: "alice-tok",
		ExpiresAt: future,
	})
	_ = ts.Put(t.Context(), store.Token{
		Domain: "example.cybozu.com", PrincipalID: "oauth:bob",
		AuthType: store.AuthTypeOAuth, AccessToken: "bob-tok",
		ExpiresAt: future,
	})

	restore := cliauth.SetOpenStoreFn(func() (store.Container, error) { return c, nil })
	t.Cleanup(restore)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")

	var out, errOut bytes.Buffer
	if err := cliauth.ExecuteLogoutWith([]string{"--principal-id", "oauth:alice"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v\nout: %s", err, out.String())
	}

	// alice が削除されていること
	_, err = ts.Get(t.Context(), "example.cybozu.com", "oauth:alice", store.AuthTypeOAuth)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected alice deleted, err=%v", err)
	}
	// bob は残っていること
	_, err = ts.Get(t.Context(), "example.cybozu.com", "oauth:bob", store.AuthTypeOAuth)
	if err != nil {
		t.Errorf("expected bob to remain, err=%v", err)
	}

	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			DeletedCount int `json:"deleted_count"`
		} `json:"data"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if resp.Data.DeletedCount != 1 {
		t.Errorf("deleted_count: got %d, want 1", resp.Data.DeletedCount)
	}
}

// AO-2: --all 指定 → 当該 Domain の全 OAuth エントリを Delete
func TestLogout_All(t *testing.T) {
	c := newMemoryContainer(t)
	ts, err := c.Tokens()
	if err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	future := time.Now().Add(1 * time.Hour)
	for _, pid := range []string{"oauth:alice", "oauth:bob", "oauth:charlie"} {
		_ = ts.Put(t.Context(), store.Token{
			Domain: "example.cybozu.com", PrincipalID: pid,
			AuthType: store.AuthTypeOAuth, AccessToken: "tok",
			ExpiresAt: future,
		})
	}

	restore := cliauth.SetOpenStoreFn(func() (store.Container, error) { return c, nil })
	t.Cleanup(restore)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")

	var out, errOut bytes.Buffer
	if err := cliauth.ExecuteLogoutWith([]string{"--all"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v\nout: %s", err, out.String())
	}

	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			DeletedCount int `json:"deleted_count"`
		} `json:"data"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if resp.Data.DeletedCount != 3 {
		t.Errorf("deleted_count: got %d, want 3", resp.Data.DeletedCount)
	}
}

// AO-3: 該当なし → ok=true / deleted=0
func TestLogout_NotFound(t *testing.T) {
	c := newMemoryContainer(t)
	restore := cliauth.SetOpenStoreFn(func() (store.Container, error) { return c, nil })
	t.Cleanup(restore)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")

	var out, errOut bytes.Buffer
	if err := cliauth.ExecuteLogoutWith([]string{"--principal-id", "oauth:nonexistent"}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp struct {
		OK   bool `json:"ok"`
		Data struct {
			DeletedCount int `json:"deleted_count"`
		} `json:"data"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if resp.Data.DeletedCount != 0 {
		t.Errorf("deleted_count: got %d, want 0", resp.Data.DeletedCount)
	}
}
