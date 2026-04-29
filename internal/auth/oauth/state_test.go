package oauth_test

import (
	"io"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/auth/oauth"
)

// ST-1: GenerateState は 16 byte の rand → URL-safe Base64（パディングなし）で長さ 22 の文字列を返す。
func TestGenerateState_Length(t *testing.T) {
	t.Parallel()
	state, err := oauth.GenerateState(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 16 byte → Base64URL: ceil(16*4/3) = 22 文字（パディングなし）
	if len(state) != 22 {
		t.Errorf("state length: got %d, want 22", len(state))
	}
	// URL-safe 文字のみ
	if strings.ContainsAny(state, "+/=") {
		t.Errorf("state contains non-URL-safe chars: %q", state)
	}
}

// ST-2: rand がエラーを返すとエラーが伝播すること。
func TestGenerateState_RandError(t *testing.T) {
	t.Parallel()
	_, err := oauth.GenerateState(errReader{})
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// ST-3 (bonus): rand が EOF を返すとエラーが伝播すること。
func TestGenerateState_RandEOF(t *testing.T) {
	t.Parallel()
	_, err := oauth.GenerateState(strings.NewReader("")) // empty reader
	if err == nil {
		t.Error("expected error from empty reader, got nil")
	}
	_ = io.EOF // ensure io is used (for lint)
}
