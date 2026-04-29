package idproxy

import (
	"strings"
	"testing"
)

const validHexSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" // 64 hex = 32 bytes

func TestEnvValidate_OK_Https(t *testing.T) {
	t.Parallel()
	e := Env{
		Issuer:       "https://accounts.google.com",
		ClientID:     "client",
		ExternalURL:  "https://mcp.example.com",
		CookieSecret: validHexSecret,
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(e.CookieSecretDecoded) != 32 {
		t.Fatalf("CookieSecretDecoded len: got %d want 32", len(e.CookieSecretDecoded))
	}
}

func TestEnvValidate_OK_LocalhostHTTP(t *testing.T) {
	t.Parallel()
	e := Env{Issuer: "https://i", ClientID: "c", ExternalURL: "http://localhost:8080", CookieSecret: validHexSecret}
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate localhost http should be ok: %v", err)
	}
}

func TestEnvValidate_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		env     Env
		wantSub string
	}{
		{"missing issuer", Env{ClientID: "c", ExternalURL: "https://x", CookieSecret: validHexSecret}, "ISSUER"},
		{"missing client id", Env{Issuer: "https://i", ExternalURL: "https://x", CookieSecret: validHexSecret}, "CLIENT_ID"},
		{"missing external url", Env{Issuer: "https://i", ClientID: "c", CookieSecret: validHexSecret}, "EXTERNAL_URL"},
		{"http non-localhost external url", Env{Issuer: "https://i", ClientID: "c", ExternalURL: "http://example.com", CookieSecret: validHexSecret}, "https://"},
		{"missing cookie secret", Env{Issuer: "https://i", ClientID: "c", ExternalURL: "https://x"}, "COOKIE_SECRET"},
		{"invalid hex cookie secret", Env{Issuer: "https://i", ClientID: "c", ExternalURL: "https://x", CookieSecret: "not-hex!"}, "valid hex"},
		{"short cookie secret", Env{Issuer: "https://i", ClientID: "c", ExternalURL: "https://x", CookieSecret: "deadbeef"}, "32 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.env.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}
