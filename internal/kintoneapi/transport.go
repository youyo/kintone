package kintoneapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxRawBodyLen は APIError.RawBody の最大保持バイト数。
const maxRawBodyLen = 4096

// RetryPolicy はリトライ戦略。MaxAttempts<=0 ならデフォルトに置換。
type RetryPolicy struct {
	// MaxAttempts は初回 + リトライの合計試行回数（>=1 が有効）。
	MaxAttempts int
	// BaseBackoff は指数バックオフの初期値（base * 2^(n-1)）。
	BaseBackoff time.Duration
	// MaxBackoff はバックオフの上限値。
	MaxBackoff time.Duration
	// RetryOn はリトライ対象 HTTP ステータス。空ならデフォルト [429, 503]。
	RetryOn []int
}

// DefaultRetryPolicy は推奨デフォルト。
var DefaultRetryPolicy = RetryPolicy{
	MaxAttempts: 3,
	BaseBackoff: 500 * time.Millisecond,
	MaxBackoff:  5 * time.Second,
	RetryOn:     []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
}

// shouldRetry は HTTP ステータスがリトライ対象か判定する。
// retryOn が空（zero policy）の場合はデフォルト [429,503] を採用。
func shouldRetry(status int, retryOn []int) bool {
	if len(retryOn) == 0 {
		retryOn = DefaultRetryPolicy.RetryOn
	}
	for _, s := range retryOn {
		if s == status {
			return true
		}
	}
	return false
}

// backoff は attempt 番目の待機時間を計算する（attempt は 1 起算）。
// retryAfter > 0 ならそれを優先（max でクランプ）。
func backoff(attempt int, retryAfter time.Duration, base, maxBackoff time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxBackoff {
			return maxBackoff
		}
		return retryAfter
	}
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > maxBackoff {
			return maxBackoff
		}
	}
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// doJSON は GET リクエストを実行し、レスポンス body を out にデコードする。
// out が nil ならデコードしない。
//
// エラー戦略:
//   - 2xx: out にデコードして nil
//   - 4xx/5xx (リトライ対象外 or 最終 attempt): *APIError
//   - リトライ対象 (4xx/5xx): policy.MaxAttempts まで再送、最終的に *APIError
//   - net error: ctx 起因なら ctx.Err、その他は wrap して返す
func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		u = u + "?" + encoded
	}

	policy := c.retry

	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		// ctx pre-check
		if err := ctx.Err(); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, method, u, http.NoBody)
		if err != nil {
			return fmt.Errorf("kintoneapi: build request: %w", err)
		}
		if err := c.auth.Apply(ctx, req); err != nil {
			return fmt.Errorf("kintoneapi: auth apply: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// ctx 起因は即時返却
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			lastErr = fmt.Errorf("kintoneapi: http do: %w", err)
			// timeout 的な net error はリトライ対象とする
			if attempt < policy.MaxAttempts && isTransientNetError(err) {
				c.sleep(backoff(attempt, 0, policy.BaseBackoff, policy.MaxBackoff))
				continue
			}
			return lastErr
		}

		// 2xx: 成功
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer func() { _ = resp.Body.Close() }()
			if out == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				return nil
			}
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return fmt.Errorf("kintoneapi: read body: %w", readErr)
			}
			if len(body) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("kintoneapi: decode body: %w", err)
			}
			return nil
		}

		// 非 2xx: APIError 構築
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		retryAfter := parseRetryAfter(resp.Header, c.now)

		// リトライ対象 & 残り試行あり
		if shouldRetry(resp.StatusCode, policy.RetryOn) && attempt < policy.MaxAttempts {
			c.sleep(backoff(attempt, retryAfter, policy.BaseBackoff, policy.MaxBackoff))
			continue
		}

		return buildAPIError(resp.StatusCode, body, retryAfter)
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("kintoneapi: retry exhausted")
}

// buildAPIError は kintone エラーレスポンス body から APIError を組み立てる。
func buildAPIError(status int, body []byte, retryAfter time.Duration) *APIError {
	e := &APIError{
		HTTPStatus: status,
		RawBody:    truncateBody(body, maxRawBodyLen),
		RetryAfter: retryAfter,
	}
	if len(body) > 0 && bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		var parsed struct {
			Code    string `json:"code"`
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &parsed); err == nil {
			e.Code = parsed.Code
			e.ID = parsed.ID
			e.Message = parsed.Message
		}
	}
	if e.Message == "" {
		// JSON でない場合は body を message に流用（短ければ）
		trimmed := strings.TrimSpace(string(truncateBody(body, 256)))
		if trimmed != "" {
			e.Message = trimmed
		} else {
			e.Message = http.StatusText(status)
		}
	}
	e.Category = classify(status, e.Code)
	return e
}

// isWriteMethod は HTTP method が body を伴う書き込み系（POST/PUT/PATCH/DELETE）か判定する。
// kintone REST の DELETE はリクエスト body に JSON を載せるため write 系に分類する。
func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// doJSONWithBody は body を伴う非冪等リクエスト（POST/PUT/DELETE 等）を実行する。
//
// 設計判断:
//   - body は json.Marshal で 1 回エンコードし、attempt ごとに bytes.NewReader で再生成（retry 対応）
//   - body=nil なら http.NoBody を送り、Content-Type ヘッダは付与しない
//   - 共通動作（auth, UA, APIError 構築, ctx 取り扱い）は doJSON と揃える
//   - **書き込み系（POST/PUT/PATCH/DELETE）はデフォルトで MaxAttempts=1**（advisor 指摘 #3）。
//     idempotency 保証がないため body 再送による多重作成リスクを回避する。
//     上位がリトライしたい場合は doJSONWithBodyWithPolicy を直接使う。
func (c *Client) doJSONWithBody(ctx context.Context, method, path string, body any, out any) error {
	policy := c.retry
	if isWriteMethod(method) {
		policy.MaxAttempts = 1
	}
	return c.doJSONWithBodyWithPolicy(ctx, method, path, body, out, policy)
}

// doJSONWithBodyWithPolicy は doJSONWithBody の policy 上書き版。テスト用途中心。
func (c *Client) doJSONWithBodyWithPolicy(ctx context.Context, method, path string, body any, out any, policy RetryPolicy) error {
	u := c.baseURL + path
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}

	// body を 1 回だけマーシャル。attempt ごとに bytes.Reader を再生成する。
	var encoded []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kintoneapi: marshal body: %w", err)
		}
		encoded = b
	}

	var lastErr error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		var bodyReader io.Reader = http.NoBody
		if encoded != nil {
			bodyReader = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
		if err != nil {
			return fmt.Errorf("kintoneapi: build request: %w", err)
		}
		if err := c.auth.Apply(ctx, req); err != nil {
			return fmt.Errorf("kintoneapi: auth apply: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if encoded != nil {
			req.Header.Set("Content-Type", "application/json")
			req.ContentLength = int64(len(encoded))
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			lastErr = fmt.Errorf("kintoneapi: http do: %w", err)
			if attempt < policy.MaxAttempts && isTransientNetError(err) {
				c.sleep(backoff(attempt, 0, policy.BaseBackoff, policy.MaxBackoff))
				continue
			}
			return lastErr
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer func() { _ = resp.Body.Close() }()
			if out == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				return nil
			}
			respBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return fmt.Errorf("kintoneapi: read body: %w", readErr)
			}
			if len(respBody) == 0 {
				return nil
			}
			if err := json.Unmarshal(respBody, out); err != nil {
				return fmt.Errorf("kintoneapi: decode body: %w", err)
			}
			return nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		retryAfter := parseRetryAfter(resp.Header, c.now)
		if shouldRetry(resp.StatusCode, policy.RetryOn) && attempt < policy.MaxAttempts {
			c.sleep(backoff(attempt, retryAfter, policy.BaseBackoff, policy.MaxBackoff))
			continue
		}
		return buildAPIError(resp.StatusCode, respBody, retryAfter)
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("kintoneapi: retry exhausted")
}

// isTransientNetError は net error が一時的（タイムアウト等）かを判定する。
func isTransientNetError(err error) bool {
	if err == nil {
		return false
	}
	type timeoutErr interface{ Timeout() bool }
	if te, ok := err.(timeoutErr); ok {
		return te.Timeout()
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Timeout()
	}
	return false
}
