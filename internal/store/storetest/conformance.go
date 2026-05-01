// Package storetest は store パッケージの interface 適合テストヘルパーを提供する。
//
// 各 backend 実装パッケージ（memory / sqlite / redis / dynamodb）は
// このパッケージの Run*Conformance を呼ぶことで、最小限の挙動契約を担保できる。
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/store"
)

// RunTokenStoreConformance は TokenStore の Put/Get/Update/ListByDomain/Delete/ErrNotFound を検証する。
//
// makeStore は新しい空の TokenStore と、テスト終了時に呼ばれる cleanup を返す。
func RunTokenStoreConformance(t *testing.T, makeStore func() (store.TokenStore, func())) {
	t.Helper()
	t.Run("Put_Get_Update_Delete", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()

		tok := store.Token{
			Domain:      "example.cybozu.com",
			PrincipalID: "oauth:user1",
			AuthType:    store.AuthTypeOAuth,
			AccessToken: "at-1",
		}
		if err := s.Put(ctx, tok); err != nil {
			t.Fatalf("Put: %v", err)
		}
		got, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.AccessToken != "at-1" {
			t.Fatalf("AccessToken got=%q want=%q", got.AccessToken, "at-1")
		}
		if got.UpdatedAt.IsZero() {
			t.Fatalf("UpdatedAt should be auto-set when Put receives zero time")
		}

		// 同キー上書き
		tok.AccessToken = "at-2"
		if err := s.Put(ctx, tok); err != nil {
			t.Fatalf("Put(update): %v", err)
		}
		got2, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType)
		if err != nil {
			t.Fatalf("Get(after-update): %v", err)
		}
		if got2.AccessToken != "at-2" {
			t.Fatalf("after update AccessToken got=%q want=%q", got2.AccessToken, "at-2")
		}

		// Delete + 不在 Get
		if err := s.Delete(ctx, tok.Domain, tok.PrincipalID, tok.AuthType); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Get(ctx, tok.Domain, tok.PrincipalID, tok.AuthType); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Get(after-delete): want ErrNotFound, got %v", err)
		}

		// Delete 不在 no-op
		if err := s.Delete(ctx, tok.Domain, tok.PrincipalID, tok.AuthType); err != nil {
			t.Fatalf("Delete(missing) should be no-op, got %v", err)
		}
	})

	t.Run("ListByDomain_FiltersAndSorts", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()

		tokens := []store.Token{
			{Domain: "a.cybozu.com", PrincipalID: "oauth:userB", AuthType: store.AuthTypeOAuth, AccessToken: "ab"},
			{Domain: "a.cybozu.com", PrincipalID: "oauth:userA", AuthType: store.AuthTypeOAuth, AccessToken: "aa"},
			{Domain: "a.cybozu.com", PrincipalID: "", AuthType: store.AuthTypeAPIToken, APIToken: "k1"},
			{Domain: "b.cybozu.com", PrincipalID: "oauth:userC", AuthType: store.AuthTypeOAuth, AccessToken: "bc"},
		}
		for _, tk := range tokens {
			if err := s.Put(ctx, tk); err != nil {
				t.Fatalf("Put: %v", err)
			}
		}

		got, err := s.ListByDomain(ctx, "a.cybozu.com", store.AuthTypeOAuth)
		if err != nil {
			t.Fatalf("ListByDomain: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len got=%d want=2 (only oauth in a.cybozu.com)", len(got))
		}
		if got[0].PrincipalID != "oauth:userA" || got[1].PrincipalID != "oauth:userB" {
			t.Fatalf("sort order wrong: %v", []string{got[0].PrincipalID, got[1].PrincipalID})
		}

		gotApi, err := s.ListByDomain(ctx, "a.cybozu.com", store.AuthTypeAPIToken)
		if err != nil {
			t.Fatalf("ListByDomain(api): %v", err)
		}
		if len(gotApi) != 1 {
			t.Fatalf("api len got=%d want=1", len(gotApi))
		}
	})
}

// RunCacheStoreConformance は CacheStore の基本契約を検証する。
func RunCacheStoreConformance(t *testing.T, makeStore func() (store.CacheStore, func())) {
	t.Helper()
	t.Run("Put_Get_Delete", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		if err := s.Put(ctx, "v1:app:1", []byte(`{"a":1}`), time.Minute); err != nil {
			t.Fatalf("Put: %v", err)
		}
		v, err := s.Get(ctx, "v1:app:1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if string(v) != `{"a":1}` {
			t.Fatalf("Get got=%q", string(v))
		}
		if err := s.Delete(ctx, "v1:app:1"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := s.Get(ctx, "v1:app:1"); !errors.Is(err, store.ErrCacheMiss) {
			t.Fatalf("Get(after-delete): want ErrCacheMiss got %v", err)
		}
	})

	t.Run("TTL_Expiration", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		if err := s.Put(ctx, "v1:short", []byte("x"), 5*time.Millisecond); err != nil {
			t.Fatalf("Put: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
		if _, err := s.Get(ctx, "v1:short"); !errors.Is(err, store.ErrCacheMiss) {
			t.Fatalf("Get(expired): want ErrCacheMiss got %v", err)
		}
	})

	t.Run("DeleteByPrefix", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		_ = s.Put(ctx, "v1:app:1", []byte("a"), time.Minute)
		_ = s.Put(ctx, "v1:app:2", []byte("b"), time.Minute)
		_ = s.Put(ctx, "v1:fields:1", []byte("c"), time.Minute)
		n, err := s.DeleteByPrefix(ctx, "v1:app:")
		if err != nil {
			t.Fatalf("DeleteByPrefix: %v", err)
		}
		if n != 2 {
			t.Fatalf("DeleteByPrefix count got=%d want=2", n)
		}
		// fields は残る
		if _, err := s.Get(ctx, "v1:fields:1"); err != nil {
			t.Fatalf("Get(fields): %v", err)
		}
	})

	t.Run("Stats", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		_ = s.Put(ctx, "v1:app:1", []byte("a"), time.Minute)
		st, err := s.Stats(ctx)
		if err != nil {
			t.Fatalf("Stats: %v", err)
		}
		if st.EntryCount < 1 {
			t.Fatalf("EntryCount got=%d want>=1", st.EntryCount)
		}
		if st.Backend == "" {
			t.Fatalf("Stats.Backend should not be empty")
		}
	})

	t.Run("DeleteMissing_NoOp", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		if err := s.Delete(ctx, "v1:nope"); err != nil {
			t.Fatalf("Delete missing should be no-op: %v", err)
		}
	})
}

// RunSigningKeyStoreConformance は SigningKeyStore.LoadOrCreate のべき等性を検証する。
func RunSigningKeyStoreConformance(t *testing.T, makeStore func() (store.SigningKeyStore, func())) {
	t.Helper()
	t.Run("LoadOrCreate_Idempotent", func(t *testing.T) {
		s, cleanup := makeStore()
		defer cleanup()
		ctx := context.Background()
		k1, err := s.LoadOrCreate(ctx)
		if err != nil {
			t.Fatalf("LoadOrCreate(1): %v", err)
		}
		k2, err := s.LoadOrCreate(ctx)
		if err != nil {
			t.Fatalf("LoadOrCreate(2): %v", err)
		}
		if k1 != k2 {
			t.Fatalf("LoadOrCreate should return the same key on repeated calls (k1=%p k2=%p)", k1, k2)
		}
		if k1 == nil || k1.Curve == nil {
			t.Fatalf("LoadOrCreate returned invalid key: %#v", k1)
		}
	})
}
