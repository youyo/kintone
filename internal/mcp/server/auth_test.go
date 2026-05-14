package server

import (
	"strings"
	"testing"
)

func TestParseAuthMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    AuthMode
		wantErr bool
	}{
		{"", AuthModeNone, false},
		{"none", AuthModeNone, false},
		{"oidc", AuthModeOIDC, false},
		{"basic", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAuthMode(tt.in)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestParseAuthZMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    AuthZMode
		wantErr bool
	}{
		{"", AuthZModeAPIToken, false},
		{"api-token", AuthZModeAPIToken, false},
		{"oauth", AuthZModeOAuth, false},
		{"jwt", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAuthZMode(tt.in)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestPickServeMode(t *testing.T) {
	t.Parallel()
	if got := PickServeMode(""); got != ServeModeStdio {
		t.Errorf("empty addr should be stdio, got %v", got)
	}
	if got := PickServeMode(":8080"); got != ServeModeHTTP {
		t.Errorf("non-empty addr should be http, got %v", got)
	}
}

func TestValidateModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		serve   ServeMode
		auth    AuthMode
		authz   AuthZMode
		wantErr string
	}{
		{"stdio+none+apitoken", ServeModeStdio, AuthModeNone, AuthZModeAPIToken, ""},
		// M15: stdio + authz=oauth は OAuth の per-request principal binding が不可のため fail-fast。
		{"stdio+none+oauth (rejected)", ServeModeStdio, AuthModeNone, AuthZModeOAuth, "HTTP transport"},
		{"stdio+oidc rejected", ServeModeStdio, AuthModeOIDC, AuthZModeAPIToken, "stdio"},
		{"stdio+oidc+oauth rejected", ServeModeStdio, AuthModeOIDC, AuthZModeOAuth, "stdio"},
		{"http+none+apitoken", ServeModeHTTP, AuthModeNone, AuthZModeAPIToken, ""},
		// issue #7: HTTP + auth=none + authz=oauth はデッドな設定なので fail-fast。
		// auth=none では Principal が注入されないため OAuth フローが動作しない。
		{"http+none+oauth (rejected)", ServeModeHTTP, AuthModeNone, AuthZModeOAuth, "oidc"},
		{"http+oidc+oauth", ServeModeHTTP, AuthModeOIDC, AuthZModeOAuth, ""},
		{"http+oidc+apitoken (allowed)", ServeModeHTTP, AuthModeOIDC, AuthZModeAPIToken, ""},
		{"invalid authz", ServeModeHTTP, AuthModeNone, "wat", "AuthZMode"},
		{"invalid auth", ServeModeHTTP, "wat", AuthZModeAPIToken, "AuthMode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateModes(tt.serve, tt.auth, tt.authz)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
