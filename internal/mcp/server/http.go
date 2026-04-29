package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// MiddlewareFunc は http.Handler ラッパー型。idproxy.Auth.Wrap や principal middleware を渡す。
type MiddlewareFunc func(http.Handler) http.Handler

// noopMiddleware は何もしない MiddlewareFunc。
func noopMiddleware(h http.Handler) http.Handler { return h }

// HTTPServeOptions は ServeHTTP の入力。
type HTTPServeOptions struct {
	// Addr はリッスン address（host:port）。
	Addr string
	// Middleware は MCP transport の前段に挟むミドルウェア。nil で no-op。
	// 通常は idproxy.Auth.Wrap → idproxy.PrincipalMiddleware を合成して渡す。
	Middleware MiddlewareFunc
	// ReadHeaderTimeout はサーバの ReadHeaderTimeout（slowloris 対策）。
	// 0 のとき 10 秒を使用。
	ReadHeaderTimeout time.Duration
	// ShutdownTimeout は graceful shutdown の制限時間。0 のとき 5 秒。
	ShutdownTimeout time.Duration
}

// ServeHTTP は MCP server を Streamable HTTP で起動する。
//
//   - StreamableHTTP transport で /mcp を提供する
//   - ctx.Done() でリッスンを止める（graceful close）
//   - 既存 stdio 経路（ServeStdio）には一切影響しない
//
// 設計判断:
//   - SSE transport は M10 では提供しない（仕様 2025-03-26 で StreamableHTTP が推奨）
//   - middleware は composable に外から注入する（テスト容易性 + idproxy 依存の上位層配置）
func ServeHTTP(ctx context.Context, s *MCPServer, opts HTTPServeOptions) error {
	if s == nil {
		return errors.New("server: nil MCPServer")
	}
	if opts.Addr == "" {
		return errors.New("server: empty Addr")
	}
	mw := opts.Middleware
	if mw == nil {
		mw = noopMiddleware
	}
	readHeader := opts.ReadHeaderTimeout
	if readHeader == 0 {
		readHeader = 10 * time.Second
	}
	shutdownTimeout := opts.ShutdownTimeout
	if shutdownTimeout == 0 {
		shutdownTimeout = 5 * time.Second
	}

	streamable := server.NewStreamableHTTPServer(s)
	mux := http.NewServeMux()
	mux.Handle("/mcp", streamable)

	httpServer := &http.Server{
		Addr:              opts.Addr,
		Handler:           mw(mux),
		ReadHeaderTimeout: readHeader,
	}

	errCh := make(chan error, 1)
	go func() {
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			errCh <- nil
			return
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		// 終了後 errCh から最終エラー（あれば）を引き取る
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}

// MCPServer は server.NewMCPServer の戻り値型エイリアス（コール側が import を増やさないため）。
type MCPServer = server.MCPServer
