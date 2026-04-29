package kintoneapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		code   string
		want   ErrorCategory
	}{
		{"ER-1 401", 401, "CB_AU01", CategoryUnauthorized},
		{"ER-2 403", 403, "GAIA_NO01", CategoryForbidden},
		{"ER-3 404", 404, "GAIA_AP01", CategoryNotFound},
		{"ER-4 429", 429, "", CategoryRateLimited},
		{"ER-5 503", 503, "", CategoryServerError},
		{"ER-6 422 validation", 422, "CB_VA01", CategoryValidation},
		{"ER-7 400 unknown", 400, "", CategoryClientError},
		{"500", 500, "", CategoryServerError},
		{"200 → unknown", 200, "", CategoryUnknown},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := classify(c.status, c.code)
			if got != c.want {
				t.Fatalf("classify(%d,%q)=%v want %v", c.status, c.code, got, c.want)
			}
		})
	}
}

func TestAPIError_Error(t *testing.T) {
	t.Parallel()

	e := &APIError{
		HTTPStatus: 401,
		Code:       "CB_AU01",
		Message:    "認証エラー",
	}
	got := e.Error()
	if !strings.Contains(got, "401") || !strings.Contains(got, "CB_AU01") || !strings.Contains(got, "認証エラー") {
		t.Fatalf("ER-8: unexpected error message: %q", got)
	}

	// kintone コードがない場合
	e2 := &APIError{HTTPStatus: 500, Message: ""}
	got2 := e2.Error()
	if !strings.Contains(got2, "500") {
		t.Fatalf("expected 500 in message, got %q", got2)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 29, 9, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }

	t.Run("ER-9 数値", func(t *testing.T) {
		t.Parallel()
		h := http.Header{}
		h.Set("Retry-After", "5")
		got := parseRetryAfter(h, nowFn)
		if got != 5*time.Second {
			t.Fatalf("expected 5s, got %v", got)
		}
	})

	t.Run("ER-10 HTTP-date 未来", func(t *testing.T) {
		t.Parallel()
		future := now.Add(3 * time.Second)
		h := http.Header{}
		h.Set("Retry-After", future.Format(http.TimeFormat))
		got := parseRetryAfter(h, nowFn)
		// HTTP-date は秒精度
		if got < 2*time.Second || got > 4*time.Second {
			t.Fatalf("expected ~3s, got %v", got)
		}
	})

	t.Run("ER-10 HTTP-date 過去 → 0", func(t *testing.T) {
		t.Parallel()
		past := now.Add(-10 * time.Second)
		h := http.Header{}
		h.Set("Retry-After", past.Format(http.TimeFormat))
		got := parseRetryAfter(h, nowFn)
		if got != 0 {
			t.Fatalf("expected 0 for past date, got %v", got)
		}
	})

	t.Run("ER-11 空", func(t *testing.T) {
		t.Parallel()
		h := http.Header{}
		got := parseRetryAfter(h, nowFn)
		if got != 0 {
			t.Fatalf("expected 0, got %v", got)
		}
	})

	t.Run("ER-11 不正", func(t *testing.T) {
		t.Parallel()
		h := http.Header{}
		h.Set("Retry-After", "abc")
		got := parseRetryAfter(h, nowFn)
		if got != 0 {
			t.Fatalf("expected 0 for invalid, got %v", got)
		}
	})
}
