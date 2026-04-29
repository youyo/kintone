package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// GenerateState は 16 byte の crypto/rand を URL-safe Base64（パディングなし）で返す。
//
// rand が nil の場合は crypto/rand.Reader を使用する。
// 出力は 22 文字の文字列（16 byte → ceil(16*4/3)=22）。
// CSRF 対策として authorization リクエストに付与し、callback で完全一致検証する。
func GenerateState(randReader io.Reader) (string, error) {
	if randReader == nil {
		randReader = rand.Reader
	}

	buf := make([]byte, 16)
	if _, err := io.ReadFull(randReader, buf); err != nil {
		return "", fmt.Errorf("oauth: state: read random bytes: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
