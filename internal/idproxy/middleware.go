package idproxy

import (
	"net/http"

	upstream "github.com/youyo/idproxy"
)

// PrincipalMiddleware は idproxy.Wrap の後段に挿入し、
// idproxy.UserFromContext で取得した認証済みユーザーを kintone Principal に変換し
// context へ注入する http.Handler ラッパーを返す。
//
// idproxy.User が nil の場合（未認証 / Bearer 無し）は context への注入を行わない。
func PrincipalMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if p := principalFromUser(upstream.UserFromContext(ctx)); p != nil {
			r = r.WithContext(WithPrincipal(ctx, p))
		}
		next.ServeHTTP(w, r)
	})
}
