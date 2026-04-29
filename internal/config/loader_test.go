package config

import (
	"errors"
	"io/fs"
	"testing"
)

func mockReadFile(content []byte, err error) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		return content, err
	}
}

func TestLoadFile_ValidTOML(t *testing.T) {
	t.Parallel()
	content := []byte(`
[default_profile]
name = "default"

[profiles.default]
domain = "example.cybozu.com"
auth   = "api-token"
`)
	got, err := LoadFile("/tmp/c.toml", mockReadFile(content, nil))
	if err != nil {
		t.Fatalf("LoadFile error: %v", err)
	}
	if got.DefaultProfile.Name != "default" {
		t.Errorf("DefaultProfile.Name = %q, want %q", got.DefaultProfile.Name, "default")
	}
	dp, ok := got.Profiles["default"]
	if !ok {
		t.Fatalf("Profiles[default] missing")
	}
	if dp.Domain != "example.cybozu.com" {
		t.Errorf("Domain = %q, want %q", dp.Domain, "example.cybozu.com")
	}
	if dp.Auth != "api-token" {
		t.Errorf("Auth = %q, want %q", dp.Auth, "api-token")
	}
}

func TestLoadFile_NotExist(t *testing.T) {
	t.Parallel()
	// ファイル不在は (zero, nil) を返す（明示 --config 指定の有無は呼び出し側責務）
	got, err := LoadFile("/tmp/nonexistent.toml", mockReadFile(nil, fs.ErrNotExist))
	if err != nil {
		t.Fatalf("LoadFile should not error on ErrNotExist, got: %v", err)
	}
	if got.DefaultProfile.Name != "" || len(got.Profiles) != 0 {
		t.Errorf("expected zero FileConfig, got %+v", got)
	}
}

func TestLoadFile_EmptyContent(t *testing.T) {
	t.Parallel()
	got, err := LoadFile("/tmp/empty.toml", mockReadFile([]byte{}, nil))
	if err != nil {
		t.Fatalf("LoadFile error: %v", err)
	}
	if got.DefaultProfile.Name != "" || len(got.Profiles) != 0 {
		t.Errorf("expected zero FileConfig for empty content, got %+v", got)
	}
}

func TestLoadFile_ParseError(t *testing.T) {
	t.Parallel()
	broken := []byte(`broken =`)
	_, err := LoadFile("/tmp/broken.toml", mockReadFile(broken, nil))
	if err == nil {
		t.Fatalf("expected ParseError, got nil")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	if pe.Path != "/tmp/broken.toml" {
		t.Errorf("ParseError.Path = %q, want %q", pe.Path, "/tmp/broken.toml")
	}
	if pe.Err == nil {
		t.Errorf("ParseError.Err should not be nil")
	}
}

func TestLoadFile_MultipleProfiles(t *testing.T) {
	t.Parallel()
	content := []byte(`
[profiles.default]
domain = "a.cybozu.com"
auth   = "api-token"

[profiles.dev]
domain = "b.cybozu.com"
auth   = "oauth"
`)
	got, err := LoadFile("/tmp/c.toml", mockReadFile(content, nil))
	if err != nil {
		t.Fatalf("LoadFile error: %v", err)
	}
	if len(got.Profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(got.Profiles))
	}
	if got.Profiles["dev"].Auth != "oauth" {
		t.Errorf("dev.Auth = %q, want %q", got.Profiles["dev"].Auth, "oauth")
	}
}

func TestLoadFile_UnknownKeyIgnored(t *testing.T) {
	t.Parallel()
	// 前方互換性のため、未知のキーはエラーにせず無視する
	content := []byte(`
[profiles.default]
domain = "a.cybozu.com"
auth   = "api-token"
foo    = "bar"
`)
	got, err := LoadFile("/tmp/c.toml", mockReadFile(content, nil))
	if err != nil {
		t.Fatalf("LoadFile should ignore unknown keys, got: %v", err)
	}
	if got.Profiles["default"].Domain != "a.cybozu.com" {
		t.Errorf("Domain = %q, want %q", got.Profiles["default"].Domain, "a.cybozu.com")
	}
}

func TestLoadFile_ParseErrorUnwrap(t *testing.T) {
	t.Parallel()
	broken := []byte(`= invalid`)
	_, err := LoadFile("/tmp/b.toml", mockReadFile(broken, nil))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseError")
	}
	// Unwrap で原因 error を取れること
	if errors.Unwrap(err) == nil {
		t.Errorf("Unwrap should return cause error")
	}
}

func TestParseError_Error(t *testing.T) {
	t.Parallel()
	pe := &ParseError{Path: "/tmp/x.toml", Err: errors.New("bad token")}
	got := pe.Error()
	if got == "" {
		t.Errorf("Error() should not be empty")
	}
	if !contains(got, "/tmp/x.toml") || !contains(got, "bad token") {
		t.Errorf("Error() = %q, missing path or cause", got)
	}
}

func TestNotFoundError_Error(t *testing.T) {
	t.Parallel()
	nfe := &NotFoundError{Path: "/no/file"}
	got := nfe.Error()
	if !contains(got, "/no/file") {
		t.Errorf("Error() = %q, should contain path", got)
	}
}

func TestAlreadyExistsError_Error(t *testing.T) {
	t.Parallel()
	ae := &AlreadyExistsError{Path: "/exists"}
	got := ae.Error()
	if !contains(got, "/exists") {
		t.Errorf("Error() = %q, should contain path", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestLoadFile_ReadError(t *testing.T) {
	t.Parallel()
	// ErrNotExist 以外の IO エラーはそのまま伝播
	want := errors.New("permission denied")
	_, err := LoadFile("/tmp/c.toml", mockReadFile(nil, want))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, want) {
		t.Errorf("expected error chain to contain %v, got %v", want, err)
	}
}
