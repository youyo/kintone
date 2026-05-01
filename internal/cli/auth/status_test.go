package auth_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	cliauth "github.com/youyo/kintone/internal/cli/auth"
	"github.com/youyo/kintone/internal/store"
)

// AS-1: TokenStore に複数 PrincipalID → 配列で出力
func TestStatus_MultiplePrincipalIDs(t *testing.T) {
	c := newMemoryContainer(t)
	ts, err := c.Tokens()
	if err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	future := time.Now().Add(1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain: "example.cybozu.com", PrincipalID: "oauth:alice",
		AuthType: store.AuthTypeOAuth, AccessToken: "alice-token",
		RefreshToken: "alice-refresh", ExpiresAt: future,
	})
	_ = ts.Put(t.Context(), store.Token{
		Domain: "example.cybozu.com", PrincipalID: "oauth:bob",
		AuthType: store.AuthTypeOAuth, AccessToken: "bob-token",
		RefreshToken: "bob-refresh", ExpiresAt: future,
	})

	restore := cliauth.SetOpenStoreFn(func() (store.Container, error) { return c, nil })
	t.Cleanup(restore)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")

	var out, errOut bytes.Buffer
	if err := cliauth.ExecuteStatusWith([]string{}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v\nout: %s", err, out.String())
	}

	var resp struct {
		OK   bool `json:"ok"`
		Data []struct {
			PrincipalID string `json:"principal_id"`
		} `json:"data"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if len(resp.Data) < 2 {
		t.Errorf("expected at least 2 entries, got %d", len(resp.Data))
	}
}

// AS-2: access_token がマスクされること
func TestStatus_AccessTokenMasked(t *testing.T) {
	c := newMemoryContainer(t)
	ts, err := c.Tokens()
	if err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	future := time.Now().Add(1 * time.Hour)
	_ = ts.Put(t.Context(), store.Token{
		Domain: "example.cybozu.com", PrincipalID: "oauth:alice",
		AuthType: store.AuthTypeOAuth, AccessToken: "super-secret-access-token-1234567890",
		RefreshToken: "ref", ExpiresAt: future,
	})

	restore := cliauth.SetOpenStoreFn(func() (store.Container, error) { return c, nil })
	t.Cleanup(restore)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")

	var out, errOut bytes.Buffer
	if err := cliauth.ExecuteStatusWith([]string{}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v\nout: %s", err, out.String())
	}
	body := out.String()
	if strings.Contains(body, "super-secret-access-token-1234567890") {
		t.Errorf("access_token should be masked, got: %s", body)
	}
	// マスク形式: 先頭4 + "..." + 末尾4
	// "super-secret-access-token-1234567890" → "supe...7890"
	if !strings.Contains(body, "supe...7890") {
		t.Errorf("expected masked format, got: %s", body)
	}
}

// AS-3: TokenStore に該当なし → 空配列 / ok=true
func TestStatus_Empty(t *testing.T) {
	c := newMemoryContainer(t)
	restore := cliauth.SetOpenStoreFn(func() (store.Container, error) { return c, nil })
	t.Cleanup(restore)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")

	var out, errOut bytes.Buffer
	if err := cliauth.ExecuteStatusWith([]string{}, &out, &errOut); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		OK   bool  `json:"ok"`
		Data []any `json:"data"`
	}
	if parseErr := json.Unmarshal(out.Bytes(), &resp); parseErr != nil {
		t.Fatalf("parse JSON: %v, out=%q", parseErr, out.String())
	}
	if !resp.OK {
		t.Error("expected ok=true")
	}
	if len(resp.Data) != 0 {
		t.Errorf("expected empty data, got %d entries", len(resp.Data))
	}
}

// AS-4: Container オープン失敗 → 戻り値が non-nil error
func TestStatus_DBOpenFail(t *testing.T) {
	restore := cliauth.SetOpenStoreFn(func() (store.Container, error) {
		return nil, errors.New("backend unavailable")
	})
	t.Cleanup(restore)

	t.Setenv("KINTONE_DOMAIN", "example.cybozu.com")

	var out, errOut bytes.Buffer
	err := cliauth.ExecuteStatusWith([]string{}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
