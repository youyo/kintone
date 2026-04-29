package idproxy

import (
	"context"

	upstream "github.com/youyo/idproxy"
)

// Principal は kintone 側で扱う認証済みユーザーの最小情報。
//
// idproxy.User から派生する正規化形であり、principal_id は仕様
// （docs/specs/kintone_spec.md 「Principal」）に従い "issuer:subject" を持つ。
type Principal struct {
	// ID は principal_id（"issuer:subject"）。TokenStore のキーに使う。
	ID string
	// Issuer は OIDC issuer URL。
	Issuer string
	// Subject は OIDC sub クレーム。
	Subject string
	// Email はユーザーのメールアドレス。
	Email string
	// Name は表示名（任意）。
	Name string
}

// principalCtxKey は Context への注入キー。型衝突を避けるため unexported。
type principalCtxKey struct{}

// WithPrincipal は Principal を含む新しい Context を返す。p が nil でもそのまま注入する。
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalCtxKey{}, p)
}

// FromContext は Context から Principal を取得する。未設定または型不一致の場合 nil。
func FromContext(ctx context.Context) *Principal {
	if ctx == nil {
		return nil
	}
	p, _ := ctx.Value(principalCtxKey{}).(*Principal)
	return p
}

// makePrincipalID は issuer:subject を構成する純粋関数。
//
// issuer / subject のいずれかが空の場合は空文字を返す（呼び出し側で要バリデーション）。
func makePrincipalID(issuer, subject string) string {
	if issuer == "" || subject == "" {
		return ""
	}
	return issuer + ":" + subject
}

// principalFromUser は idproxy.User から kintone Principal を構築する純粋関数。
//
// nil 入力 / issuer 欠落 / subject 欠落の場合は nil を返す。
// セキュリティクリティカル: principal_id 構成ロジックを単一化し、テスト容易化を図る。
func principalFromUser(u *upstream.User) *Principal {
	if u == nil {
		return nil
	}
	id := makePrincipalID(u.Issuer, u.Subject)
	if id == "" {
		return nil
	}
	return &Principal{
		ID:      id,
		Issuer:  u.Issuer,
		Subject: u.Subject,
		Email:   u.Email,
		Name:    u.Name,
	}
}
