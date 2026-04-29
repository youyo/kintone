package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestNewAPITokenAuthenticator(t *testing.T) {
	t.Parallel()

	t.Run("AT-1 正常生成", func(t *testing.T) {
		t.Parallel()
		a, err := NewAPITokenAuthenticator("abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a == nil {
			t.Fatal("expected non-nil authenticator")
		}
	})

	t.Run("AT-2 空トークン拒否", func(t *testing.T) {
		t.Parallel()
		_, err := NewAPITokenAuthenticator("")
		if !errors.Is(err, ErrEmptyAPIToken) {
			t.Fatalf("expected ErrEmptyAPIToken, got %v", err)
		}
	})
}

func TestAPITokenAuthenticator_Apply(t *testing.T) {
	t.Parallel()

	t.Run("AT-3 ヘッダ付与", func(t *testing.T) {
		t.Parallel()
		a, _ := NewAPITokenAuthenticator("abc")
		req, _ := http.NewRequest(http.MethodGet, "https://example.cybozu.com/k/v1/app.json", nil)
		if err := a.Apply(context.Background(), req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := req.Header.Get("X-Cybozu-API-Token")
		if got != "abc" {
			t.Fatalf("expected token header 'abc', got %q", got)
		}
	})

	t.Run("AT-4 冪等", func(t *testing.T) {
		t.Parallel()
		a, _ := NewAPITokenAuthenticator("abc")
		req, _ := http.NewRequest(http.MethodGet, "https://x/", nil)
		_ = a.Apply(context.Background(), req)
		_ = a.Apply(context.Background(), req)
		values := req.Header.Values("X-Cybozu-API-Token")
		if len(values) != 1 {
			t.Fatalf("expected single header value, got %v", values)
		}
		if values[0] != "abc" {
			t.Fatalf("expected 'abc', got %q", values[0])
		}
	})

	t.Run("AT-5 カンマ区切り透過", func(t *testing.T) {
		t.Parallel()
		a, _ := NewAPITokenAuthenticator("a,b,c")
		req, _ := http.NewRequest(http.MethodGet, "https://x/", nil)
		_ = a.Apply(context.Background(), req)
		if got := req.Header.Get("X-Cybozu-API-Token"); got != "a,b,c" {
			t.Fatalf("expected 'a,b,c', got %q", got)
		}
	})

	t.Run("AT-6 nil ctx でも動作", func(t *testing.T) {
		t.Parallel()
		a, _ := NewAPITokenAuthenticator("abc")
		req, _ := http.NewRequest(http.MethodGet, "https://x/", nil)
		//nolint:staticcheck // テスト目的で意図的に nil ctx を渡す
		if err := a.Apply(nil, req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := req.Header.Get("X-Cybozu-API-Token"); got != "abc" {
			t.Fatalf("expected 'abc', got %q", got)
		}
	})

	t.Run("nil request はエラー", func(t *testing.T) {
		t.Parallel()
		a, _ := NewAPITokenAuthenticator("abc")
		err := a.Apply(context.Background(), nil)
		if err == nil {
			t.Fatal("expected error for nil request")
		}
	})
}

// インターフェイス満足の compile-time check
var _ Authenticator = (*APITokenAuthenticator)(nil)
