package output

import "testing"

func TestSanitizeURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "no_userinfo",
			in:   "redis://localhost:6379/0",
			want: "redis://localhost:6379/0",
		},
		{
			name: "userinfo_password_masked",
			in:   "redis://user:secret@localhost:6379/0",
			want: "redis://user:***@localhost:6379/0",
		},
		{
			name: "userinfo_username_only",
			in:   "redis://user@localhost:6379/0",
			want: "redis://user@localhost:6379/0",
		},
		{
			name: "rediss_userinfo_password",
			in:   "rediss://admin:topsecret@host.example.com:6380/1",
			want: "rediss://admin:***@host.example.com:6380/1",
		},
		{
			name: "query_password_masked",
			in:   "https://host/path?password=topsecret&db=1",
			want: "https://host/path?db=1&password=***",
		},
		{
			name: "userinfo_and_query",
			in:   "redis://u:p@host:6379/0?password=q",
			want: "redis://u:***@host:6379/0?password=***",
		},
		{
			name: "invalid_url_passthrough",
			in:   "::not a url::",
			want: "::not a url::",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeURL(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizeURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
