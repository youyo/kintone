package server

import (
	"errors"
	"fmt"
)

// AuthMode は MCP server 層の認証要否を表す。
//
// kintone MCP は HTTP/SSE remote 公開時のリクエスト前段保護で使う。
// stdio mode（cli 経由）は AuthModeNone 固定。
type AuthMode string

const (
	// AuthModeNone は認証なし。stdio または信頼済み LAN での HTTP 公開で使う。
	AuthModeNone AuthMode = "none"
	// AuthModeOIDC は idproxy v0.4.2 によるリクエスト認証。
	AuthModeOIDC AuthMode = "oidc"
)

// AuthZMode は upstream kintone への認証方式（Authorization）。
//
// service/api.AuthZMode と同じ値域だが、cli/server 層の設定として独立に保持する
// （層をまたぐ struct 共有を避ける、内部 enum の重複は許容する）。
type AuthZMode string

const (
	// AuthZModeAPIToken は API Token 認証（既存挙動）。
	AuthZModeAPIToken AuthZMode = "api-token"
	// AuthZModeOAuth は OAuth 認証（M09）。
	AuthZModeOAuth AuthZMode = "oauth"
)

// ParseAuthMode は文字列から AuthMode を返す。空文字は AuthModeNone。
func ParseAuthMode(s string) (AuthMode, error) {
	switch s {
	case "", "none":
		return AuthModeNone, nil
	case "oidc":
		return AuthModeOIDC, nil
	default:
		return "", fmt.Errorf("server: unknown auth mode %q (allowed: none, oidc)", s)
	}
}

// ParseAuthZMode は文字列から AuthZMode を返す。空文字は AuthZModeAPIToken。
func ParseAuthZMode(s string) (AuthZMode, error) {
	switch s {
	case "", "api-token":
		return AuthZModeAPIToken, nil
	case "oauth":
		return AuthZModeOAuth, nil
	default:
		return "", fmt.Errorf("server: unknown authz mode %q (allowed: api-token, oauth)", s)
	}
}

// ServeMode は MCP server の起動モードを表す（stdio / http）。
type ServeMode int

const (
	// ServeModeStdio は stdio JSON-RPC モード。listen 空のとき。
	ServeModeStdio ServeMode = iota
	// ServeModeHTTP は HTTP/Streamable モード。listen 指定あり。
	ServeModeHTTP
)

// PickServeMode は --listen フラグから ServeMode を決定する。
func PickServeMode(listenAddr string) ServeMode {
	if listenAddr == "" {
		return ServeModeStdio
	}
	return ServeModeHTTP
}

// ErrStdioOAuthUnsupported は stdio transport で authz=oauth が指定された場合に返される
// 型付き sentinel エラー。
//
// stdio は単一プロセス・単一認証文脈で動作するため OAuth の per-request principal binding
// と矛盾する。M15 以前は silent no-op として API Token に degrade していたが、運用事故
// （OAuth で動いていると誤認する）を排除するため fail-fast に変更した。
//
// 復旧手順を含む user-facing メッセージは CLI 層（cli/mcp）が `clierr.UsageError` で
// ラップして返す。本 sentinel は短い理由のみ保持する。
var ErrStdioOAuthUnsupported = errors.New("server: authz=oauth requires HTTP transport (--listen)")

// ValidateModes は (mode, auth, authz) の組み合わせを検証する。
//
//   - stdio + auth=oidc は不正（stdio に HTTP 認証は不要かつ不可能）
//   - stdio + authz=oauth は不正（OAuth は per-request principal binding が必須で
//     HTTP transport を要する。M15）
//   - http + auth=oidc + authz=api-token は許容（multi-user だが共通 API Token）
func ValidateModes(serve ServeMode, auth AuthMode, authz AuthZMode) error {
	switch authz {
	case AuthZModeAPIToken, AuthZModeOAuth:
	default:
		return fmt.Errorf("server: invalid AuthZMode %q", authz)
	}
	switch auth {
	case AuthModeNone, AuthModeOIDC:
	default:
		return fmt.Errorf("server: invalid AuthMode %q", auth)
	}
	if serve == ServeModeStdio && auth == AuthModeOIDC {
		return errors.New("server: AuthMode=oidc is not supported on stdio (use --listen for HTTP mode)")
	}
	if serve == ServeModeStdio && authz == AuthZModeOAuth {
		return ErrStdioOAuthUnsupported
	}
	return nil
}
