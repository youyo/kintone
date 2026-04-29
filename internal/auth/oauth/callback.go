package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// CallbackResult はコールバックサーバの受信結果。
type CallbackResult struct {
	Code string // authorization code
	Err  error  // エラー（state 不一致など）
}

// CallbackServer は loopback HTTP サーバ。
// /callback パスのみを受け付ける（CSRF state 検証 + authorization code 抽出）。
type CallbackServer struct {
	server    *http.Server
	listener  net.Listener
	port      int
	expectState string
	once      sync.Once
	resultCh  chan<- CallbackResult
}

// NewCallbackServer は指定アドレス（"127.0.0.1:<port>" または "127.0.0.1:0"）で
// TCP 接続を待ち受ける CallbackServer を返す。
//
// addr に "127.0.0.1:0" を渡すと空きポートを自動選択する。
// バインド失敗（ポート衝突など）は即座に error を返す。
func NewCallbackServer(addr, expectedState string) (*CallbackServer, int, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, 0, fmt.Errorf("oauth: callback: listen %s: %w", addr, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	cs := &CallbackServer{
		listener:    ln,
		port:        port,
		expectState: expectedState,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", cs.handleCallback)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	cs.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return cs, port, nil
}

// Serve は listener で接続を受け付け、ctx がキャンセルされるか
// callback 受信完了まで待機する。
//
// 結果は resultCh に 1 度だけ送信する（送信後 cs.server.Shutdown を呼ぶ）。
func (cs *CallbackServer) Serve(ctx context.Context, resultCh chan<- CallbackResult) {
	cs.resultCh = resultCh

	serveErrCh := make(chan error, 1)
	go func() {
		err := cs.server.Serve(cs.listener)
		if err != nil && err != http.ErrServerClosed {
			serveErrCh <- err
		} else {
			serveErrCh <- nil
		}
	}()

	select {
	case <-ctx.Done():
		// context キャンセル → shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cs.server.Shutdown(shutdownCtx)
		// timeout の場合は resultCh に通知していなければ送信
		cs.once.Do(func() {
			resultCh <- CallbackResult{Err: ErrCallbackTimeout}
		})
	case <-serveErrCh:
		// サーバが何らかの理由で終了（通常は Shutdown 後）
	}
}

// Close は HTTP サーバと listener を強制終了する。
func (cs *CallbackServer) Close() {
	if cs.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cs.server.Shutdown(shutdownCtx)
	}
}

// handleCallback は /callback リクエストを処理する。
// sync.Once で 1 回のみ受理し、2 回目以降は 400 を返す。
func (cs *CallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	var processed bool
	cs.once.Do(func() {
		processed = true
		result := cs.validateCallback(r)

		// HTML で「ログイン完了」を返す
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if result.Err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h1>認証エラー</h1><p>` +
				result.Err.Error() + `</p><p>ターミナルに戻ってください。</p></body></html>`))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h1>認証完了</h1>` +
				`<p>ログインが完了しました。このブラウザタブを閉じて CLI に戻ってください。</p></body></html>`))
		}

		if cs.resultCh != nil {
			cs.resultCh <- result
		}

		// 成功 / 失敗に関わらずサーバを停止する
		go func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = cs.server.Shutdown(shutdownCtx)
		}()
	})

	if !processed {
		// 2回目以降のリクエスト
		http.Error(w, "already processed", http.StatusBadRequest)
	}
}

// validateCallback は callback パラメータを検証し CallbackResult を返す。
func (cs *CallbackServer) validateCallback(r *http.Request) CallbackResult {
	q := r.URL.Query()

	// state 検証（CSRF 対策）
	gotState := q.Get("state")
	if gotState != cs.expectState {
		return CallbackResult{Err: ErrStateMismatch}
	}

	// authorization code 確認
	code := q.Get("code")
	if code == "" {
		return CallbackResult{Err: ErrAuthorizationCodeMissing}
	}

	return CallbackResult{Code: code}
}
