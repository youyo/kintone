package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// PKCEPair は code_verifier と challenge のペア。
// RFC 7636 (Proof Key for Code Exchange) の S256 方式に従う。
type PKCEPair struct {
	Verifier  string // 43〜128 文字の URL-safe Base64 文字列
	Challenge string // BASE64URL(SHA256(verifier))、パディングなし
	Method    string // "S256" 固定
}

// GeneratePKCE は crypto/rand から PKCE ペアを生成する。
//
// rand が nil の場合は crypto/rand.Reader を使用する。
// verifier は 32 byte のランダムバイト列を Base64URL エンコードした 43 文字の文字列。
// challenge は SHA-256(verifier) を Base64URL エンコードした文字列。
func GeneratePKCE(randReader io.Reader) (PKCEPair, error) {
	if randReader == nil {
		randReader = rand.Reader
	}

	buf := make([]byte, 32)
	if _, err := io.ReadFull(randReader, buf); err != nil {
		return PKCEPair{}, fmt.Errorf("oauth: pkce: read random bytes: %w", err)
	}

	verifier := base64.RawURLEncoding.EncodeToString(buf)

	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	return PKCEPair{
		Verifier:  verifier,
		Challenge: challenge,
		Method:    "S256",
	}, nil
}
