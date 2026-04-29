package kintoneapi

import (
	"errors"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/auth"
	"github.com/youyo/kintone/internal/config"
)

func newTestAuth(t *testing.T) auth.Authenticator {
	t.Helper()
	a, err := auth.NewAPITokenAuthenticator("test-token")
	if err != nil {
		t.Fatalf("auth setup: %v", err)
	}
	return a
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("CL-1 正常生成", func(t *testing.T) {
		t.Parallel()
		c, err := New(ClientOptions{Domain: "x.cybozu.com", Authenticator: newTestAuth(t)})
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if c == nil {
			t.Fatal("nil client")
		}
		if c.baseURL != "https://x.cybozu.com" {
			t.Fatalf("baseURL=%q", c.baseURL)
		}
	})

	t.Run("CL-2 Domain 空", func(t *testing.T) {
		t.Parallel()
		_, err := New(ClientOptions{Domain: "", Authenticator: newTestAuth(t)})
		if !errors.Is(err, ErrEmptyDomain) {
			t.Fatalf("expected ErrEmptyDomain, got %v", err)
		}
	})

	t.Run("CL-3 Authenticator nil", func(t *testing.T) {
		t.Parallel()
		_, err := New(ClientOptions{Domain: "x.cybozu.com"})
		if !errors.Is(err, ErrNilAuthenticator) {
			t.Fatalf("expected ErrNilAuthenticator, got %v", err)
		}
	})

	t.Run("CL-7 デフォルト UA", func(t *testing.T) {
		t.Parallel()
		c, err := New(ClientOptions{Domain: "x", Authenticator: newTestAuth(t)})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.HasPrefix(c.userAgent, "kintone-cli/") {
			t.Fatalf("UA=%q", c.userAgent)
		}
	})

	t.Run("CL-8 スキーム混入拒否", func(t *testing.T) {
		t.Parallel()
		_, err := New(ClientOptions{Domain: "https://x.cybozu.com", Authenticator: newTestAuth(t)})
		if !errors.Is(err, ErrInvalidDomain) {
			t.Fatalf("expected ErrInvalidDomain, got %v", err)
		}
	})

	t.Run("CL-9 スラッシュ混入拒否", func(t *testing.T) {
		t.Parallel()
		_, err := New(ClientOptions{Domain: "x.cybozu.com/", Authenticator: newTestAuth(t)})
		if !errors.Is(err, ErrInvalidDomain) {
			t.Fatalf("expected ErrInvalidDomain, got %v", err)
		}
	})

	t.Run("CL-10 空白混入拒否", func(t *testing.T) {
		t.Parallel()
		_, err := New(ClientOptions{Domain: " x.cybozu.com", Authenticator: newTestAuth(t)})
		if !errors.Is(err, ErrInvalidDomain) {
			t.Fatalf("expected ErrInvalidDomain, got %v", err)
		}
	})

	t.Run("カスタム UA", func(t *testing.T) {
		t.Parallel()
		c, _ := New(ClientOptions{Domain: "x", Authenticator: newTestAuth(t), UserAgent: "my-ua/1.0"})
		if c.userAgent != "my-ua/1.0" {
			t.Fatalf("UA=%q", c.userAgent)
		}
	})
}

func TestNewFromResolved(t *testing.T) {
	t.Parallel()

	t.Run("CL-4 api-token", func(t *testing.T) {
		t.Parallel()
		c, err := NewFromResolved(&config.Resolved{
			Domain: "x.cybozu.com", Auth: config.AuthModeAPIToken, APIToken: "tok",
		})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if c == nil {
			t.Fatal("nil")
		}
	})

	t.Run("CL-5 oauth 未対応", func(t *testing.T) {
		t.Parallel()
		_, err := NewFromResolved(&config.Resolved{
			Domain: "x.cybozu.com", Auth: config.AuthModeOAuth,
		})
		if !errors.Is(err, ErrUnsupportedAuthMode) {
			t.Fatalf("expected ErrUnsupportedAuthMode, got %v", err)
		}
	})

	t.Run("CL-6 空 token", func(t *testing.T) {
		t.Parallel()
		_, err := NewFromResolved(&config.Resolved{
			Domain: "x.cybozu.com", Auth: config.AuthModeAPIToken, APIToken: "",
		})
		if !errors.Is(err, auth.ErrEmptyAPIToken) {
			t.Fatalf("expected ErrEmptyAPIToken, got %v", err)
		}
	})

	t.Run("nil Resolved", func(t *testing.T) {
		t.Parallel()
		_, err := NewFromResolved(nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("空 Auth", func(t *testing.T) {
		t.Parallel()
		_, err := NewFromResolved(&config.Resolved{Domain: "x", Auth: ""})
		if !errors.Is(err, ErrUnsupportedAuthMode) {
			t.Fatalf("expected ErrUnsupportedAuthMode, got %v", err)
		}
	})
}

// CK-1: NewFromResolvedWithAuth + AuthModeOAuth + Authenticator → Client 構築成功
func TestNewFromResolvedWithAuth(t *testing.T) {
	t.Parallel()

	t.Run("CK-1 oauth 用拡張コンストラクタ", func(t *testing.T) {
		t.Parallel()
		r := &config.Resolved{
			Domain: "x.cybozu.com",
			Auth:   config.AuthModeOAuth,
		}
		a := newTestAuth(t)
		c, err := NewFromResolvedWithAuth(r, a)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c == nil {
			t.Fatal("nil client")
		}
	})

	t.Run("CK-2 nil Resolved", func(t *testing.T) {
		t.Parallel()
		_, err := NewFromResolvedWithAuth(nil, newTestAuth(t))
		if err == nil {
			t.Fatal("expected error for nil resolved")
		}
	})

	t.Run("CK-3 nil Authenticator", func(t *testing.T) {
		t.Parallel()
		r := &config.Resolved{Domain: "x.cybozu.com", Auth: config.AuthModeOAuth}
		_, err := NewFromResolvedWithAuth(r, nil)
		if !errors.Is(err, ErrNilAuthenticator) {
			t.Fatalf("expected ErrNilAuthenticator, got %v", err)
		}
	})
}
