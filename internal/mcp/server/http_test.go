package server

import (
	"context"
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
