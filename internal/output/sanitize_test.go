package output

import (
	"context"
	"errors"
	"net"
	"testing"
)

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

// TestClassifyBackendError は ClassifyBackendError が各エラーを正しい cause_class に分類することを確認する。
func TestClassifyBackendError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		// CBE-1: nil → "unknown"
		{
			name: "nil",
			err:  nil,
			want: "unknown",
		},
		// CBE-2: net.OpError → "network"
		{
			name: "net_op_error",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
			want: "network",
		},
		// CBE-3: context.DeadlineExceeded → "timeout"
		{
			name: "deadline_exceeded",
			err:  context.DeadlineExceeded,
			want: "timeout",
		},
		// CBE-4: "NOAUTH" を含むエラー → "auth"
		{
			name: "noauth_message",
			err:  errors.New("NOAUTH Authentication required"),
			want: "auth",
		},
		// CBE-5: "wrongpass" を含むエラー → "auth"
		{
			name: "wrongpass_message",
			err:  errors.New("WRONGPASS invalid username-password pair"),
			want: "auth",
		},
		// CBE-6: "access denied" を含むエラー → "auth"
		{
			name: "access_denied_message",
			err:  errors.New("access denied for user"),
			want: "auth",
		},
		// CBE-7: "unauthorized" を含むエラー → "auth"
		{
			name: "unauthorized_message",
			err:  errors.New("unauthorized"),
			want: "auth",
		},
		// CBE-8: "timeout" を含むエラー → "timeout"
		{
			name: "timeout_message",
			err:  errors.New("i/o timeout"),
			want: "timeout",
		},
		// CBE-9: "connection refused" を含むエラー → "network"
		{
			name: "connection_refused_message",
			err:  errors.New("connection refused"),
			want: "network",
		},
		// CBE-10: "no such host" を含むエラー → "network"
		{
			name: "no_such_host_message",
			err:  errors.New("no such host"),
			want: "network",
		},
		// CBE-11: その他 → "unknown"
		{
			name: "unknown_error",
			err:  errors.New("boom"),
			want: "unknown",
		},
		// CBE-12: wrapped net.OpError → "network"（errors.As でチェーン走査）
		{
			name: "wrapped_net_op_error",
			err:  errors.Join(errors.New("wrap"), &net.OpError{Op: "connect", Net: "tcp", Err: errors.New("refused")}),
			want: "network",
		},
		// CBE-13: wrapped context.DeadlineExceeded → "timeout"（errors.Is でチェーン走査）
		{
			name: "wrapped_deadline_exceeded",
			err:  errors.Join(errors.New("wrap"), context.DeadlineExceeded),
			want: "timeout",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyBackendError(tc.err)
			if got != tc.want {
				t.Errorf("ClassifyBackendError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
