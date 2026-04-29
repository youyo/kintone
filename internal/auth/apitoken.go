// Package auth は kintone REST API への認証戦略を提供する。
//
// Authenticator はリクエストに認証情報を付与する責務のみを持つ。
// HTTP 通信は kintoneapi 層が担当する。
//
// 実装一覧:
//   - APITokenAuthenticator (M03)
//   - OAuthAuthenticator    (M09)
//   - IDProxyAuthenticator  (M10)
package auth

import (
	"context"
	"errors"
	"net/http"
)

// Authenticator はリクエストに認証情報を付与する。
// 実装は冪等であり、同じリクエストに対して何度呼ばれても同じ結果を返す。
//
// 設計判断: Apply はあえて ctx を受け取る。M03 (API Token) では ctx は使わないが、
// M09 (OAuth) は refresh トークン HTTP 呼び出しに ctx が必須となる。
// M09 で interface を変更すると M03 に破壊的変更が逆流するため、最初から ctx を含める。
type Authenticator interface {
	// Apply はリクエストに認証ヘッダを付与する。
	// ctx は認証情報の取得（OAuth refresh 等）の HTTP 呼び出しに使う。
	Apply(ctx context.Context, req *http.Request) error
}

// ErrEmptyAPIToken は空トークンが渡されたエラー。
var ErrEmptyAPIToken = errors.New("auth: API Token is empty")

// APITokenAuthenticator は X-Cybozu-API-Token ヘッダを付与する。
//
// 仕様: https://cybozu.dev/ja/kintone/docs/rest-api/overview/auth/
//   - 単一トークン形式: X-Cybozu-API-Token: <token>
//   - 複数トークン形式: X-Cybozu-API-Token: <token1>,<token2>（カンマ区切り）
//
// M03 は単一/複数いずれも文字列としてそのまま透過する（呼び出し側責務）。
type APITokenAuthenticator struct {
	token string
}

// NewAPITokenAuthenticator は API Token 認証を行う Authenticator を返す。
// token が空文字の場合は ErrEmptyAPIToken を返す。
func NewAPITokenAuthenticator(token string) (*APITokenAuthenticator, error) {
	if token == "" {
		return nil, ErrEmptyAPIToken
	}
	return &APITokenAuthenticator{token: token}, nil
}

// Apply は X-Cybozu-API-Token ヘッダを req に付与する。
// ctx は本実装では未使用（インターフェイス契約のみ）。
// 同一 req に複数回呼ばれても重複ヘッダにならない（Set を使用）。
func (a *APITokenAuthenticator) Apply(_ context.Context, req *http.Request) error {
	if req == nil {
		return errors.New("auth: request is nil")
	}
	req.Header.Set("X-Cybozu-API-Token", a.token)
	return nil
}
