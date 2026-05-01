package idproxy

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	upstream "github.com/youyo/idproxy"
	idstore "github.com/youyo/idproxy/store"

	"github.com/youyo/kintone/internal/store"
)

// Env は env / CLI から読み取った idproxy 構成のパラメータ。
//
// CookieSecret は hex 文字列（>= 64 hex chars = 32 bytes）として保持し、
// Validate 時にデコード結果を CookieSecretDecoded に格納する。
//
// M12 Phase 6c で SigningKeyPEM / SigningKeyAutoGenerate / StoreBackend を追加。
// これらは BuildAuth 内で ResolveSigningKey に渡される。
type Env struct {
	Issuer         string   // KINTONE_MCP_OIDC_ISSUER（必須）
	ClientID       string   // KINTONE_MCP_OIDC_CLIENT_ID（必須）
	ClientSecret   string   // KINTONE_MCP_OIDC_CLIENT_SECRET（任意・PKCE のみなら不要）
	ExternalURL    string   // KINTONE_MCP_EXTERNAL_URL（必須）
	CookieSecret   string   // hex（>=64 hex chars）
	AllowedDomains []string // KINTONE_MCP_ALLOWED_DOMAINS
	AllowedEmails  []string // KINTONE_MCP_ALLOWED_EMAILS

	// CookieSecretDecoded は Validate 後にバイナリ化された Cookie シークレット。
	CookieSecretDecoded []byte

	// SigningKeyPEM は KINTONE_MCP_SIGNING_KEY_PEM の値。空のとき env からの注入なし。
	SigningKeyPEM string
	// SigningKeyAutoGenerate は KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1 のオプトイン。
	SigningKeyAutoGenerate bool
	// StoreBackend は backend × authMode 禁止組合せ検証で使う backend 名。
	StoreBackend string
}

// LoadEnvFromOS は os.Getenv で Env を構築する。Validate は呼ばない。
func LoadEnvFromOS() Env {
	csv := func(s string) []string {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if v := strings.TrimSpace(p); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	storeCfg := store.LoadFromEnv()
	return Env{
		Issuer:                 os.Getenv("KINTONE_MCP_OIDC_ISSUER"),
		ClientID:               os.Getenv("KINTONE_MCP_OIDC_CLIENT_ID"),
		ClientSecret:           os.Getenv("KINTONE_MCP_OIDC_CLIENT_SECRET"),
		ExternalURL:            os.Getenv("KINTONE_MCP_EXTERNAL_URL"),
		CookieSecret:           os.Getenv("KINTONE_MCP_COOKIE_SECRET"),
		AllowedDomains:         csv(os.Getenv("KINTONE_MCP_ALLOWED_DOMAINS")),
		AllowedEmails:          csv(os.Getenv("KINTONE_MCP_ALLOWED_EMAILS")),
		SigningKeyPEM:          storeCfg.SigningKeyPEM,
		SigningKeyAutoGenerate: storeCfg.SigningKeyAutoGenerate,
		StoreBackend:           storeCfg.Backend,
	}
}

// Validate は Env のバリデーション + CookieSecretDecoded への代入を行う。
//
// 期待:
//   - Issuer / ClientID / ExternalURL / CookieSecret 必須
//   - ExternalURL は https:// or http://localhost
//   - CookieSecret は hex デコード後 >= 32 bytes
func (e *Env) Validate() error {
	var errs []string
	if e.Issuer == "" {
		errs = append(errs, "KINTONE_MCP_OIDC_ISSUER is required")
	}
	if e.ClientID == "" {
		errs = append(errs, "KINTONE_MCP_OIDC_CLIENT_ID is required")
	}
	if e.ExternalURL == "" {
		errs = append(errs, "KINTONE_MCP_EXTERNAL_URL is required")
	} else if !isExternalURLValid(e.ExternalURL) {
		errs = append(errs, "KINTONE_MCP_EXTERNAL_URL must start with https:// (except http://localhost)")
	}
	if e.CookieSecret == "" {
		errs = append(errs, "KINTONE_MCP_COOKIE_SECRET is required")
	} else {
		raw, err := hex.DecodeString(e.CookieSecret)
		if err != nil {
			errs = append(errs, "KINTONE_MCP_COOKIE_SECRET must be valid hex")
		} else if len(raw) < 32 {
			errs = append(errs, "KINTONE_MCP_COOKIE_SECRET must be at least 32 bytes (>=64 hex chars)")
		} else {
			e.CookieSecretDecoded = raw
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("idproxy env validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func isExternalURLValid(s string) bool {
	if strings.HasPrefix(s, "https://") {
		return true
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// BuildAuth は Env から idproxy.Auth を構築する。
//
// Validate 済みであることが前提。Validate を呼んでいない場合は内部で呼ぶ。
//
// M12 Phase 6c から:
//   - SigningKey は ResolveSigningKey 経由で env > Storage > ephemeral の順で解決
//   - idproxy の Store は container.IDProxyStore() で取得（auth=oidc 必須）
//   - container == nil かつ authMode == "oidc" は ErrSigningKeyRequired で fail-fast
//
// authZMode は idproxy 自身では使わないが、上位の判断と整合させるためシグネチャに残す。
func BuildAuth(ctx context.Context, e *Env, authMode string, authZMode string, container store.Container) (*upstream.Auth, error) {
	_ = authZMode // idproxy の関心事ではない（upstream kintone への認可方式）
	if e == nil {
		return nil, errors.New("idproxy: nil Env")
	}
	if e.CookieSecretDecoded == nil {
		if err := e.Validate(); err != nil {
			return nil, err
		}
	}

	// SigningKey 解決（env > Storage > ephemeral）
	signingKey, err := ResolveSigningKey(
		ctx,
		e.SigningKeyPEM,
		e.SigningKeyAutoGenerate,
		AuthMode(authMode),
		e.StoreBackend,
		container,
	)
	if err != nil {
		return nil, fmt.Errorf("idproxy: resolve signing key: %w", err)
	}

	// idproxy の Store: auth=oidc では Container.IDProxyStore を使う
	var idpStore upstream.Store
	if container != nil && AuthMode(authMode) == AuthModeOIDC {
		s, err := container.IDProxyStore()
		if err != nil {
			return nil, fmt.Errorf("idproxy: get idproxy store: %w", err)
		}
		idpStore = s
	} else {
		// fallback: Memory（auth=none / container 不在の development 経路）
		idpStore = idstore.NewMemoryStore()
	}

	cfg := upstream.Config{
		Providers: []upstream.OIDCProvider{{
			Issuer:       e.Issuer,
			ClientID:     e.ClientID,
			ClientSecret: e.ClientSecret,
		}},
		AllowedDomains:  e.AllowedDomains,
		AllowedEmails:   e.AllowedEmails,
		ExternalURL:     e.ExternalURL,
		CookieSecret:    e.CookieSecretDecoded,
		Store:           idpStore,
		OAuth:           &upstream.OAuthConfig{SigningKey: signingKey},
		SessionMaxAge:   24 * time.Hour,
		AccessTokenTTL:  1 * time.Hour,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	}
	return upstream.New(ctx, cfg)
}
