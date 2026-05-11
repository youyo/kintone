package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/mcp/facade"
	"github.com/youyo/kintone/internal/mcp/oauthcallback"
	mcpserver "github.com/youyo/kintone/internal/mcp/server"
	serviceapi "github.com/youyo/kintone/internal/service/api"
	"github.com/youyo/kintone/internal/store"
)

// oauthSetup は HTTP モード起動時に必要な OAuth 関連リソースを束ねる。
//
// AuthZ=oauth のときのみ非 nil の Factory / ExtraRoutes / Builder を持つ。
type oauthSetup struct {
	Deps        facade.ToolDeps // Factory + AuthorizeURLBuilder（OAuth 時）
	ExtraRoutes []mcpserver.RouteEntry
	StateStore  oauthcallback.StateStore // close 用
}

// closeStates は StateStore.Close を呼ぶ（OAuth 時のみ非 nil）。
func (s *oauthSetup) closeStates() {
	if s == nil || s.StateStore == nil {
		return
	}
	_ = s.StateStore.Close()
}

// buildOAuthSetup は AuthZ=oauth のときに PrincipalAPIFactory と
// /oauth/kintone/start, /callback のハンドラを構築する。
//
// AuthZ=api-token / auth=none では nil 返却（呼び出し元は fallback ロジック）。
//
// 必須 env:
//   - KINTONE_OAUTH_CLIENT_ID / SECRET / REDIRECT_URL
//   - KINTONE_MCP_EXTERNAL_URL
//
// fail-fast 検証:
//   - redirect URL は HTTPS（KINTONE_OAUTH_ALLOW_PLAINTEXT_REDIRECT=1 で localhost http 許容）
//   - redirect URL == externalURL + "/oauth/kintone/callback"
func buildOAuthSetup(ctx context.Context, resolved *config.Resolved, container store.Container, authZ mcpserver.AuthZMode) (*oauthSetup, error) {
	if authZ != mcpserver.AuthZModeOAuth {
		return nil, nil
	}
	if resolved == nil {
		return nil, errors.New("mcp serve: nil config.Resolved")
	}
	if container == nil {
		return nil, errors.New("mcp serve: authz=oauth requires Storage Container in context")
	}
	if resolved.OAuthClientID == "" || resolved.OAuthClientSecret == "" {
		return nil, errors.New("mcp serve: KINTONE_OAUTH_CLIENT_ID / KINTONE_OAUTH_CLIENT_SECRET are required for authz=oauth")
	}
	if resolved.OAuthRedirectURL == "" {
		return nil, errors.New("mcp serve: KINTONE_OAUTH_REDIRECT_URL is required for authz=oauth")
	}
	externalURL := os.Getenv("KINTONE_MCP_EXTERNAL_URL")
	if externalURL == "" {
		return nil, errors.New("mcp serve: KINTONE_MCP_EXTERNAL_URL is required for authz=oauth")
	}
	allowPlaintext := os.Getenv("KINTONE_OAUTH_ALLOW_PLAINTEXT_REDIRECT") == "1"
	if err := oauthcallback.ValidateRedirectURL(resolved.OAuthRedirectURL, externalURL, allowPlaintext); err != nil {
		return nil, err
	}

	tokens, err := container.Tokens()
	if err != nil {
		return nil, fmt.Errorf("mcp serve: get TokenStore: %w", err)
	}

	// Refresher は per-request の Authenticator から使われる
	refresher := oauth.NewRefresher(oauth.RefresherConfig{
		TokenEndpoint: "https://" + resolved.Domain + "/oauth2/token",
		ClientID:      resolved.OAuthClientID,
		ClientSecret:  resolved.OAuthClientSecret,
	})

	// fallback API は AuthZ=oauth では本来呼ばれないが、interface 違反を避けるため
	// nil 安全な stub を用意する（呼ばれた場合は ErrAuthRequired 相当）。
	fallback := &noPrincipalFallback{}

	factory, err := serviceapi.NewPrincipalAPIFactory(serviceapi.PrincipalAPIFactoryConfig{
		Base:      resolved,
		Mode:      serviceapi.AuthZModeOAuth,
		Store:     tokens,
		Refresher: refresher,
		Fallback:  fallback,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp serve: build PrincipalAPIFactory: %w", err)
	}

	// Authorize URL builder（facade に渡す）
	//
	// principal_id クエリパラメータは LLM クライアント向けの参考情報。StartHandler は
	// idproxy ctx の Principal を読み、クエリは load-bearing ではない（誤動作防止のため）。
	startBaseURL := strings.TrimRight(externalURL, "/") + "/oauth/kintone/start"
	builder := func(principalID string) string {
		if principalID == "" {
			return startBaseURL
		}
		return startBaseURL + "?principal_id=" + url.QueryEscape(principalID)
	}

	// state store
	states := oauthcallback.NewMemoryStateStore()
	scopes := resolved.OAuthScopes
	_ = allowPlaintext // 検証は ValidateRedirectURL で完結し、handler 側は ExternalURL の scheme で cookie Secure を判定
	handler, err := oauthcallback.NewHandler(oauthcallback.HandlerConfig{
		Domain:       resolved.Domain,
		ClientID:     resolved.OAuthClientID,
		ClientSecret: resolved.OAuthClientSecret,
		RedirectURL:  resolved.OAuthRedirectURL,
		Scopes:       scopes,
		ExternalURL:  externalURL,
		States:       states,
		Tokens:       tokens,
	})
	if err != nil {
		_ = states.Close()
		return nil, fmt.Errorf("mcp serve: build OAuth callback handler: %w", err)
	}

	_ = ctx // 将来的に signal 制御等で利用予定

	return &oauthSetup{
		Deps: facade.ToolDeps{
			Factory:             factory,
			AuthorizeURLBuilder: builder,
		},
		ExtraRoutes: []mcpserver.RouteEntry{
			{Path: "/oauth/kintone/start", Handler: handler.StartHandler()},
			{Path: "/oauth/kintone/callback", Handler: handler.CallbackHandler()},
		},
		StateStore: states,
	}, nil
}

// noPrincipalFallback は AuthZ=oauth のとき必要となる fallback の no-op 実装。
//
// AuthZ=oauth では ForContext は常に Principal を見て分岐するため、fallback が
// 実呼び出しされることはない。インタフェース契約のため Stub のみ。
//
// API は意図的に nil（AuthZ=oauth では fallback methods は決して呼ばれない）。
// 将来の保守者が「無意味」と判断して削除しないよう明示する。
type noPrincipalFallback struct {
	serviceapi.API
}
