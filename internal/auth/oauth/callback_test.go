package oauth_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/auth/oauth"
)

// CB-1: callback ハンドラに正常 code/state → code を取得できること。
func TestCallbackServer_Success(t *testing.T) {
	t.Parallel()
	state := "test-state-value"
	srv, port, err := oauth.NewCallbackServer("127.0.0.1:0", state)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resultCh := make(chan oauth.CallbackResult, 1)
	go srv.Serve(ctx, resultCh)

	// callback URL にリクエスト
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?code=my-auth-code&state=%s", port, state)
	resp, err := http.Get(callbackURL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()

	select {
	case result := <-resultCh:
		if result.Err != nil {
			t.Fatalf("unexpected error: %v", result.Err)
		}
		if result.Code != "my-auth-code" {
			t.Errorf("code: got %q, want %q", result.Code, "my-auth-code")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for callback result")
	}
}

// CB-2: state 不一致 → ErrStateMismatch
func TestCallbackServer_StateMismatch(t *testing.T) {
	t.Parallel()
	srv, port, err := oauth.NewCallbackServer("127.0.0.1:0", "expected-state")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resultCh := make(chan oauth.CallbackResult, 1)
	go srv.Serve(ctx, resultCh)

	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?code=code&state=wrong-state", port)
	resp, err := http.Get(callbackURL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()

	select {
	case result := <-resultCh:
		if result.Err == nil {
			t.Fatal("expected ErrStateMismatch, got nil")
		}
		if result.Err.Error() != oauth.ErrStateMismatch.Error() {
			t.Errorf("error: got %v, want %v", result.Err, oauth.ErrStateMismatch)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for callback result")
	}
}

// CB-3: code パラメータ欠落 → ErrAuthorizationCodeMissing
func TestCallbackServer_MissingCode(t *testing.T) {
	t.Parallel()
	state := "test-state"
	srv, port, err := oauth.NewCallbackServer("127.0.0.1:0", state)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resultCh := make(chan oauth.CallbackResult, 1)
	go srv.Serve(ctx, resultCh)

	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?state=%s", port, state)
	resp, err := http.Get(callbackURL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	defer resp.Body.Close()

	select {
	case result := <-resultCh:
		if result.Err == nil {
			t.Fatal("expected ErrAuthorizationCodeMissing, got nil")
		}
		if result.Err.Error() != oauth.ErrAuthorizationCodeMissing.Error() {
			t.Errorf("error: got %v, want %v", result.Err, oauth.ErrAuthorizationCodeMissing)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for callback result")
	}
}

// CB-4: 二重 callback → 1 回目のみ受理、2 回目は 400 (sync.Once)
func TestCallbackServer_DoubleCallback(t *testing.T) {
	t.Parallel()
	state := "test-state"
	srv, port, err := oauth.NewCallbackServer("127.0.0.1:0", state)
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resultCh := make(chan oauth.CallbackResult, 1)
	go srv.Serve(ctx, resultCh)

	// 1回目
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?code=code&state=%s", port, state)
	resp1, err := http.Get(callbackURL) //nolint:noctx
	if err != nil {
		t.Fatalf("GET 1st callback: %v", err)
	}
	defer resp1.Body.Close()

	// 結果を受け取る
	select {
	case <-resultCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for 1st callback result")
	}

	// 2回目（短いタイムアウト）
	client := &http.Client{Timeout: 2 * time.Second}
	resp2, err := client.Get(callbackURL)
	if err != nil {
		// 接続が切れていることもある（サーバが停止済みの場合）
		return
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("2nd callback: expected 400, got %d", resp2.StatusCode)
	}
}

// CB-5: context timeout → サーバが停止すること
func TestCallbackServer_Timeout(t *testing.T) {
	t.Parallel()
	srv, _, err := oauth.NewCallbackServer("127.0.0.1:0", "state")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	resultCh := make(chan oauth.CallbackResult, 1)
	srv.Serve(ctx, resultCh) // context が切れるまで待つ

	// context キャンセルで Serve が戻ること（この assert はスキップ）
	// テスト自体がタイムアウトしなければ OK
}

// CB-6: listen ポート競合 → bind error
func TestCallbackServer_PortConflict(t *testing.T) {
	t.Parallel()
	// まず空きポートを取得して占有する
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	// 同じポートに NewCallbackServer を試みる
	_, _, err = oauth.NewCallbackServer(fmt.Sprintf("127.0.0.1:%d", port), "state")
	if err == nil {
		t.Error("expected bind error, got nil")
	}
}

// CB-7: shutdown 後の listener cleanup（サーバ停止後に同ポートで再 listen 可能）
func TestCallbackServer_CleanupAfterShutdown(t *testing.T) {
	t.Parallel()
	srv, port, err := oauth.NewCallbackServer("127.0.0.1:0", "state")
	if err != nil {
		t.Fatalf("NewCallbackServer: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	resultCh := make(chan oauth.CallbackResult, 1)
	srv.Serve(ctx, resultCh)
	srv.Close()

	// Close 後に同じポートで再 listen できることを確認
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Errorf("re-listen after close: %v", err)
		return
	}
	_ = ln.Close()
}
