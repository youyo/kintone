package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// MiddlewareFunc は http.Handler ラッパー型。idproxy.Auth.Wrap や principal middleware を渡す。
type MiddlewareFunc func(http.Handler) http.Handler

// noopMiddleware は何もしない MiddlewareFunc。
func noopMiddleware(h http.Handler) http.Handler { return h }

// RouteEntry は ExtraRoutes 経由で /mcp 以外の path に登録する追加ルートを表す。
//
// M13 で導入。`/oauth/kintone/start` / `/oauth/kintone/callback` の登録に使う。
// Handler は middleware（idproxy.Auth.Wrap 等）も適用される（全 route 一律の
// SameSite=Lax cookie 同伴を想定）。
type RouteEntry struct {
	Path    string
	Handler http.Handler
}

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
	// ExtraRoutes は /mcp 以外に登録する追加 HTTP route（M13）。
	// Path 重複は ServeHTTP 開始時に検出され error を返す。
	ExtraRoutes []RouteEntry
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

	// 追加 routes の事前検証（path 重複 / 衝突）
	if err := validateExtraRoutes(opts.ExtraRoutes); err != nil {
		return err
	}

	streamable := server.NewStreamableHTTPServer(s)
	mux := http.NewServeMux()
	mux.Handle("/mcp", streamable)
	for _, r := range opts.ExtraRoutes {
		mux.Handle(r.Path, r.Handler)
	}

	httpServer := &http.Server{
		Addr:              opts.Addr,
		Handler:           mw(mux),
		ReadHeaderTimeout: readHeader,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout は SSE 長時間接続（MCP Streamable HTTP）を維持するため 0（無制限）のまま。
		IdleTimeout: 120 * time.Second,
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

// validateExtraRoutes は ExtraRoutes に対する事前検証を行う。
//
// チェック項目:
//   - Path が空文字でないこと
//   - Handler が nil でないこと
//   - 重複 path がないこと（自身どうし + /mcp との衝突）
//
// http.ServeMux.Handle は重複登録で panic するため、事前に error として弾く。
func validateExtraRoutes(routes []RouteEntry) error {
	if len(routes) == 0 {
		return nil
	}
	seen := map[string]bool{"/mcp": true}
	for i, r := range routes {
		if r.Path == "" {
			return errors.New("server: ExtraRoutes[" + itoa(i) + "].Path is empty")
		}
		if r.Handler == nil {
			return errors.New("server: ExtraRoutes[" + r.Path + "].Handler is nil")
		}
		if seen[r.Path] {
			return errors.New("server: ExtraRoutes path " + r.Path + " duplicates /mcp or another entry")
		}
		seen[r.Path] = true
	}
	return nil
}

// itoa は 0-99 の小さい index を文字列化する軽量ヘルパ（fmt 依存削減）。
func itoa(i int) string {
	if i < 10 {
		return string([]byte{'0' + byte(i)})
	}
	return string([]byte{'0' + byte(i/10), '0' + byte(i%10)})
}

// RequestLogMiddleware は全 HTTP リクエストの受信・完了を Info レベルで記録する。
//
// 設計原則:
//   - idproxy ミドルウェアの最外側（前段）に配置する
//   - status code / has_bearer を記録することで「idproxy が弾いたか」「アプリ層か」を区別できる
//   - Lambda + CloudWatch 環境での認証失敗診断を主目的とする（Issue #10）
func RequestLogMiddleware(logger *slog.Logger) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			logger.Info("mcp request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"has_bearer", r.Header.Get("Authorization") != "",
			)
		})
	}
}

// statusRecorder は http.ResponseWriter をラップして WriteHeader で設定されたステータスコードを記録する。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
