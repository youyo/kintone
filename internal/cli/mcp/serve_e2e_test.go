//go:build e2e

// Phase 8.5 — in-process E2E ハーネス。
//
// 簡略版シナリオ（計画書 §G の縮約）:
//  1. oidcstub と kintonefake を httptest.NewServer で起動
//  2. sqlite backend で store.Container を作成
//  3. SeedTokenForE2E で初期 refresh_token を Storage に書き込み
//  4. oauth.Refresher を直接呼んで refresh フローを実行
//  5. 新 access_token / refresh_token が返り、Storage に永続化されることを確認
//
// mcp serve 全体の HTTP テストは難度が高いため省略している。Phase 8.5 の最低
// スコープ「ハーネスがビルドできて Refresher の refresh 経路が pass する」を満たす。
package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/store"
	_ "github.com/youyo/kintone/internal/store/sqlite"
	"github.com/youyo/kintone/internal/store/storetest"
	"github.com/youyo/kintone/internal/testsupport/kintonefake"
	"github.com/youyo/kintone/internal/testsupport/oidcstub"
)

func TestE2E_OAuthRefreshFlow_SQLite(t *testing.T) {
	t.Parallel()

	// 1) OIDC stub（discovery / jwks 経路の到達確認のみ）
	oidc, err := oidcstub.New(oidcstub.Config{ClientID: "kintone-mcp"})
	if err != nil {
		t.Fatalf("oidcstub.New: %v", err)
	}
	defer oidc.Stop()

	// 2) kintone fake
	kf := kintonefake.New()
	defer kf.Stop()

	// 3) sqlite backend Container
	dir := t.TempDir()
	cfg := &store.Config{Backend: store.BackendSQLite, SQLiteDir: dir}
	container, err := store.OpenFromConfig(cfg)
	if err != nil {
		t.Fatalf("OpenFromConfig sqlite: %v", err)
	}
	t.Cleanup(func() { _ = container.Close(context.Background()) })

	const (
		domain      = "example.cybozu.com"
		principalID = "oauth:user-1"
	)

	// 4) 初期 refresh_token を fake サーバ + Storage 双方に seed
	initialRT := kf.SeedTokenFor(principalID)
	seedTok := store.Token{
		Domain:       domain,
		PrincipalID:  principalID,
		AuthType:     store.AuthTypeOAuth,
		RefreshToken: initialRT,
	}
	if err := storetest.SeedTokenForE2E(context.Background(), container, seedTok); err != nil {
		t.Fatalf("SeedTokenForE2E: %v", err)
	}

	// 5) Refresher で refresh_token grant を実行
	refresher := oauth.NewRefresher(oauth.RefresherConfig{
		TokenEndpoint: kf.URL() + "/oauth2/token",
		ClientID:      "kintone-cli",
		ClientSecret:  "test-secret",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := refresher.Refresh(ctx, initialRT)
	if err != nil {
		t.Fatalf("Refresher.Refresh: %v", err)
	}
	if res.AccessToken == "" {
		t.Fatal("AccessToken empty after refresh")
	}
	if res.RefreshToken == "" || res.RefreshToken == initialRT {
		t.Fatalf("RefreshToken not rotated: got %q (initial %q)", res.RefreshToken, initialRT)
	}

	// 6) Storage に新しい Token を書き戻し、永続化を確認
	updated := seedTok
	updated.AccessToken = res.AccessToken
	updated.RefreshToken = res.RefreshToken
	updated.ExpiresAt = res.ExpiresAt
	if err := storetest.SeedTokenForE2E(context.Background(), container, updated); err != nil {
		t.Fatalf("SeedTokenForE2E (write-back): %v", err)
	}
	tokens, err := container.Tokens()
	if err != nil {
		t.Fatalf("container.Tokens: %v", err)
	}
	got, err := tokens.Get(ctx, domain, principalID, store.AuthTypeOAuth)
	if err != nil {
		t.Fatalf("Tokens.Get: %v", err)
	}
	if got.RefreshToken != res.RefreshToken {
		t.Fatalf("Storage RefreshToken got=%q want=%q", got.RefreshToken, res.RefreshToken)
	}
	if got.AccessToken != res.AccessToken {
		t.Fatalf("Storage AccessToken got=%q want=%q", got.AccessToken, res.AccessToken)
	}

	// 7) OIDC stub の discovery が応答することを最低限確認
	_ = oidc.URL()
}
