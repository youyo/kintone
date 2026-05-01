package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/store"
)

// ErrAuthRequired は MCP リクエスト時に Principal が必要だが取得できなかった場合のエラー。
//
// facade で AUTH_REQUIRED にマップする想定。CLI 単発実行（stdio + apitoken）では発生しない。
var ErrAuthRequired = errors.New("api: authentication required")

// AuthZMode は upstream kintone への認証方式。
type AuthZMode string

const (
	// AuthZModeAPIToken は API Token 認証。Principal の有無に関わらず Resolved.APIToken を使う。
	AuthZModeAPIToken AuthZMode = "api-token"
	// AuthZModeOAuth は OAuth 2.0 認証。Principal が必須で、TokenStore から ID 別トークンを引く。
	AuthZModeOAuth AuthZMode = "oauth"
)

// PrincipalAPIFactory は MCP リクエスト context から API を構築するファクトリ。
//
// stateless に各リクエストで呼ばれる。base は config.Resolved（domain / OAuth client 情報を含む）。
// tokens は OAuth トークン参照用、refresher は refresh_token grant 実行用。
type PrincipalAPIFactory struct {
	base      *config.Resolved
	mode      AuthZMode
	tokens    store.TokenStore
	refresher oauth.RefresherInterface
	// fallback は AuthZMode=api-token または Principal 不在時に使う既存 API。
	// 既存 stdio 経路の単発構築をそのまま流用するため、構築済みの API を持ち回る。
	fallback API
}

// PrincipalAPIFactoryConfig は PrincipalAPIFactory のコンストラクタ引数。
type PrincipalAPIFactoryConfig struct {
	// Base は config.Load で得た Resolved。Domain と OAuth 設定を参照する。
	Base *config.Resolved
	// Mode は upstream への認証方式。
	Mode AuthZMode
	// Store は OAuth Token 永続ストア。AuthZMode=oauth で必須。
	Store store.TokenStore
	// Refresher は refresh_token grant を実行する。AuthZMode=oauth で必須。
	Refresher oauth.RefresherInterface
	// Fallback は Principal 不在時に使う API。AuthZMode=api-token のときは常にこれを返す。
	Fallback API
}

// NewPrincipalAPIFactory は ファクトリを構築する。
//
// 必須:
//   - cfg.Base
//   - cfg.Fallback（少なくとも api-token 経路で必要）
//   - AuthZMode=oauth のときは Store + Refresher
func NewPrincipalAPIFactory(cfg PrincipalAPIFactoryConfig) (*PrincipalAPIFactory, error) {
	if cfg.Base == nil {
		return nil, errors.New("api: PrincipalAPIFactory: base config is nil")
	}
	if cfg.Fallback == nil {
		return nil, errors.New("api: PrincipalAPIFactory: fallback API is nil")
	}
	switch cfg.Mode {
	case AuthZModeAPIToken:
		// store / refresher は不要
	case AuthZModeOAuth:
		if cfg.Store == nil {
			return nil, errors.New("api: PrincipalAPIFactory: store required for AuthZ=oauth")
		}
	default:
		return nil, fmt.Errorf("api: PrincipalAPIFactory: unknown mode %q", cfg.Mode)
	}
	return &PrincipalAPIFactory{
		base:      cfg.Base,
		mode:      cfg.Mode,
		tokens:    cfg.Store,
		refresher: cfg.Refresher,
		fallback:  cfg.Fallback,
	}, nil
}

// ForContext は ctx の Principal を見て対応する API を返す。
//
// 振る舞い:
//   - AuthZMode=api-token: 常に fallback を返す（Principal は無視）
//   - AuthZMode=oauth + Principal あり: TokenStore から取得し、oauth.Authenticator で client 構築
//   - AuthZMode=oauth + Principal なし: ErrAuthRequired
//   - AuthZMode=oauth + TokenStore に該当なし: ErrAuthRequired をラップして返す
func (f *PrincipalAPIFactory) ForContext(ctx context.Context) (API, error) {
	if f.mode == AuthZModeAPIToken {
		return f.fallback, nil
	}

	p := idproxy.FromContext(ctx)
	if p == nil {
		return nil, ErrAuthRequired
	}

	// AuthZ=oauth + Principal あり: TokenStore から ID 別トークンを参照
	tok, err := f.tokens.Get(ctx, f.base.Domain, p.ID, store.AuthTypeOAuth)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: principal %q has no token", ErrAuthRequired, p.ID)
		}
		return nil, fmt.Errorf("api: PrincipalAPIFactory: get token: %w", err)
	}
	if tok == nil {
		return nil, fmt.Errorf("%w: principal %q has no token", ErrAuthRequired, p.ID)
	}

	authn := oauth.NewAuthenticator(f.tokens, f.base.Domain, p.ID, f.refresher, nil)
	kc, err := kintoneapi.NewFromResolvedWithAuth(f.base, authn)
	if err != nil {
		return nil, fmt.Errorf("api: PrincipalAPIFactory: build kintone client: %w", err)
	}
	return NewFromKintone(kc)
}
