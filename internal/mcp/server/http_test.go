package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// freePort は OS にポートを割り当てさせて即解放、addr を返す。
// 競合の可能性は残るが unit test 範囲では十分。
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func newTestMCP() *server.MCPServer {
	return server.NewMCPServer("kintone-test", "0.0.0", server.WithToolCapabilities(false))
}

func TestServeHTTP_RejectsEmptyAddr(t *testing.T) {
	t.Parallel()
	err := ServeHTTP(context.Background(), newTestMCP(), HTTPServeOptions{Addr: ""})
	if err == nil || !strings.Contains(err.Error(), "Addr") {
		t.Fatalf("expected Addr error, got %v", err)
	}
}

func TestServeHTTP_RejectsNilServer(t *testing.T) {
	t.Parallel()
	err := ServeHTTP(context.Background(), nil, HTTPServeOptions{Addr: ":0"})
	if err == nil || !strings.Contains(err.Error(), "MCPServer") {
		t.Fatalf("expected MCPServer error, got %v", err)
	}
}

func TestServeHTTP_GracefulShutdownOnContextCancel(t *testing.T) {
	t.Parallel()
	addr := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeHTTP(ctx, newTestMCP(), HTTPServeOptions{Addr: addr, ShutdownTimeout: 2 * time.Second})
	}()
	// サーバーが listen 完了するのを待つ（軽い retry）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ServeHTTP returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHTTP did not return after context cancel")
	}
}

func TestServeHTTP_MiddlewareApplied(t *testing.T) {
	t.Parallel()
	addr := freePort(t)
	called := false
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			next.ServeHTTP(w, r)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeHTTP(ctx, newTestMCP(), HTTPServeOptions{Addr: addr, Middleware: mw})
	}()
	// listen 待ち
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// /mcp に POST（無効ペイロードでも middleware を経由するはず）
	resp, err := http.Post("http://"+addr+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	_ = resp.Body.Close()
	cancel()
	<-done
	if !called {
		t.Fatal("middleware was not invoked")
	}
}

// 401 ミドルウェアテスト: middleware が認証拒否を返したら upstream に届かないことを確認
func TestServeHTTP_Middleware_Rejects(t *testing.T) {
	t.Parallel()
	addr := freePort(t)
	hits := 0
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits++
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			// next は呼ばない
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeHTTP(ctx, newTestMCP(), HTTPServeOptions{Addr: addr, Middleware: mw})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := http.Post("http://"+addr+"/mcp", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
	cancel()
	<-done
}

// M13: ExtraRoutes の正常動作
func TestServeHTTP_AdditionalRoutes(t *testing.T) {
	t.Parallel()
	addr := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	hit := make(chan struct{}, 1)
	extra := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit <- struct{}{}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	done := make(chan error, 1)
	go func() {
		done <- ServeHTTP(ctx, newTestMCP(), HTTPServeOptions{
			Addr: addr,
			ExtraRoutes: []RouteEntry{
				{Path: "/oauth/kintone/start", Handler: extra},
			},
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := http.Get("http://" + addr + "/oauth/kintone/start")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	select {
	case <-hit:
	case <-time.After(time.Second):
		t.Fatal("extra handler not invoked")
	}
	cancel()
	<-done
}

// M13: ExtraRoutes と /mcp の重複 → error
func TestServeHTTP_AdditionalRoutes_DuplicatePathRejected(t *testing.T) {
	t.Parallel()
	err := ServeHTTP(context.Background(), newTestMCP(), HTTPServeOptions{
		Addr: ":0",
		ExtraRoutes: []RouteEntry{
			{Path: "/mcp", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate path error, got %v", err)
	}
}

// M13: ExtraRoutes 同士の重複 → error
func TestServeHTTP_AdditionalRoutes_SelfDuplicateRejected(t *testing.T) {
	t.Parallel()
	dummy := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	err := ServeHTTP(context.Background(), newTestMCP(), HTTPServeOptions{
		Addr: ":0",
		ExtraRoutes: []RouteEntry{
			{Path: "/x", Handler: dummy},
			{Path: "/x", Handler: dummy},
		},
	})
	if err == nil {
		t.Errorf("expected duplicate path error")
	}
}

// M13: ExtraRoutes に nil handler を渡すと error
func TestServeHTTP_AdditionalRoutes_NilHandlerRejected(t *testing.T) {
	t.Parallel()
	err := ServeHTTP(context.Background(), newTestMCP(), HTTPServeOptions{
		Addr: ":0",
		ExtraRoutes: []RouteEntry{
			{Path: "/x", Handler: nil},
		},
	})
	if err == nil {
		t.Errorf("expected nil handler error")
	}
}

// M13: ExtraRoutes にも middleware が適用される
func TestServeHTTP_AdditionalRoutes_MiddlewareApplied(t *testing.T) {
	t.Parallel()
	addr := freePort(t)
	called := 0
	mw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called++
			next.ServeHTTP(w, r)
		})
	}
	extra := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeHTTP(ctx, newTestMCP(), HTTPServeOptions{
			Addr:       addr,
			Middleware: mw,
			ExtraRoutes: []RouteEntry{
				{Path: "/oauth/kintone/start", Handler: extra},
			},
		})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := http.Get("http://" + addr + "/oauth/kintone/start")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	cancel()
	<-done
	if called == 0 {
		t.Errorf("middleware was not applied to extra route")
	}
}

// noopMiddleware が露出している重複定義に対する sanity check
func TestServeHTTP_NoopMiddleware(t *testing.T) {
	t.Parallel()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	rec := httptest.NewRecorder()
	noopMiddleware(h).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("noop did not pass through: %d", rec.Code)
	}
}

// TestRequestLogMiddleware_LogsInfoOnRequest は RequestLogMiddleware がリクエストを
// Info レベルで記録し、下流ハンドラーにリクエストを正しく転送することを確認する。
func TestRequestLogMiddleware_LogsInfoOnRequest(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := RequestLogMiddleware(logger)
	h := mw(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "mcp request") {
		t.Errorf("log output should contain 'mcp request', got: %s", out)
	}
	if !strings.Contains(out, "path=/mcp") {
		t.Errorf("log output should contain path=/mcp, got: %s", out)
	}
	if !strings.Contains(out, "status=200") {
		t.Errorf("log output should contain status=200, got: %s", out)
	}
	if !strings.Contains(out, "method=POST") {
		t.Errorf("log output should contain method=POST, got: %s", out)
	}
	if !strings.Contains(out, "has_bearer=false") {
		t.Errorf("log output should contain has_bearer=false, got: %s", out)
	}
}

// TestRequestLogMiddleware_RecordsStatus401 はミドルウェアが 401 を返した場合に
// status=401 が記録されることを確認する。
func TestRequestLogMiddleware_RecordsStatus401(t *testing.T) {
	t.Parallel()
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	mw := RequestLogMiddleware(logger)
	h := mw(inner)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer some-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d want 401", rec.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "status=401") {
		t.Errorf("log output should contain status=401, got: %s", out)
	}
	if !strings.Contains(out, "has_bearer=true") {
		t.Errorf("log output should contain has_bearer=true, got: %s", out)
	}
}
