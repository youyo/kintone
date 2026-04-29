package oauth_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/auth/oauth"
)

// PK-1: verifier の長さが 43〜128 文字であること。
func TestGeneratePKCE_VerifierLength(t *testing.T) {
	t.Parallel()
	pair, err := oauth.GeneratePKCE(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pair.Verifier) < 43 || len(pair.Verifier) > 128 {
		t.Errorf("verifier length %d is out of range [43, 128]", len(pair.Verifier))
	}
}

// PK-2: challenge は SHA256(verifier) を URL-safe Base64（パディングなし）にエンコードした値であること。
func TestGeneratePKCE_ChallengeIsS256(t *testing.T) {
	t.Parallel()
	pair, err := oauth.GeneratePKCE(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 手動で検証
	h := sha256.Sum256([]byte(pair.Verifier))
	want := base64.RawURLEncoding.EncodeToString(h[:])
	if pair.Challenge != want {
		t.Errorf("challenge mismatch: got %q, want %q", pair.Challenge, want)
	}
	// URL-safe 文字のみ（'+', '/', '=' を含まない）
	if strings.ContainsAny(pair.Challenge, "+/=") {
		t.Errorf("challenge contains non-URL-safe chars: %q", pair.Challenge)
	}
}

// PK-3: method は "S256" 固定であること。
func TestGeneratePKCE_MethodIsS256(t *testing.T) {
	t.Parallel()
	pair, err := oauth.GeneratePKCE(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.Method != "S256" {
		t.Errorf("method: got %q, want %q", pair.Method, "S256")
	}
}

// PK-4: 同じ rand バイト列から同じ verifier/challenge が生成されること（決定論性）。
func TestGeneratePKCE_Deterministic(t *testing.T) {
	t.Parallel()
	// 32 byte のゼロ埋め固定バイト列
	fixed := bytes.Repeat([]byte{0xAB}, 32)

	pair1, err := oauth.GeneratePKCE(bytes.NewReader(fixed))
	if err != nil {
		t.Fatalf("pair1: unexpected error: %v", err)
	}
	pair2, err := oauth.GeneratePKCE(bytes.NewReader(fixed))
	if err != nil {
		t.Fatalf("pair2: unexpected error: %v", err)
	}
	if pair1.Verifier != pair2.Verifier {
		t.Errorf("verifier non-deterministic: %q != %q", pair1.Verifier, pair2.Verifier)
	}
	if pair1.Challenge != pair2.Challenge {
		t.Errorf("challenge non-deterministic: %q != %q", pair1.Challenge, pair2.Challenge)
	}
}

// PK-5: rand が io.EOF を返すとエラーが伝播すること。
func TestGeneratePKCE_RandError(t *testing.T) {
	t.Parallel()
	_, err := oauth.GeneratePKCE(errReader{})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// errReader は常に io.EOF を返すテスト用 io.Reader。
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) { return 0, io.EOF }
