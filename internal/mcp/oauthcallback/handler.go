package oauthcallback

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/youyo/kintone/internal/auth/oauth"
	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/store"
)

// StateCookieName は state を保持する HTTP cookie の名前。
const StateCookieName = "kintone_oauth_state"

// HandlerConfig は Handler 構築の入力。
//
// AuthorizeBase / TokenEndpoint はテスト時に kintonefake などへ差し替えできるよう exposed。
// 本番では Domain から組み立てる（NewHandler 内）。
type HandlerConfig struct {
	Domain        string // kintone domain（例: "example.cybozu.com"）
	ClientID      string
	ClientSecret  string
	RedirectURL   string   // 公開 callback URL
	Scopes        []string // 空なら oauth.DefaultOAuthScopes 相当
	ExternalURL   string   // 公開ベース URL（cookie の Secure 判定用）
	States        StateStore
	Tokens        store.TokenStore
	HTTPClient    *http.Client
	Now           func() time.Time
	RandReader    io.Reader
	AuthorizeBase string                                        // 既定: "https://{Domain}/oauth2/authorization"
	TokenEndpoint string                                        // 既定: "https://{Domain}/oauth2/token"
	PrincipalFn   func(*http.Request) *idproxy.Principal        // 既定: idproxy.FromContext(r.Context())
	Logger        func(level, msg string, attrs map[string]any) // テスト用 hook（nil OK）
}

// Handler は /oauth/kintone/start と /oauth/kintone/callback の HTTP ハンドラを提供する。
type Handler struct {
	cfg HandlerConfig
}

// NewHandler は Handler を構築し、必須フィールドを検証する。
func NewHandler(cfg HandlerConfig) (*Handler, error) {
	if cfg.Domain == "" {
		return nil, errors.New("oauthcallback: Domain is required")
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("oauthcallback: ClientID / ClientSecret are required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("oauthcallback: RedirectURL is required")
	}
	if cfg.States == nil {
		return nil, errors.New("oauthcallback: States is required")
	}
	if cfg.Tokens == nil {
		return nil, errors.New("oauthcallback: Tokens is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.AuthorizeBase == "" {
		cfg.AuthorizeBase = "https://" + cfg.Domain + "/oauth2/authorization"
	}
	if cfg.TokenEndpoint == "" {
		cfg.TokenEndpoint = "https://" + cfg.Domain + "/oauth2/token"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = defaultScopes()
	}
	if cfg.PrincipalFn == nil {
		cfg.PrincipalFn = func(r *http.Request) *idproxy.Principal {
			return idproxy.FromContext(r.Context())
		}
	}
	return &Handler{cfg: cfg}, nil
}

// StartHandler は /oauth/kintone/start を処理する HTTP ハンドラ。
//
// idproxy 経由で Principal が context に注入されていることを前提とする。
// Principal なしは 401。
func (h *Handler) StartHandler() http.Handler {
	return http.HandlerFunc(h.handleStart)
}

// CallbackHandler は /oauth/kintone/callback を処理する HTTP ハンドラ。
//
// idproxy 経由で Principal が context に注入されていることを前提とする
// （SameSite=Lax で top-level GET navigation に cookie が同伴）。
func (h *Handler) CallbackHandler() http.Handler {
	return http.HandlerFunc(h.handleCallback)
}

// handleStart は authorize URL を組み立ててリダイレクトする。
func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal := h.cfg.PrincipalFn(r)
	if principal == nil || principal.ID == "" {
		h.log("warn", "start: principal missing", map[string]any{"path": r.URL.Path})
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}

	// PKCE / state 生成
	pkce, err := oauth.GeneratePKCE(h.cfg.RandReader)
	if err != nil {
		h.log("error", "start: generate pkce", map[string]any{"err_class": "pkce"})
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	state, err := oauth.GenerateState(h.cfg.RandReader)
	if err != nil {
		h.log("error", "start: generate state", map[string]any{"err_class": "state"})
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	entry := StateEntry{
		State:       state,
		PrincipalID: principal.ID,
		Verifier:    pkce.Verifier,
		Method:      pkce.Method,
	}
	if err := h.cfg.States.Put(r.Context(), entry); err != nil {
		h.log("error", "start: put state", map[string]any{"err_class": "state_store"})
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// state cookie 設定
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    state,
		Path:     "/oauth/kintone/",
		HttpOnly: true,
		Secure:   h.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((DefaultStateTTL + 30*time.Second).Seconds()),
	})

	// authorize URL を組み立てて 302
	authzURL := h.buildAuthorizeURL(state, pkce.Challenge, pkce.Method)
	http.Redirect(w, r, authzURL, http.StatusFound)
}

// handleCallback は authorization code を token に交換して TokenStore に保存する。
func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// kintone から ?error= で戻る場合もあるが、ここでは 400 で固定メッセージ
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		// errCode 自体はログに残す（プロバイダ側エラーコードは secret ではない）
		h.log("warn", "callback: provider error", map[string]any{"error": errCode})
		h.renderHTML(w, http.StatusBadRequest, htmlAuthError)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		h.log("warn", "callback: missing params", map[string]any{"has_state": state != "", "has_code": code != ""})
		h.renderHTML(w, http.StatusBadRequest, htmlAuthError)
		return
	}

	// state cookie 一致確認（layer 2）
	cookie, err := r.Cookie(StateCookieName)
	if err != nil || cookie.Value == "" {
		h.log("warn", "callback: state cookie missing", nil)
		h.renderHTML(w, http.StatusForbidden, htmlAuthError)
		return
	}
	if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		h.log("warn", "callback: state cookie mismatch", map[string]any{"state_prefix": prefix(state)})
		h.renderHTML(w, http.StatusForbidden, htmlAuthError)
		return
	}

	// state map から取り出し（layer 2 second-half: one-shot）
	entry, err := h.cfg.States.Take(r.Context(), state)
	if err != nil {
		if errors.Is(err, ErrStateNotFound) {
			h.log("warn", "callback: state not found or expired", map[string]any{"state_prefix": prefix(state)})
			h.renderHTML(w, http.StatusBadRequest, htmlAuthExpired)
			return
		}
		h.log("error", "callback: take state", map[string]any{"err_class": "state_store"})
		h.renderHTML(w, http.StatusInternalServerError, htmlAuthError)
		return
	}

	// layer 1: idproxy 経由の Principal と一致確認
	principal := h.cfg.PrincipalFn(r)
	if principal == nil || principal.ID == "" {
		h.log("warn", "callback: principal missing", nil)
		h.renderHTML(w, http.StatusUnauthorized, htmlAuthError)
		return
	}
	if subtle.ConstantTimeCompare([]byte(principal.ID), []byte(entry.PrincipalID)) != 1 {
		h.log("warn", "callback: principal mismatch", map[string]any{
			"expected_prefix": prefix(entry.PrincipalID),
			"actual_prefix":   prefix(principal.ID),
		})
		h.renderHTML(w, http.StatusForbidden, htmlAuthError)
		return
	}

	// token endpoint へ交換
	tokenResp, err := oauth.ExchangeCode(r.Context(), oauth.ExchangeCodeRequest{
		TokenEndpoint: h.cfg.TokenEndpoint,
		ClientID:      h.cfg.ClientID,
		ClientSecret:  h.cfg.ClientSecret,
		Code:          code,
		RedirectURL:   h.cfg.RedirectURL,
		CodeVerifier:  entry.Verifier,
		HTTPClient:    h.cfg.HTTPClient,
		Now:           h.cfg.Now,
	})
	if err != nil {
		// access_token / refresh_token / code を絶対にロガーに出さない
		h.log("error", "callback: token exchange failed", map[string]any{"err_class": "token_exchange"})
		h.renderHTML(w, http.StatusBadGateway, htmlAuthError)
		return
	}

	// TokenStore へ保存
	now := h.cfg.Now()
	tok := store.Token{
		Domain:       h.cfg.Domain,
		PrincipalID:  principal.ID,
		AuthType:     store.AuthTypeOAuth,
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    tokenResp.ExpiresAt,
		UpdatedAt:    now,
	}
	if err := h.cfg.Tokens.Put(r.Context(), tok); err != nil {
		h.log("error", "callback: store put failed", map[string]any{"err_class": "store"})
		h.renderHTML(w, http.StatusInternalServerError, htmlAuthError)
		return
	}

	// state cookie 削除（過去のセッションを無効化）
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    "",
		Path:     "/oauth/kintone/",
		HttpOnly: true,
		Secure:   h.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	h.log("info", "callback: token stored", map[string]any{"principal_prefix": prefix(principal.ID)})
	h.renderHTML(w, http.StatusOK, htmlAuthSuccess)
}

// buildAuthorizeURL は kintone authorize endpoint URL を組み立てる。
func (h *Handler) buildAuthorizeURL(state, codeChallenge, method string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", h.cfg.ClientID)
	q.Set("redirect_uri", h.cfg.RedirectURL)
	q.Set("scope", strings.Join(h.cfg.Scopes, " "))
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", method)
	return h.cfg.AuthorizeBase + "?" + q.Encode()
}

// cookieSecure は外部 URL が https かどうかで Secure 属性を決定する。
func (h *Handler) cookieSecure() bool {
	if h.cfg.ExternalURL == "" {
		// 安全側: HTTPS 想定で Secure=true
		return true
	}
	return strings.HasPrefix(h.cfg.ExternalURL, "https://")
}

// renderHTML は固定 HTML を返す（secret / detail を含まない）。
func (h *Handler) renderHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// log は構造化ログを出力する（hook が設定されていれば）。
//
// access_token / refresh_token / code / authorization code / verifier を絶対に attrs に渡さない。
// state / principal_id は prefix（最初 4 文字 + "..."）にマスクしてから渡す。
func (h *Handler) log(level, msg string, attrs map[string]any) {
	if h.cfg.Logger != nil {
		h.cfg.Logger(level, msg, attrs)
		return
	}
	// 標準ロガー連携は serve.go の logger を経由する想定。ここでは何もしない。
	_ = level
	_ = msg
	_ = attrs
}

// defaultScopes は kintone 全 scope。
func defaultScopes() []string {
	return []string{
		"k:app_record:read",
		"k:app_record:write",
		"k:app_settings:read",
		"k:app_settings:write",
		"k:file:read",
		"k:file:write",
	}
}

// prefix は state / principal_id 等を最初 4 文字 + "..." にマスクする（ログ漏洩防止）。
func prefix(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:4] + "..."
}

// ValidateRedirectURL は KINTONE_OAUTH_REDIRECT_URL の HTTPS 制約と
// KINTONE_MCP_EXTERNAL_URL との整合を fail-fast で検証する。
//
// ルール:
//   - redirectURL の scheme は https://、または allowPlaintext=true 時の http://localhost / http://127.0.0.1 のみ
//   - redirectURL は externalURL + "/oauth/kintone/callback" と完全一致（URL 正規化後）
//
// kintone OAuth は不一致時に opaque な error しか返さないため、起動時に検出する。
func ValidateRedirectURL(redirectURL, externalURL string, allowPlaintext bool) error {
	if redirectURL == "" {
		return errors.New("oauthcallback: KINTONE_OAUTH_REDIRECT_URL is required")
	}
	if externalURL == "" {
		return errors.New("oauthcallback: KINTONE_MCP_EXTERNAL_URL is required")
	}
	ru, err := url.Parse(redirectURL)
	if err != nil {
		return fmt.Errorf("oauthcallback: redirect URL parse: %w", err)
	}
	eu, err := url.Parse(externalURL)
	if err != nil {
		return fmt.Errorf("oauthcallback: external URL parse: %w", err)
	}
	if err := validateScheme(ru, allowPlaintext); err != nil {
		return err
	}
	expected := strings.TrimRight(eu.String(), "/") + "/oauth/kintone/callback"
	if redirectURL != expected {
		return fmt.Errorf(
			"oauthcallback: KINTONE_OAUTH_REDIRECT_URL %q must equal %q. "+
				"Update the kintone OAuth Application's redirect URI to match this exactly",
			redirectURL, expected,
		)
	}
	return nil
}

func validateScheme(u *url.URL, allowPlaintext bool) error {
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if !allowPlaintext {
			return fmt.Errorf("oauthcallback: redirect URL scheme must be https (got http). Set KINTONE_OAUTH_ALLOW_PLAINTEXT_REDIRECT=1 for development with localhost only")
		}
		host := u.Hostname()
		switch host {
		case "localhost", "127.0.0.1", "::1":
			return nil
		default:
			return fmt.Errorf("oauthcallback: http allowed only for localhost/127.0.0.1, got %q", host)
		}
	default:
		return fmt.Errorf("oauthcallback: unsupported scheme %q", u.Scheme)
	}
}

// HTML テンプレート（固定文字列、secret なし）

const htmlAuthSuccess = `<!DOCTYPE html>
<html lang="ja"><head><meta charset="utf-8"><title>認証完了</title></head>
<body><h1>kintone 認証が完了しました</h1>
<p>このブラウザタブを閉じて、MCP クライアント（Claude Desktop など）に戻ってください。</p>
</body></html>`

const htmlAuthError = `<!DOCTYPE html>
<html lang="ja"><head><meta charset="utf-8"><title>認証エラー</title></head>
<body><h1>認証エラー</h1>
<p>kintone OAuth フローでエラーが発生しました。もう一度認証フローを開始してください。</p>
</body></html>`

const htmlAuthExpired = `<!DOCTYPE html>
<html lang="ja"><head><meta charset="utf-8"><title>認証セッションが期限切れです</title></head>
<body><h1>認証セッションが期限切れです</h1>
<p>OAuth セッションが ` + "10 分" + ` を超過しました。もう一度認証フローを開始してください。</p>
</body></html>`
