// Package kintoneapi は kintone REST API への薄い HTTP クライアントを提供する。
//
// 設計方針:
//   - 外部 SDK 非依存（net/http 薄ラッパー）
//   - エンドポイントごとに型付き Request/Response 構造体
//   - 認証は auth.Authenticator 経由で抽象化
//   - レート制限/5xx は ClientOptions.RetryPolicy で扱う
//   - 戻り値の error は *APIError か net 系 error
package kintoneapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/youyo/kintone/internal/auth"
	"github.com/youyo/kintone/internal/config"
)

// 既知エラー。
var (
	// ErrEmptyDomain は ClientOptions.Domain が空のとき返る。
	ErrEmptyDomain = errors.New("kintoneapi: domain is empty")
	// ErrInvalidDomain は Domain がスキーム/スラッシュ/空白を含むとき返る。
	ErrInvalidDomain = errors.New("kintoneapi: domain must be a bare host (no scheme, no path)")
	// ErrNilAuthenticator は Authenticator が nil のとき返る。
	ErrNilAuthenticator = errors.New("kintoneapi: authenticator is nil")
	// ErrUnsupportedAuthMode は Resolved.Auth が現状未対応のとき返る。
	ErrUnsupportedAuthMode = errors.New("kintoneapi: unsupported auth mode")
)

// defaultUserAgent は M03 時点の UA 値。
// M11 リリース時に internal/cli.Version から動的注入する方針。
const defaultUserAgent = "kintone-cli/dev"

// defaultHTTPTimeout はデフォルトの HTTP 全体タイムアウト。
const defaultHTTPTimeout = 30 * time.Second

// Client は kintone REST API クライアント。複数 goroutine から安全に共有可能。
type Client struct {
	baseURL    string
	httpClient *http.Client
	auth       auth.Authenticator
	userAgent  string
	retry      RetryPolicy
	now        func() time.Time
	sleep      func(time.Duration)
}

// ClientOptions は Client コンストラクタの入力。
type ClientOptions struct {
	// Domain は kintone のホスト名（必須、"example.cybozu.com" 形式）。
	// スキーム・スラッシュ・空白を含むと ErrInvalidDomain。
	Domain string
	// Authenticator は認証戦略（必須）。
	Authenticator auth.Authenticator
	// HTTPClient は任意。nil の場合 30s timeout の http.Client を使用。
	HTTPClient *http.Client
	// UserAgent は任意。空のとき defaultUserAgent。
	UserAgent string
	// RetryPolicy は任意。ゼロ値の場合 DefaultRetryPolicy。
	RetryPolicy RetryPolicy
	// Now はテスト用注入。nil の場合 time.Now。
	Now func() time.Time
	// Sleep はテスト用注入。nil の場合 time.Sleep。
	Sleep func(time.Duration)
}

// New は ClientOptions から Client を構築する。
func New(opts ClientOptions) (*Client, error) {
	if opts.Domain == "" {
		return nil, ErrEmptyDomain
	}
	if err := validateDomain(opts.Domain); err != nil {
		return nil, err
	}
	if opts.Authenticator == nil {
		return nil, ErrNilAuthenticator
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}

	ua := opts.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}

	retry := opts.RetryPolicy
	if retry.MaxAttempts <= 0 {
		retry = DefaultRetryPolicy
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	return &Client{
		baseURL:    "https://" + opts.Domain,
		httpClient: httpClient,
		auth:       opts.Authenticator,
		userAgent:  ua,
		retry:      retry,
		now:        now,
		sleep:      sleep,
	}, nil
}

// NewFromResolved は config.Resolved から Client を構築する便利関数。
//
// Resolved.Auth が "api-token" のとき auth.NewAPITokenAuthenticator を使い、
// "oauth" のとき ErrUnsupportedAuthMode を返す（OAuth の場合は NewFromResolvedWithAuth を使うこと）。
// それ以外の未知の値も ErrUnsupportedAuthMode を返す。
func NewFromResolved(r *config.Resolved) (*Client, error) {
	if r == nil {
		return nil, errors.New("kintoneapi: resolved config is nil")
	}
	switch r.Auth {
	case config.AuthModeAPIToken:
		a, err := auth.NewAPITokenAuthenticator(r.APIToken)
		if err != nil {
			return nil, fmt.Errorf("kintoneapi: NewFromResolved: %w", err)
		}
		return New(ClientOptions{Domain: r.Domain, Authenticator: a})
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedAuthMode, r.Auth)
	}
}

// NewFromResolvedWithAuth は config.Resolved と外部から構築済みの Authenticator から
// Client を構築する。
//
// OAuth 用 Authenticator（oauth.Authenticator）を注入するときに使用する。
// Authenticator の構築は cli/auth/helpers.go の責務として完全に切り出し、
// kintoneapi は auth パッケージ詳細（oauth, tokenstore）を知らない設計を維持する。
//
// 使用例:
//
//	a := oauth.NewAuthenticator(store, domain, principalID, refresher, nil)
//	client, err := kintoneapi.NewFromResolvedWithAuth(resolved, a)
func NewFromResolvedWithAuth(r *config.Resolved, a auth.Authenticator) (*Client, error) {
	if r == nil {
		return nil, errors.New("kintoneapi: resolved config is nil")
	}
	return New(ClientOptions{Domain: r.Domain, Authenticator: a})
}

// validateDomain は Domain の形式を検証する。
// スキーム・スラッシュ・空白文字を含むものは拒否する。
func validateDomain(domain string) error {
	if strings.Contains(domain, "://") {
		return fmt.Errorf("%w: contains scheme: %q", ErrInvalidDomain, domain)
	}
	if strings.ContainsAny(domain, " /\\\t\n\r") {
		return fmt.Errorf("%w: contains whitespace or slash: %q", ErrInvalidDomain, domain)
	}
	return nil
}
