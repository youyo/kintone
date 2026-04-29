package kintoneapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth"
)

// testFixture は httptest 経由のテスト用クライアントを生成する。
type testFixture struct {
	server  *httptest.Server
	client  *Client
	sleeps  []time.Duration
	nowFunc func() time.Time
}

func newFixture(t *testing.T, handler http.HandlerFunc, retryOverrides ...RetryPolicy) *testFixture {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	a, _ := auth.NewAPITokenAuthenticator("test-token")

	policy := DefaultRetryPolicy
	if len(retryOverrides) > 0 {
		policy = retryOverrides[0]
	}

	fx := &testFixture{server: srv}
	now := time.Date(2026, 4, 29, 9, 0, 0, 0, time.UTC)
	fx.nowFunc = func() time.Time { return now }
	fx.client = &Client{
		baseURL:    srv.URL,
		httpClient: srv.Client(),
		auth:       a,
		userAgent:  "test-ua/1.0",
		retry:      policy,
		now:        fx.nowFunc,
		sleep: func(d time.Duration) {
			fx.sleeps = append(fx.sleeps, d)
		},
	}
	return fx
}

func TestTransport_Success(t *testing.T) {
	t.Parallel()

	t.Run("TR-1 200 デコード成功", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"appId":"42","code":"foo","name":"テスト"}`)
		})
		var resp GetAppResponse
		err := fx.client.doJSON(context.Background(), http.MethodGet, "/k/v1/app.json", nil, &resp)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if resp.AppID != "42" || resp.Name != "テスト" {
			t.Fatalf("unexpected resp: %+v", resp)
		}
	})

	t.Run("TR-2 認証ヘッダ付与", func(t *testing.T) {
		t.Parallel()
		var headerSeen string
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			headerSeen = r.Header.Get("X-Cybozu-API-Token")
			_, _ = io.WriteString(w, `{}`)
		})
		_ = fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		if headerSeen != "test-token" {
			t.Fatalf("X-Cybozu-API-Token=%q", headerSeen)
		}
	})

	t.Run("TR-3 UA 付与", func(t *testing.T) {
		t.Parallel()
		var ua string
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			ua = r.Header.Get("User-Agent")
			_, _ = io.WriteString(w, `{}`)
		})
		_ = fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		if ua != "test-ua/1.0" {
			t.Fatalf("UA=%q", ua)
		}
	})

	t.Run("Accept ヘッダ", func(t *testing.T) {
		t.Parallel()
		var accept string
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			accept = r.Header.Get("Accept")
			_, _ = io.WriteString(w, `{}`)
		})
		_ = fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		if accept != "application/json" {
			t.Fatalf("Accept=%q", accept)
		}
	})
}

func TestTransport_APIErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		status   int
		body     string
		wantCat  ErrorCategory
		wantCode string
	}{
		{"TR-4 401", 401, `{"code":"CB_AU01","id":"x","message":"認証エラー"}`, CategoryUnauthorized, "CB_AU01"},
		{"TR-5 403", 403, `{"code":"GAIA_NO01","message":"権限なし"}`, CategoryForbidden, "GAIA_NO01"},
		{"TR-6 404", 404, `{"code":"GAIA_AP01","message":"アプリ不在"}`, CategoryNotFound, "GAIA_AP01"},
		{"TR-7 500 空 body", 500, ``, CategoryServerError, ""},
		{"TR-16 不正 JSON", 400, `not json`, CategoryClientError, ""},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// 5xx をリトライ対象から外して即時失敗にする
			policy := RetryPolicy{MaxAttempts: 1, RetryOn: []int{}}
			fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			}, policy)
			err := fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %v", err)
			}
			if apiErr.HTTPStatus != c.status {
				t.Fatalf("status=%d", apiErr.HTTPStatus)
			}
			if apiErr.Category != c.wantCat {
				t.Fatalf("cat=%v want %v", apiErr.Category, c.wantCat)
			}
			if apiErr.Code != c.wantCode {
				t.Fatalf("code=%q want %q", apiErr.Code, c.wantCode)
			}
		})
	}
}

func TestTransport_Retry(t *testing.T) {
	t.Parallel()

	t.Run("TR-8 429 → 200 (Retry-After=1s)", func(t *testing.T) {
		t.Parallel()
		var calls int32
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(429)
				return
			}
			_, _ = io.WriteString(w, `{"appId":"42"}`)
		})
		var resp GetAppResponse
		err := fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, &resp)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if resp.AppID != "42" {
			t.Fatalf("appID=%q", resp.AppID)
		}
		if len(fx.sleeps) != 1 || fx.sleeps[0] != time.Second {
			t.Fatalf("sleeps=%v", fx.sleeps)
		}
	})

	t.Run("TR-9 429 連続 最終失敗", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
		}, RetryPolicy{MaxAttempts: 3, BaseBackoff: 100 * time.Millisecond, MaxBackoff: 5 * time.Second, RetryOn: []int{429}})
		err := fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected APIError, got %v", err)
		}
		if apiErr.Category != CategoryRateLimited {
			t.Fatalf("cat=%v", apiErr.Category)
		}
		// 3 attempts → 2 sleep calls
		if len(fx.sleeps) != 2 {
			t.Fatalf("sleeps=%v", fx.sleeps)
		}
	})

	t.Run("TR-10 503 リトライ → 200", func(t *testing.T) {
		t.Parallel()
		var calls int32
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&calls, 1) == 1 {
				w.WriteHeader(503)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		})
		err := fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})

	t.Run("TR-11 Retry-After なし 指数バックオフ", func(t *testing.T) {
		t.Parallel()
		var calls int32
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&calls, 1) == 1 {
				w.WriteHeader(429)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		}, RetryPolicy{MaxAttempts: 3, BaseBackoff: 200 * time.Millisecond, MaxBackoff: 5 * time.Second, RetryOn: []int{429}})
		_ = fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		if len(fx.sleeps) != 1 || fx.sleeps[0] != 200*time.Millisecond {
			t.Fatalf("sleeps=%v", fx.sleeps)
		}
	})

	t.Run("TR-12 Retry-After HTTP-date", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, 4, 29, 9, 0, 0, 0, time.UTC)
		future := now.Add(2 * time.Second).Format(http.TimeFormat)
		var calls int32
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			if atomic.AddInt32(&calls, 1) == 1 {
				w.Header().Set("Retry-After", future)
				w.WriteHeader(429)
				return
			}
			_, _ = io.WriteString(w, `{}`)
		})
		fx.client.now = func() time.Time { return now }
		_ = fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		if len(fx.sleeps) != 1 {
			t.Fatalf("sleeps=%v", fx.sleeps)
		}
		// HTTP-date は秒精度
		if fx.sleeps[0] < time.Second || fx.sleeps[0] > 3*time.Second {
			t.Fatalf("sleep=%v", fx.sleeps[0])
		}
	})

	t.Run("TR-13 ctx cancel", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond)
			_, _ = io.WriteString(w, `{}`)
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := fx.client.doJSON(ctx, http.MethodGet, "/", nil, nil)
		if err == nil {
			t.Fatal("expected ctx error")
		}
	})

	t.Run("TR-15 RawBody 4KB 切り詰め", func(t *testing.T) {
		t.Parallel()
		big := strings.Repeat("a", 5000)
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, big)
		}, RetryPolicy{MaxAttempts: 1, RetryOn: []int{}})
		err := fx.client.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected APIError, got %v", err)
		}
		if len(apiErr.RawBody) != 4096 {
			t.Fatalf("rawBody len=%d", len(apiErr.RawBody))
		}
	})
}

// roundTripperFunc は RoundTripper の関数アダプタ。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// countingBody は Close 呼び出しを計測する io.ReadCloser。
type countingBody struct {
	io.Reader
	closed *int32
}

func (b *countingBody) Close() error {
	atomic.AddInt32(b.closed, 1)
	return nil
}

func TestTransport_BodyClosed(t *testing.T) {
	t.Parallel()

	t.Run("TR-17 リトライ前に body Close", func(t *testing.T) {
		t.Parallel()
		var closed int32
		var calls int32
		rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				h := http.Header{}
				h.Set("Retry-After", "1")
				return &http.Response{
					StatusCode: 429,
					Header:     h,
					Body:       &countingBody{Reader: strings.NewReader(""), closed: &closed},
				}, nil
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{},
				Body:       &countingBody{Reader: strings.NewReader(`{}`), closed: &closed},
			}, nil
		})
		a, _ := auth.NewAPITokenAuthenticator("t")
		c := &Client{
			baseURL:    "http://test",
			httpClient: &http.Client{Transport: rt},
			auth:       a,
			userAgent:  "x",
			retry:      RetryPolicy{MaxAttempts: 2, BaseBackoff: time.Millisecond, MaxBackoff: time.Second, RetryOn: []int{429}},
			now:        time.Now,
			sleep:      func(time.Duration) {},
		}
		err := c.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		if err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if got := atomic.LoadInt32(&closed); got != 2 {
			t.Fatalf("body Close called %d times, want 2", got)
		}
	})
}

func TestTransport_NetError(t *testing.T) {
	t.Parallel()

	t.Run("net timeout retry then success", func(t *testing.T) {
		t.Parallel()
		var calls int32
		rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				return nil, &timeoutNetErr{}
			}
			body, _ := json.Marshal(map[string]string{"appId": "42"})
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		})
		a, _ := auth.NewAPITokenAuthenticator("t")
		c := &Client{
			baseURL:    "http://test",
			httpClient: &http.Client{Transport: rt},
			auth:       a,
			userAgent:  "x",
			retry:      RetryPolicy{MaxAttempts: 2, BaseBackoff: time.Millisecond, MaxBackoff: time.Second, RetryOn: []int{429}},
			now:        time.Now,
			sleep:      func(time.Duration) {},
		}
		var resp GetAppResponse
		if err := c.doJSON(context.Background(), http.MethodGet, "/", nil, &resp); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if resp.AppID != "42" {
			t.Fatalf("appID=%q", resp.AppID)
		}
	})

	t.Run("net non-timeout error は即時失敗", func(t *testing.T) {
		t.Parallel()
		rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		})
		a, _ := auth.NewAPITokenAuthenticator("t")
		c := &Client{
			baseURL: "http://test", httpClient: &http.Client{Transport: rt}, auth: a,
			userAgent: "x", retry: DefaultRetryPolicy, now: time.Now, sleep: func(time.Duration) {},
		}
		err := c.doJSON(context.Background(), http.MethodGet, "/", nil, nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

type timeoutNetErr struct{}

func (e *timeoutNetErr) Error() string   { return "timeout" }
func (e *timeoutNetErr) Timeout() bool   { return true }
func (e *timeoutNetErr) Temporary() bool { return true }

func TestShouldRetry(t *testing.T) {
	t.Parallel()
	if !shouldRetry(429, nil) {
		t.Fatal("default should retry 429")
	}
	if shouldRetry(500, []int{429}) {
		t.Fatal("explicit list should not retry 500")
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	// Retry-After 優先
	if got := backoff(1, 2*time.Second, 100*time.Millisecond, 5*time.Second); got != 2*time.Second {
		t.Fatalf("got %v", got)
	}
	// Retry-After が max を超える
	if got := backoff(1, 10*time.Second, 100*time.Millisecond, 5*time.Second); got != 5*time.Second {
		t.Fatalf("got %v", got)
	}
	// 指数
	if got := backoff(1, 0, 100*time.Millisecond, 5*time.Second); got != 100*time.Millisecond {
		t.Fatalf("attempt 1: %v", got)
	}
	if got := backoff(2, 0, 100*time.Millisecond, 5*time.Second); got != 200*time.Millisecond {
		t.Fatalf("attempt 2: %v", got)
	}
	if got := backoff(10, 0, 100*time.Millisecond, 500*time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("attempt 10: %v", got)
	}
}
