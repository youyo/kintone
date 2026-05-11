package oauthcallback_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/mcp/oauthcallback"
	"github.com/youyo/kintone/internal/store"
)

// --- fake stores ---

type fakeTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*store.Token
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{tokens: map[string]*store.Token{}}
}

func tokenKey(domain, principalID string, t store.AuthType) string {
	return domain + "|" + principalID + "|" + string(t)
}

func (s *fakeTokenStore) Get(_ context.Context, domain, principalID string, t store.AuthType) (*store.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, ok := s.tokens[tokenKey(domain, principalID, t)]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *tok
	return &cp, nil
}

func (s *fakeTokenStore) Put(_ context.Context, tok store.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := tok
	s.tokens[tokenKey(tok.Domain, tok.PrincipalID, tok.AuthType)] = &cp
	return nil
}

func (s *fakeTokenStore) Delete(_ context.Context, domain, principalID string, t store.AuthType) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, tokenKey(domain, principalID, t))
	return nil
}

func (s *fakeTokenStore) ListByDomain(_ context.Context, _ string, _ store.AuthType) ([]*store.Token, error) {
	return nil, nil
}

func (s *fakeTokenStore) Close() error { return nil }

// --- test util ---

// newTokenServer は kintone /oauth2/token を mock する httptest サーバを返す。
//
// formCapture が non-nil なら、受け取った form をコピーして送信する。
func newTokenServer(t *testing.T, status int, body map[string]any, formCapture chan<- url.Values) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if formCapture != nil {
			cp := make(url.Values, len(r.PostForm))
			for k, v := range r.PostForm {
				cp[k] = append([]string{}, v...)
			}
			select {
			case formCapture <- cp:
			default:
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
}

// buildHandler は test 用 Handler を構築する。
func buildHandler(t *testing.T, opt ...func(*oauthcallback.HandlerConfig)) (*oauthcallback.Handler, *fakeTokenStore, *oauthcallback.MemoryStateStore) {
	t.Helper()
	tokens := newFakeTokenStore()
	states := oauthcallback.NewMemoryStateStore()
	cfg := oauthcallback.HandlerConfig{
		Domain:        "example.cybozu.com",
		ClientID:      "cid",
		ClientSecret:  "csec",
		RedirectURL:   "https://mcp.example.com/oauth/kintone/callback",
		ExternalURL:   "https://mcp.example.com",
		States:        states,
		Tokens:        tokens,
		AuthorizeBase: "https://example.cybozu.com/oauth2/authorization",
		TokenEndpoint: "https://example.cybozu.com/oauth2/token",
	}
	for _, fn := range opt {
		fn(&cfg)
	}
	h, err := oauthcallback.NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, tokens, states
}

func principalReq(method, target string, body io.Reader, p *idproxy.Principal) *http.Request {
	r := httptest.NewRequest(method, target, body)
	if p != nil {
		r = r.WithContext(idproxy.WithPrincipal(r.Context(), p))
	}
	return r
}

// --- StartHandler tests ---

func TestStartHandler_RedirectsToAuthorize(t *testing.T) {
	t.Parallel()
	h, _, states := buildHandler(t)

	pid := "https://issuer.example.com:user-1"
	req := principalReq(http.MethodGet, "/oauth/kintone/start", nil, &idproxy.Principal{ID: pid})
	rw := httptest.NewRecorder()
	h.StartHandler().ServeHTTP(rw, req)

	if rw.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", rw.Code, rw.Body.String())
	}
	loc := rw.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://example.cybozu.com/oauth2/authorization?") {
		t.Errorf("Location = %q", loc)
	}
	u, _ := url.Parse(loc)
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("client_id") != "cid" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://mcp.example.com/oauth/kintone/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") == "" {
		t.Errorf("state is empty")
	}
	if q.Get("code_challenge") == "" {
		t.Errorf("code_challenge is empty")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	// state cookie が設定されている
	cookieHeader := rw.Header().Get("Set-Cookie")
	if !strings.Contains(cookieHeader, oauthcallback.StateCookieName+"=") {
		t.Errorf("state cookie missing; headers=%v", rw.Header())
	}
	if !strings.Contains(cookieHeader, "HttpOnly") {
		t.Errorf("cookie missing HttpOnly: %s", cookieHeader)
	}
	if !strings.Contains(cookieHeader, "SameSite=Lax") {
		t.Errorf("cookie missing SameSite=Lax: %s", cookieHeader)
	}
	// state が StateStore に格納されている（Verifier 含む）
	stateValue := q.Get("state")
	entry, err := states.Take(context.Background(), stateValue)
	if err != nil {
		t.Fatalf("state not stored: %v", err)
	}
	if entry.PrincipalID != pid {
		t.Errorf("entry.PrincipalID = %q, want %q", entry.PrincipalID, pid)
	}
	if entry.Verifier == "" {
		t.Errorf("entry.Verifier is empty (PKCE Verifier must be stored)")
	}
}

func TestStartHandler_NoPrincipal_401(t *testing.T) {
	t.Parallel()
	h, _, _ := buildHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth/kintone/start", nil)
	rw := httptest.NewRecorder()
	h.StartHandler().ServeHTTP(rw, req)

	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rw.Code)
	}
}

func TestStartHandler_RejectsNonGET(t *testing.T) {
	t.Parallel()
	h, _, _ := buildHandler(t)

	req := principalReq(http.MethodPost, "/oauth/kintone/start", nil, &idproxy.Principal{ID: "p"})
	rw := httptest.NewRecorder()
	h.StartHandler().ServeHTTP(rw, req)

	if rw.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rw.Code)
	}
}

// --- CallbackHandler tests ---

// startCallback は start → token mock 経由で callback まで実行するヘルパ。
func startCallback(t *testing.T, h *oauthcallback.Handler, principal *idproxy.Principal, tweak func(state, codeParam *string, cookieValue *string)) *httptest.ResponseRecorder {
	t.Helper()

	// 1. start でリダイレクトを取得し state と cookie を抽出
	startReq := principalReq(http.MethodGet, "/oauth/kintone/start", nil, principal)
	startRW := httptest.NewRecorder()
	h.StartHandler().ServeHTTP(startRW, startReq)
	if startRW.Code != http.StatusFound {
		t.Fatalf("start status = %d", startRW.Code)
	}
	loc, _ := url.Parse(startRW.Header().Get("Location"))
	state := loc.Query().Get("state")
	var stateCookieValue string
	for _, c := range startRW.Result().Cookies() {
		if c.Name == oauthcallback.StateCookieName {
			stateCookieValue = c.Value
		}
	}
	codeParam := "the-auth-code"
	if tweak != nil {
		tweak(&state, &codeParam, &stateCookieValue)
	}

	// 2. callback リクエストを構築
	cbURL := "/oauth/kintone/callback?code=" + url.QueryEscape(codeParam) + "&state=" + url.QueryEscape(state)
	cbReq := principalReq(http.MethodGet, cbURL, nil, principal)
	if stateCookieValue != "" {
		cbReq.AddCookie(&http.Cookie{Name: oauthcallback.StateCookieName, Value: stateCookieValue})
	}
	cbRW := httptest.NewRecorder()
	h.CallbackHandler().ServeHTTP(cbRW, cbReq)
	return cbRW
}

func TestCallbackHandler_Success(t *testing.T) {
	t.Parallel()
	formCh := make(chan url.Values, 1)
	ts := newTokenServer(t, http.StatusOK, map[string]any{
		"access_token":  "at-1",
		"refresh_token": "rt-1",
		"token_type":    "Bearer",
		"expires_in":    3600,
	}, formCh)
	defer ts.Close()

	h, tokens, _ := buildHandler(t, func(c *oauthcallback.HandlerConfig) {
		c.TokenEndpoint = ts.URL
	})

	pid := "issuer:user-1"
	rw := startCallback(t, h, &idproxy.Principal{ID: pid}, nil)
	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rw.Code, rw.Body.String())
	}

	// TokenStore に Put されている
	tok, err := tokens.Get(context.Background(), "example.cybozu.com", pid, store.AuthTypeOAuth)
	if err != nil {
		t.Fatalf("TokenStore.Get: %v", err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" {
		t.Errorf("token = %+v", tok)
	}

	// PKCE Verifier が送信されている（form の code_verifier）
	select {
	case form := <-formCh:
		if form.Get("code_verifier") == "" {
			t.Errorf("code_verifier missing from token request: %v", form)
		}
		if form.Get("grant_type") != "authorization_code" {
			t.Errorf("grant_type = %q", form.Get("grant_type"))
		}
		if form.Get("code") != "the-auth-code" {
			t.Errorf("code = %q", form.Get("code"))
		}
	case <-time.After(time.Second):
		t.Fatal("token server did not receive request")
	}

	// callback の cookie 削除確認
	var cleared bool
	for _, c := range rw.Result().Cookies() {
		if c.Name == oauthcallback.StateCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Errorf("state cookie was not cleared")
	}
}

func TestCallbackHandler_StateCookieMismatch_403(t *testing.T) {
	t.Parallel()
	h, _, _ := buildHandler(t)

	pid := "issuer:user-1"
	rw := startCallback(t, h, &idproxy.Principal{ID: pid}, func(state, _, cookieValue *string) {
		*cookieValue = "tampered-cookie"
	})
	if rw.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rw.Code)
	}
}

func TestCallbackHandler_PrincipalMismatch_403(t *testing.T) {
	t.Parallel()
	h, _, _ := buildHandler(t)

	// start は user-1、callback は user-2 で行う
	startReq := principalReq(http.MethodGet, "/oauth/kintone/start", nil, &idproxy.Principal{ID: "user-1"})
	startRW := httptest.NewRecorder()
	h.StartHandler().ServeHTTP(startRW, startReq)
	if startRW.Code != http.StatusFound {
		t.Fatalf("start status = %d", startRW.Code)
	}
	loc, _ := url.Parse(startRW.Header().Get("Location"))
	state := loc.Query().Get("state")
	cookies := startRW.Result().Cookies()
	cbReq := principalReq(http.MethodGet, "/oauth/kintone/callback?code=c&state="+url.QueryEscape(state), nil, &idproxy.Principal{ID: "user-2"})
	for _, c := range cookies {
		cbReq.AddCookie(c)
	}
	cbRW := httptest.NewRecorder()
	h.CallbackHandler().ServeHTTP(cbRW, cbReq)

	if cbRW.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", cbRW.Code)
	}
}

func TestCallbackHandler_StateExpired_400(t *testing.T) {
	t.Parallel()

	// TTL=10ms の StateStore に差し替える
	states := oauthcallback.NewMemoryStateStore(oauthcallback.WithTTL(10 * time.Millisecond))
	tokens := newFakeTokenStore()
	h, err := oauthcallback.NewHandler(oauthcallback.HandlerConfig{
		Domain: "example.cybozu.com", ClientID: "cid", ClientSecret: "csec",
		RedirectURL: "https://mcp.example.com/oauth/kintone/callback",
		ExternalURL: "https://mcp.example.com",
		States:      states, Tokens: tokens,
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	startReq := principalReq(http.MethodGet, "/oauth/kintone/start", nil, &idproxy.Principal{ID: "p"})
	startRW := httptest.NewRecorder()
	h.StartHandler().ServeHTTP(startRW, startReq)
	loc, _ := url.Parse(startRW.Header().Get("Location"))
	state := loc.Query().Get("state")
	cookies := startRW.Result().Cookies()

	// 待機して TTL 超過
	time.Sleep(50 * time.Millisecond)

	cbReq := principalReq(http.MethodGet, "/oauth/kintone/callback?code=c&state="+url.QueryEscape(state), nil, &idproxy.Principal{ID: "p"})
	for _, c := range cookies {
		cbReq.AddCookie(c)
	}
	cbRW := httptest.NewRecorder()
	h.CallbackHandler().ServeHTTP(cbRW, cbReq)

	if cbRW.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (state expired)", cbRW.Code)
	}
}

func TestCallbackHandler_TokenExchangeFails_502(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"x"}`))
	}))
	defer ts.Close()

	h, _, _ := buildHandler(t, func(c *oauthcallback.HandlerConfig) { c.TokenEndpoint = ts.URL })
	rw := startCallback(t, h, &idproxy.Principal{ID: "p"}, nil)
	if rw.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rw.Code)
	}
}

func TestCallbackHandler_ProviderError_400(t *testing.T) {
	t.Parallel()
	h, _, _ := buildHandler(t)
	req := principalReq(http.MethodGet, "/oauth/kintone/callback?error=access_denied", nil, &idproxy.Principal{ID: "p"})
	rw := httptest.NewRecorder()
	h.CallbackHandler().ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestCallbackHandler_MissingParams_400(t *testing.T) {
	t.Parallel()
	h, _, _ := buildHandler(t)
	// no state, no code
	req := principalReq(http.MethodGet, "/oauth/kintone/callback", nil, &idproxy.Principal{ID: "p"})
	rw := httptest.NewRecorder()
	h.CallbackHandler().ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

// HTML レスポンスに secret （access_token / refresh_token / code / verifier）が含まれない。
func TestCallbackHandler_ErrorResponseDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"secret-payload"}`))
	}))
	defer ts.Close()
	h, _, _ := buildHandler(t, func(c *oauthcallback.HandlerConfig) { c.TokenEndpoint = ts.URL })

	rw := startCallback(t, h, &idproxy.Principal{ID: "p"}, nil)
	body := rw.Body.String()
	for _, leak := range []string{"the-auth-code", "secret-payload"} {
		if strings.Contains(body, leak) {
			t.Errorf("response body contains %q: %s", leak, body)
		}
	}
}

// Logger hook に渡される attrs が access_token / refresh_token / code / verifier を含まない。
func TestCallbackHandler_LogDoesNotLeakSecrets(t *testing.T) {
	t.Parallel()

	type entry struct {
		level string
		msg   string
		attrs map[string]any
	}
	var (
		mu      sync.Mutex
		entries []entry
	)
	logger := func(level, msg string, attrs map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		entries = append(entries, entry{level: level, msg: msg, attrs: attrs})
	}

	// 401 を返す token server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer ts.Close()

	h, _, _ := buildHandler(t, func(c *oauthcallback.HandlerConfig) {
		c.TokenEndpoint = ts.URL
		c.Logger = logger
	})

	_ = startCallback(t, h, &idproxy.Principal{ID: "issuer:user-1-secret"}, nil)

	mu.Lock()
	defer mu.Unlock()
	for _, e := range entries {
		for k, v := range e.attrs {
			s, ok := v.(string)
			if !ok {
				continue
			}
			for _, leak := range []string{"the-auth-code", "rt-", "at-", "issuer:user-1-secret"} {
				if strings.Contains(s, leak) {
					t.Errorf("logged attr %s contains %q: %v", k, leak, e)
				}
			}
		}
	}
}

// --- ValidateRedirectURL tests ---

func TestValidateRedirectURL_HTTPSOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		redirectURL string
		externalURL string
		allow       bool
		wantErr     bool
	}{
		{"https ok", "https://mcp.example.com/oauth/kintone/callback", "https://mcp.example.com", false, false},
		{"http rejected", "http://mcp.example.com/oauth/kintone/callback", "http://mcp.example.com", false, true},
		{"unknown scheme", "ftp://x/oauth/kintone/callback", "https://x", false, true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := oauthcallback.ValidateRedirectURL(c.redirectURL, c.externalURL, c.allow)
			if (err != nil) != c.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}

func TestValidateRedirectURL_LocalhostWithEnvOptIn(t *testing.T) {
	t.Parallel()
	err := oauthcallback.ValidateRedirectURL(
		"http://localhost:8080/oauth/kintone/callback",
		"http://localhost:8080",
		true,
	)
	if err != nil {
		t.Errorf("localhost with allow=true should pass: %v", err)
	}

	err = oauthcallback.ValidateRedirectURL(
		"http://localhost:8080/oauth/kintone/callback",
		"http://localhost:8080",
		false,
	)
	if err == nil {
		t.Errorf("localhost without allow should fail")
	}
}

func TestValidateRedirectURL_ExternalURLMismatch_FailFast(t *testing.T) {
	t.Parallel()
	cases := []struct {
		redirectURL string
		externalURL string
	}{
		{"https://other.example.com/oauth/kintone/callback", "https://mcp.example.com"},
		{"https://mcp.example.com/oauth/wrong/callback", "https://mcp.example.com"},
		{"https://mcp.example.com/oauth/kintone/", "https://mcp.example.com"}, // path 不一致
	}
	for _, c := range cases {
		err := oauthcallback.ValidateRedirectURL(c.redirectURL, c.externalURL, false)
		if err == nil {
			t.Errorf("mismatch should fail: redirect=%q ext=%q", c.redirectURL, c.externalURL)
		}
	}
}

func TestValidateRedirectURL_ExternalURLMatch_OK(t *testing.T) {
	t.Parallel()
	err := oauthcallback.ValidateRedirectURL(
		"https://mcp.example.com/oauth/kintone/callback",
		"https://mcp.example.com",
		false,
	)
	if err != nil {
		t.Errorf("matching URLs should pass: %v", err)
	}
	// trailing slash 許容
	err = oauthcallback.ValidateRedirectURL(
		"https://mcp.example.com/oauth/kintone/callback",
		"https://mcp.example.com/",
		false,
	)
	if err != nil {
		t.Errorf("trailing-slash external URL should pass: %v", err)
	}
}

func TestValidateRedirectURL_EmptyInputs(t *testing.T) {
	t.Parallel()
	if err := oauthcallback.ValidateRedirectURL("", "https://x", false); err == nil {
		t.Errorf("empty redirect should fail")
	}
	if err := oauthcallback.ValidateRedirectURL("https://x/oauth/kintone/callback", "", false); err == nil {
		t.Errorf("empty external should fail")
	}
}
