package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TokenResponse は token endpoint からのレスポンスを表す。
type TokenResponse struct {
	AccessToken  string    // access_token
	RefreshToken string    // refresh_token（rotation 時は新値）
	ExpiresIn    int       // expires_in（秒）
	ExpiresAt    time.Time // Now + ExpiresIn
	TokenType    string    // "Bearer"
	Scope        string    // スペース区切りのスコープ文字列
}

// tokenEndpointResponse は JSON デコード用の内部型。
type tokenEndpointResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	// エラーレスポンス用
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// ExchangeCodeRequest は authorization_code グラントのパラメータ。
type ExchangeCodeRequest struct {
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
	Code          string
	RedirectURL   string
	CodeVerifier  string // PKCE。空文字なら送信しない
	HTTPClient    *http.Client
	Now           func() time.Time
	Sleep         func(time.Duration) // リトライ間の sleep（テスト用）
}

// RefreshTokenRequest は refresh_token グラントのパラメータ。
type RefreshTokenRequest struct {
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
	RefreshToken  string
	HTTPClient    *http.Client
	Now           func() time.Time
	Sleep         func(time.Duration) // リトライ間の sleep（テスト用）
}

// ExchangeCode は authorization_code グラントでトークンを取得する。
// 5xx の場合は 1 回のみリトライする。
func ExchangeCode(ctx context.Context, req ExchangeCodeRequest) (*TokenResponse, error) {
	httpClient := req.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := req.Now
	if now == nil {
		now = time.Now
	}
	sleepFn := req.Sleep
	if sleepFn == nil {
		sleepFn = time.Sleep
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", req.Code)
	form.Set("redirect_uri", req.RedirectURL)
	if req.CodeVerifier != "" {
		form.Set("code_verifier", req.CodeVerifier)
	}

	return doTokenRequest(ctx, tokenRequestParams{
		endpoint:     req.TokenEndpoint,
		clientID:     req.ClientID,
		clientSecret: req.ClientSecret,
		form:         form,
		httpClient:   httpClient,
		now:          now,
		sleep:        sleepFn,
	})
}

// RefreshToken は refresh_token グラントで新しいトークンを取得する。
// 5xx の場合は 1 回のみリトライする。
func RefreshToken(ctx context.Context, req RefreshTokenRequest) (*TokenResponse, error) {
	httpClient := req.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	now := req.Now
	if now == nil {
		now = time.Now
	}
	sleepFn := req.Sleep
	if sleepFn == nil {
		sleepFn = time.Sleep
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", req.RefreshToken)

	return doTokenRequest(ctx, tokenRequestParams{
		endpoint:     req.TokenEndpoint,
		clientID:     req.ClientID,
		clientSecret: req.ClientSecret,
		form:         form,
		httpClient:   httpClient,
		now:          now,
		sleep:        sleepFn,
	})
}

// tokenRequestParams は doTokenRequest への入力。
type tokenRequestParams struct {
	endpoint     string
	clientID     string
	clientSecret string
	form         url.Values
	httpClient   *http.Client
	now          func() time.Time
	sleep        func(time.Duration)
}

// doTokenRequest は token endpoint に POST し、5xx のとき最大 1 回リトライする。
func doTokenRequest(ctx context.Context, p tokenRequestParams) (*TokenResponse, error) {
	const maxRetry = 1

	var lastErr error
	for attempt := 0; attempt <= maxRetry; attempt++ {
		if attempt > 0 {
			// ジッタ付きの簡易バックオフ（500ms 固定で十分）
			p.sleep(500 * time.Millisecond)
		}

		resp, serverErr, clientErr := sendTokenRequest(ctx, p)
		if clientErr != nil {
			// ネットワークエラー / 4xx はリトライしない（冪等性が保証されない）
			return nil, clientErr
		}
		if serverErr != nil {
			// 5xx → lastErr を更新してリトライ
			lastErr = serverErr
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// sendTokenRequest は 1 回分の POST を実行する。
// 戻り値:
//   - (resp, nil, nil): 成功
//   - (nil, oauthErr, nil): 5xx（リトライ可能）
//   - (nil, nil, err): ネットワークエラー / 4xx（リトライ不可）
func sendTokenRequest(ctx context.Context, p tokenRequestParams) (resp *TokenResponse, serverErr *OAuthError, clientErr error) {
	reqBody := p.form.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, strings.NewReader(reqBody))
	if err != nil {
		return nil, nil, fmt.Errorf("oauth: build token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// kintone は HTTP Basic 認証必須
	httpReq.SetBasicAuth(p.clientID, p.clientSecret)

	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = httpResp.Body.Close() }()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("oauth: read token response: %w", err)
	}

	// 5xx → serverErr を返してリトライを促す
	if httpResp.StatusCode >= 500 {
		var errResp tokenEndpointResponse
		_ = json.Unmarshal(body, &errResp)
		code := errResp.Error
		if code == "" {
			code = "server_error"
		}
		return nil, &OAuthError{
			Code:        code,
			Description: errResp.ErrorDescription,
			HTTPStatus:  httpResp.StatusCode,
		}, nil
	}

	var parsed tokenEndpointResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, nil, fmt.Errorf("oauth: parse token response: %w", err)
	}

	// エラーレスポンス（4xx 系）
	if httpResp.StatusCode >= 400 || parsed.Error != "" {
		return nil, nil, &OAuthError{
			Code:        parsed.Error,
			Description: parsed.ErrorDescription,
			HTTPStatus:  httpResp.StatusCode,
		}
	}

	now := p.now()
	return &TokenResponse{
		AccessToken:  parsed.AccessToken,
		RefreshToken: parsed.RefreshToken,
		ExpiresIn:    parsed.ExpiresIn,
		ExpiresAt:    now.Add(time.Duration(parsed.ExpiresIn) * time.Second),
		TokenType:    parsed.TokenType,
		Scope:        parsed.Scope,
	}, nil, nil
}
