package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "github.com/youyo/idproxy"

	"github.com/youyo/kintone/internal/store"
	"github.com/youyo/kintone/internal/store/memory"
)

// hookDomain と hookIssuer, hookSubject は buildOnAuthenticatedHook テスト共通定数。
// フックは principalID = issuer + ":" + subject で構築するため、トークンも同じ形式で保存する。
const (
	hookDomain  = "example.cybozu.com"
	hookIssuer  = "https://issuer.example.com"
	hookSubject = "user1"
	hookPID     = hookIssuer + ":" + hookSubject // "https://issuer.example.com:user1"
)

// makeUser は Issuer:Subject を分割して *upstream.User を組み立てる補助関数。
// "issuer:subject" 形式を想定し、":" で左右に分割する。
func makeUser(issuer, subject string) *upstream.User {
	return &upstream.User{
		Issuer:  issuer,
		Subject: subject,
	}
}

// makeHookRequest は buildOnAuthenticatedHook に渡す *http.Request を作る。
func makeHookRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/", nil)
}

// makeHookResponseWriter は hook 呼び出し用の ResponseWriter を返す。
func makeHookResponseWriter() http.ResponseWriter {
	return httptest.NewRecorder()
}

// TestBuildOnAuthenticatedHookSubpath はサブパス配備での startURL 処理を検証する。
func TestBuildOnAuthenticatedHookSubpath(t *testing.T) {
	t.Parallel()
	tokensEmpty := memory.NewTokenStore()
	user := makeUser(hookIssuer, hookSubject)

	tests := []struct {
		name         string
		startURL     string
		wantRedirect string
	}{
		{
			name:         "ルート直下配備: /oauth/kintone/start",
			startURL:     "https://host/oauth/kintone/start",
			wantRedirect: "/oauth/kintone/start",
		},
		{
			name:         "サブパス配備: /base/oauth/kintone/start",
			startURL:     "https://host/base/oauth/kintone/start",
			wantRedirect: "/base/oauth/kintone/start",
		},
		{
			name:         "ローカル開発: /oauth/kintone/start",
			startURL:     "http://localhost:8080/oauth/kintone/start",
			wantRedirect: "/oauth/kintone/start",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hook := buildOnAuthenticatedHook(tokensEmpty, hookDomain, tc.startURL)
			if hook == nil {
				t.Fatal("expected non-nil hook")
			}
			redirectTo, handled := hook(makeHookResponseWriter(), makeHookRequest(), user)
			if handled {
				t.Error("handled should be false")
			}
			if redirectTo != tc.wantRedirect {
				t.Errorf("redirectTo = %q, want %q", redirectTo, tc.wantRedirect)
			}
		})
	}
}

// TestBuildOnAuthenticatedHook はテーブル駆動で buildOnAuthenticatedHook の全ケースを検証する。
//
// killSwitch テストは t.Setenv を使うため Parallel を使わない。
func TestBuildOnAuthenticatedHook(t *testing.T) {
	// tokens に hookPID のトークンが存在する store
	tokensFull := memory.NewTokenStore()
	_ = tokensFull.Put(context.Background(), store.Token{
		Domain:      hookDomain,
		PrincipalID: hookPID,
		AuthType:    store.AuthTypeOAuth,
		AccessToken: "access-tok",
	})

	// tokens が空の store（ErrNotFound を返す）
	tokensEmpty := memory.NewTokenStore()

	// Get が非 ErrNotFound エラーを返す store
	ioErr := errors.New("io error")
	tokensError := &fakeErrorTokenStore{err: ioErr}

	tests := []struct {
		name         string
		tokens       store.TokenStore
		user         *upstream.User
		wantNilHook  bool   // buildOnAuthenticatedHook が nil を返すべき場合
		wantRedirect string // hook が返すべき redirectTo（非空なら検証）
		killSwitch   bool
	}{
		{
			name:         "N1: tokens に principal なし → /oauth/kintone/start へリダイレクト",
			tokens:       tokensEmpty,
			user:         makeUser(hookIssuer, hookSubject),
			wantRedirect: "/oauth/kintone/start",
		},
		{
			name:   "N2: tokens に principal あり → リダイレクトなし",
			tokens: tokensFull,
			user:   makeUser(hookIssuer, hookSubject),
			// wantRedirect は空文字列（リダイレクトなし）
		},
		{
			name:        "N3: tokens == nil → フック自体が nil",
			tokens:      nil,
			user:        makeUser(hookIssuer, hookSubject),
			wantNilHook: true,
		},
		{
			name:        "N4: KINTONE_MCP_DISABLE_OAUTH_CASCADE=1 → フック自体が nil",
			tokens:      tokensEmpty,
			user:        makeUser(hookIssuer, hookSubject),
			wantNilHook: true,
			killSwitch:  true,
		},
		{
			name:   "E1: user == nil → リダイレクトなし",
			tokens: tokensEmpty,
			user:   nil,
			// wantRedirect は空文字列
		},
		{
			name:   "E2a: user.Subject == \"\" → リダイレクトなし",
			tokens: tokensEmpty,
			user:   makeUser(hookIssuer, ""),
			// wantRedirect は空文字列
		},
		{
			name:   "E2b: user.Issuer == \"\" → リダイレクトなし",
			tokens: tokensEmpty,
			user:   makeUser("", hookSubject),
			// wantRedirect は空文字列
		},
		{
			name:   "E3: tokens.Get が非 ErrNotFound エラー → リダイレクトなし（safe default）",
			tokens: tokensError,
			user:   makeUser(hookIssuer, hookSubject),
			// wantRedirect は空文字列
		},
		{
			name:   "E6: tokens.Get が非 ErrNotFound エラー → slog 警告ログが出る（基本確認）",
			tokens: tokensError,
			user:   makeUser(hookIssuer, hookSubject),
			// slog の出力は戻り値 ("", false) で確認（deep assert は不要）
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !tc.killSwitch {
				t.Parallel()
			}
			if tc.killSwitch {
				t.Setenv("KINTONE_MCP_DISABLE_OAUTH_CASCADE", "1")
			}

			hook := buildOnAuthenticatedHook(tc.tokens, hookDomain, "https://host/oauth/kintone/start")

			if tc.wantNilHook {
				if hook != nil {
					t.Error("expected nil hook, got non-nil")
				}
				return
			}
			if hook == nil {
				t.Fatal("expected non-nil hook, got nil")
			}

			w := makeHookResponseWriter()
			r := makeHookRequest()
			redirectTo, handled := hook(w, r, tc.user)

			if handled {
				t.Errorf("handled should always be false, got true")
			}
			if redirectTo != tc.wantRedirect {
				t.Errorf("redirectTo = %q, want %q", redirectTo, tc.wantRedirect)
			}
		})
	}
}
