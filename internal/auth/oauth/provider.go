package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/youyo/kintone/internal/tokenstore"
)

// RefresherInterface は Refresh メソッドを持つ任意の型を受け付けるインターフェース。
// テスト時に mockRefresher を注入できるようにする。
type RefresherInterface interface {
	Refresh(ctx context.Context, oldRefreshToken string) (*Result, error)
}

// Authenticator は kintone API リクエストに Authorization: Bearer を付与する。
// TokenStore から最新トークンを取得し、期限切れなら自動で refresh する。
//
// auth.Authenticator interface を実装する。
type Authenticator struct {
	store       tokenstore.Store
	domain      string
	principalID string
	refresher   RefresherInterface
	skew        time.Duration // 0 なら 60s
	now         func() time.Time
	mu          sync.Mutex
}

// NewAuthenticator は OAuth 用 Authenticator を構築する。
//
// store は M07 tokenstore、refresher は *Refresher または テスト用 mock。
// refresher が nil の場合、refresh 機能は無効（access_token 期限切れ時はエラー）。
// now が nil の場合は time.Now を使用する。
func NewAuthenticator(store tokenstore.Store, domain, principalID string, refresher RefresherInterface, now func() time.Time) *Authenticator {
	if now == nil {
		now = time.Now
	}
	return &Authenticator{
		store:       store,
		domain:      domain,
		principalID: principalID,
		refresher:   refresher,
		skew:        60 * time.Second,
		now:         now,
	}
}

// Apply は req に "Authorization: Bearer <access_token>" を付与する。
//
// access_token が期限切れ（または skew 内）の場合、refresh を試みた上でセットする。
// auth.Authenticator interface を満たす。
func (a *Authenticator) Apply(ctx context.Context, req *http.Request) error {
	if req == nil {
		return errors.New("oauth: authenticator: request is nil")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// TokenStore から最新トークンを取得（lock 内で毎回取得して二重チェック）
	tok, err := a.store.Get(ctx, a.domain, a.principalID, tokenstore.AuthTypeOAuth)
	if err != nil {
		return fmt.Errorf("oauth: authenticator: get token: %w", err)
	}

	// access_token が有効か確認（skew を考慮）
	if a.isValid(tok) {
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		return nil
	}

	// 期限切れ → refresh
	if a.refresher == nil {
		return ErrTokenExpired
	}
	if tok.RefreshToken == "" {
		return ErrTokenExpired
	}

	newResult, err := a.refresher.Refresh(ctx, tok.RefreshToken)
	if err != nil {
		return err
	}

	// TokenStore を更新
	updatedTok := tokenstore.Token{
		Domain:       a.domain,
		PrincipalID:  a.principalID,
		AuthType:     tokenstore.AuthTypeOAuth,
		AccessToken:  newResult.AccessToken,
		RefreshToken: newResult.RefreshToken,
		ExpiresAt:    newResult.ExpiresAt,
		UpdatedAt:    a.now(),
	}
	if putErr := a.store.Put(ctx, updatedTok); putErr != nil {
		// Put 失敗でも今回のリクエストは続行（次回の Apply で再取得される）
		return fmt.Errorf("oauth: authenticator: put updated token: %w", putErr)
	}

	req.Header.Set("Authorization", "Bearer "+newResult.AccessToken)
	return nil
}

// isValid は access_token が有効（期限切れでなく、skew 内でもない）かを確認する。
func (a *Authenticator) isValid(tok *tokenstore.Token) bool {
	if tok.ExpiresAt.IsZero() {
		// expires_at が未設定なら有効とみなす
		return true
	}
	skew := a.skew
	if skew <= 0 {
		skew = 60 * time.Second
	}
	// now + skew < expires_at → 有効
	return a.now().Add(skew).Before(tok.ExpiresAt)
}
