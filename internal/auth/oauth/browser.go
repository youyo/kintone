package oauth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// commandRunner は exec.Command 相当の関数型（テスト用モック注入）。
type commandRunner func(name string, args ...string) error

// NewOpenBrowserWithCommand は指定の commandRunner を使うブラウザ起動関数を返す。
// テスト時に commandRunner をモックすることで実際のブラウザ起動を回避できる。
func NewOpenBrowserWithCommand(runner commandRunner) func(url string) error {
	return func(url string) error {
		name, args := browserCommand(url)
		return runner(name, args...)
	}
}

// DefaultOpenBrowser はデフォルトのブラウザ起動関数。
// runtime.GOOS に応じて OS 別の open コマンドを実行する。
func DefaultOpenBrowser(url string) error {
	return NewOpenBrowserWithCommand(func(name string, args ...string) error {
		cmd := exec.Command(name, args...) //nolint:gosec
		return cmd.Start()
	})(url)
}

// browserCommand は OS に応じた open コマンドと引数を返す。
func browserCommand(url string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "cmd", []string{"/c", "start", url}
	default: // linux, etc.
		return "xdg-open", []string{url}
	}
}

// buildAuthorizeURL は kintone の authorization エンドポイント URL を構築する。
// PKCE と state を含む。
func buildAuthorizeURL(domain, clientID, redirectURL, scope, state string, pkce PKCEPair, pkceDisabled bool) string {
	base := fmt.Sprintf("https://%s/oauth2/authorization", domain)
	params := fmt.Sprintf(
		"?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&state=%s",
		urlEncode(clientID),
		urlEncode(redirectURL),
		urlEncode(scope),
		urlEncode(state),
	)
	if !pkceDisabled {
		params += fmt.Sprintf(
			"&code_challenge=%s&code_challenge_method=%s",
			urlEncode(pkce.Challenge),
			urlEncode(pkce.Method),
		)
	}
	return base + params
}

// urlEncode は RFC 3986 の unreserved 文字以外をパーセントエンコードする簡易実装。
// net/url.QueryEscape は '+' をスペースとして扱うため RawURLEncoding を直接使う。
func urlEncode(s string) string {
	return (&urlEncoder{}).encode(s)
}

// urlEncoder は net/url を使ったパーセントエンコーダ。
type urlEncoder struct{}

func (u *urlEncoder) encode(s string) string {
	encoded := make([]byte, 0, len(s)*3)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			encoded = append(encoded, c)
		} else {
			encoded = append(encoded, '%', hexchar(c>>4), hexchar(c&0xF))
		}
	}
	return string(encoded)
}

func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}

func hexchar(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + (n - 10)
}
