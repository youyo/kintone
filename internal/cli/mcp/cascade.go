package mcp

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/store"
)

// EnsureKintoneOAuthConnected は OIDC 認証済みユーザーが kintone OAuth トークンを
// まだ取得していない場合、startURL へ自動リダイレクトする middleware を返す。
//
// 以下の条件をすべて満たすとき 302 リダイレクトする:
//  1. GET または HEAD メソッド
//  2. Accept ヘッダに "text/html" を含む
//  3. idproxy.FromContext で Principal が取得できる（non-nil かつ ID 非空）
//  4. パスが "/oauth/kintone/" で始まらない（無限ループ防止）
//  5. パスが "/mcp" または "/mcp/" で始まらない（MCP JSON-RPC 保護）
//  6. tokens.Get が store.ErrNotFound を返す（kintone トークン未取得）
//
// 環境変数 KINTONE_MCP_DISABLE_OAUTH_CASCADE=1 で no-op 化（kill switch）。
// それ以外のエラーは safe default として next に委譲する。
func EnsureKintoneOAuthConnected(
	tokens store.TokenStore,
	domain string,
	startURL string,
) func(http.Handler) http.Handler {
	if os.Getenv("KINTONE_MCP_DISABLE_OAUTH_CASCADE") == "1" {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 条件 1: GET / HEAD のみ対象
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}
			// 条件 2: Accept に text/html を含む
			if !strings.Contains(r.Header.Get("Accept"), "text/html") {
				next.ServeHTTP(w, r)
				return
			}
			// 条件 3: OIDC 認証済みで Principal が取れる
			p := idproxy.FromContext(r.Context())
			if p == nil || p.ID == "" {
				next.ServeHTTP(w, r)
				return
			}
			// 条件 4: /oauth/kintone/ パスは除外（無限ループ防止）
			if strings.HasPrefix(r.URL.Path, "/oauth/kintone/") {
				next.ServeHTTP(w, r)
				return
			}
			// 条件 5: /mcp パスは除外（MCP JSON-RPC 保護）
			if r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/mcp/") {
				next.ServeHTTP(w, r)
				return
			}
			// 条件 6: kintone トークン未取得かを確認
			_, err := tokens.Get(r.Context(), domain, p.ID, store.AuthTypeOAuth)
			if errors.Is(err, store.ErrNotFound) {
				http.Redirect(w, r, startURL, http.StatusFound)
				return
			}
			// ErrNotFound 以外のエラーや err==nil は safe default で next に委譲
			next.ServeHTTP(w, r)
		})
	}
}
