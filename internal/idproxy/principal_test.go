package idproxy

import (
	"context"
	"testing"

	upstream "github.com/youyo/idproxy"
)

func TestWithAndFromContext_RoundTrip(t *testing.T) {
	t.Parallel()
	p := &Principal{ID: "https://issuer/:sub-1", Issuer: "https://issuer/", Subject: "sub-1", Email: "u@example.com"}
	ctx := WithPrincipal(context.Background(), p)
	got := FromContext(ctx)
	if got != p {
		t.Fatalf("FromContext: got %v, want %v", got, p)
	}
}

func TestFromContext_NilWhenAbsent(t *testing.T) {
	t.Parallel()
	if got := FromContext(context.Background()); got != nil {
		t.Fatalf("FromContext: got %v, want nil", got)
	}
}

func TestFromContext_NilContext(t *testing.T) {
	t.Parallel()
	if got := FromContext(nil); got != nil { //nolint:staticcheck // intentional nil context test
		t.Fatalf("FromContext(nil): got %v, want nil", got)
	}
}

func TestPrincipalFromUser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   *upstream.User
		want *Principal
	}{
		{"nil", nil, nil},
		{"empty issuer", &upstream.User{Issuer: "", Subject: "s"}, nil},
		{"empty subject", &upstream.User{Issuer: "https://i", Subject: ""}, nil},
		{
			"normal",
			&upstream.User{Issuer: "https://accounts.google.com", Subject: "abc", Email: "u@e", Name: "U"},
			&Principal{ID: "https://accounts.google.com:abc", Issuer: "https://accounts.google.com", Subject: "abc", Email: "u@e", Name: "U"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := principalFromUser(tt.in)
			switch {
			case tt.want == nil && got == nil:
				return
			case tt.want == nil || got == nil:
				t.Fatalf("got %+v, want %+v", got, tt.want)
			case *got != *tt.want:
				t.Fatalf("got %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

func TestMakePrincipalID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		issuer  string
		subject string
		want    string
	}{
		{"normal", "https://accounts.google.com", "abc", "https://accounts.google.com:abc"},
		{"empty issuer", "", "abc", ""},
		{"empty subject", "https://x", "", ""},
		{"both empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := makePrincipalID(tt.issuer, tt.subject); got != tt.want {
				t.Fatalf("makePrincipalID(%q,%q): got %q want %q", tt.issuer, tt.subject, got, tt.want)
			}
		})
	}
}
