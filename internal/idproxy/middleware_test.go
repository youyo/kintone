package idproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "github.com/youyo/idproxy"
)

// withUpstreamUser は upstream（idproxy）の User を context に詰める internal helper を再現できないため、
// 公開 API の挙動を確認する形でテストする。
//
// idproxy は newContextWithUser を unexported にしているため、ここでは
// 「idproxy.User が context に注入されていない」状態 = next で FromContext nil を確認する。
// User 注入経路は middleware.PrincipalMiddleware の責務外（idproxy.Wrap が担当）であり、
// 結合テストで cover する。

func TestPrincipalMiddleware_PassThroughNoUser(t *testing.T) {
	t.Parallel()
	var seenPrincipal *Principal
	mw := PrincipalMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPrincipal = FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/x", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", rec.Code)
	}
	if seenPrincipal != nil {
		t.Fatalf("principal: got %v, want nil (no upstream user)", seenPrincipal)
	}
}

// upstream.UserFromContext が User を返す経路は idproxy 内部 API で組まないと再現不能。
// ここでは PrincipalMiddleware が idproxy.User を変換するロジックを通すため、
// 直接 upstream.User を context へ詰める方法（unexported keyのため不可能）の代わりに
// idproxy.Auth を使った結合テストで担保することを TestPrincipalMiddleware_NoUser コメントで明示する。

// TestPrincipalMiddleware_NextReceivesRequest は next.ServeHTTP に request が渡ることを確認する。
func TestPrincipalMiddleware_NextReceivesRequest(t *testing.T) {
	t.Parallel()
	called := false
	mw := PrincipalMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest("GET", "/y", nil).WithContext(context.Background())
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	if !called {
		t.Fatalf("next handler not called")
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status: got %d", rec.Code)
	}
}

// 確認: upstream package の関数が import されていること（dead import 防止）。
var _ = upstream.UserFromContext
