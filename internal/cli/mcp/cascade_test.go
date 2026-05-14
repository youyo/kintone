package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/youyo/kintone/internal/idproxy"
	"github.com/youyo/kintone/internal/store"
	"github.com/youyo/kintone/internal/store/memory"
)

// fakeErrorTokenStore は Get が常に指定エラーを返す stub。
type fakeErrorTokenStore struct {
	err error
}

func (f *fakeErrorTokenStore) Get(_ context.Context, _, _ string, _ store.AuthType) (*store.Token, error) {
	return nil, f.err
}

func (f *fakeErrorTokenStore) Put(_ context.Context, _ store.Token) error { return nil }

func (f *fakeErrorTokenStore) Delete(_ context.Context, _, _ string, _ store.AuthType) error {
	return nil
}

func (f *fakeErrorTokenStore) ListByDomain(_ context.Context, _ string, _ store.AuthType) ([]*store.Token, error) {
	return nil, nil
}

func (f *fakeErrorTokenStore) Close() error { return nil }

// okHandler は next に到達したことを確認するためのシンプルなハンドラ。
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

const (
	testDomain   = "example.cybozu.com"
	testStartURL = "https://example.com/oauth/kintone/start"
	testPID      = "issuer:user-1"
)

// makeRequest は path と method、Accept ヘッダ、Principal を持つ *http.Request を返す。
func makeRequest(method, path, accept string, p *idproxy.Principal) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if p != nil {
		req = req.WithContext(idproxy.WithPrincipal(req.Context(), p))
	}
	return req
}

// TestEnsureKintoneOAuthConnected はテーブル駆動で cascade middleware の全ケースを検証する。
//
// kill switch テスト（E14）は t.Setenv を使うため t.Parallel() を使わない。
func TestEnsureKintoneOAuthConnected(t *testing.T) {
	principal := &idproxy.Principal{ID: testPID}

	// tokens に p1 が存在する store
	tokensFull := memory.NewTokenStore()
	_ = tokensFull.Put(context.Background(), store.Token{
		Domain:      testDomain,
		PrincipalID: testPID,
		AuthType:    store.AuthTypeOAuth,
		AccessToken: "tok",
	})

	// tokens が空の store（ErrNotFound を返す）
	tokensEmpty := memory.NewTokenStore()

	// Get が ErrNotFound 以外のエラーを返す store
	ioErr := errors.New("io error")
	tokensError := &fakeErrorTokenStore{err: ioErr}

	tests := []struct {
		name       string
		method     string
		path       string
		accept     string
		principal  *idproxy.Principal
		tokens     store.TokenStore
		wantStatus int
		wantLoc    string
		killSwitch bool // KINTONE_MCP_DISABLE_OAUTH_CASCADE=1 環境
	}{
		// 正常系
		{
			name:       "N1: GET / + html + Principal + tokens空 → 302",
			method:     http.MethodGet,
			path:       "/",
			accept:     "text/html,application/xhtml+xml",
			principal:  principal,
			tokens:     tokensEmpty,
			wantStatus: http.StatusFound,
			wantLoc:    testStartURL,
		},
		{
			name:       "N2: GET / + html + Principal + tokens有 → next(200)",
			method:     http.MethodGet,
			path:       "/",
			accept:     "text/html",
			principal:  principal,
			tokens:     tokensFull,
			wantStatus: http.StatusOK,
		},
		{
			name:       "N3: GET /static/foo + html + Principal + tokens空 → 302",
			method:     http.MethodGet,
			path:       "/static/foo",
			accept:     "text/html",
			principal:  principal,
			tokens:     tokensEmpty,
			wantStatus: http.StatusFound,
			wantLoc:    testStartURL,
		},
		// 異常系（条件を満たさないため next へ）
		{
			name:       "E1: POST / → next",
			method:     http.MethodPost,
			path:       "/",
			accept:     "text/html",
			principal:  principal,
			tokens:     tokensEmpty,
			wantStatus: http.StatusOK,
		},
		{
			name:       "E2: GET / + Accept:application/json → next",
			method:     http.MethodGet,
			path:       "/",
			accept:     "application/json",
			principal:  principal,
			tokens:     tokensEmpty,
			wantStatus: http.StatusOK,
		},
		{
			name:       "E3: GET / + Principal nil → next",
			method:     http.MethodGet,
			path:       "/",
			accept:     "text/html",
			principal:  nil,
			tokens:     tokensEmpty,
			wantStatus: http.StatusOK,
		},
		{
			name:       "E4a: GET /mcp + html + Principal + tokens空 → next",
			method:     http.MethodGet,
			path:       "/mcp",
			accept:     "text/html",
			principal:  principal,
			tokens:     tokensEmpty,
			wantStatus: http.StatusOK,
		},
		{
			name:       "E4b: GET /mcp/foo + html + Principal + tokens空 → next",
			method:     http.MethodGet,
			path:       "/mcp/foo",
			accept:     "text/html",
			principal:  principal,
			tokens:     tokensEmpty,
			wantStatus: http.StatusOK,
		},
		{
			name:       "E5: GET /oauth/kintone/start + html + Principal + tokens空 → next（無限ループ防止）",
			method:     http.MethodGet,
			path:       "/oauth/kintone/start",
			accept:     "text/html",
			principal:  principal,
			tokens:     tokensEmpty,
			wantStatus: http.StatusOK,
		},
		{
			name:       "E13: tokens.Get が非 ErrNotFound エラー → next（safe default）",
			method:     http.MethodGet,
			path:       "/",
			accept:     "text/html",
			principal:  principal,
			tokens:     tokensError,
			wantStatus: http.StatusOK,
		},
		{
			name:       "E14: kill switch ON → 全条件 OK でも next",
			method:     http.MethodGet,
			path:       "/",
			accept:     "text/html",
			principal:  principal,
			tokens:     tokensEmpty,
			wantStatus: http.StatusOK,
			killSwitch: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// kill switch テストは Setenv を使うため Parallel 禁止
			if !tc.killSwitch {
				t.Parallel()
			}

			if tc.killSwitch {
				t.Setenv("KINTONE_MCP_DISABLE_OAUTH_CASCADE", "1")
			}

			mw := EnsureKintoneOAuthConnected(tc.tokens, testDomain, testStartURL)
			handler := mw(okHandler)

			req := makeRequest(tc.method, tc.path, tc.accept, tc.principal)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantLoc != "" {
				loc := w.Header().Get("Location")
				if loc != tc.wantLoc {
					t.Errorf("Location = %q, want %q", loc, tc.wantLoc)
				}
			}
		})
	}
}
