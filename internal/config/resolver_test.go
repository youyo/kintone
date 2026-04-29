package config

import (
	"errors"
	"testing"
)

func TestResolve_ENVOverFile_Domain(t *testing.T) {
	t.Parallel()
	cli := CLIConfig{}
	env := EnvConfig{Domain: "env.com"}
	file := FileConfig{
		Profiles: map[string]ProfileBlock{
			"default": {Domain: "file.com", Auth: "api-token"},
		},
	}
	r, err := Resolve("default", cli, env, file)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if r.Domain != "env.com" {
		t.Errorf("Domain = %q, want %q", r.Domain, "env.com")
	}
	if r.Source.Domain != "env" {
		t.Errorf("Source.Domain = %q, want %q", r.Source.Domain, "env")
	}
}

func TestResolve_ENVOverFile_Auth(t *testing.T) {
	t.Parallel()
	cli := CLIConfig{}
	env := EnvConfig{Auth: "oauth"}
	file := FileConfig{
		Profiles: map[string]ProfileBlock{
			"default": {Domain: "file.com", Auth: "api-token"},
		},
	}
	r, err := Resolve("default", cli, env, file)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if r.Auth != AuthModeOAuth {
		t.Errorf("Auth = %q, want %q", r.Auth, AuthModeOAuth)
	}
	if r.Source.Auth != "env" {
		t.Errorf("Source.Auth = %q, want %q", r.Source.Auth, "env")
	}
}

func TestResolve_FileOnly(t *testing.T) {
	t.Parallel()
	r, err := Resolve("default", CLIConfig{}, EnvConfig{}, FileConfig{
		Profiles: map[string]ProfileBlock{
			"default": {Domain: "file.com", Auth: "api-token"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if r.Domain != "file.com" || r.Source.Domain != "file" {
		t.Errorf("Domain/Source.Domain = %q/%q, want %q/%q", r.Domain, r.Source.Domain, "file.com", "file")
	}
	if r.Auth != AuthModeAPIToken || r.Source.Auth != "file" {
		t.Errorf("Auth/Source.Auth = %q/%q, want %q/%q", r.Auth, r.Source.Auth, "api-token", "file")
	}
}

func TestResolve_AllEmptyDefaultsBlank(t *testing.T) {
	t.Parallel()
	r, err := Resolve("default", CLIConfig{}, EnvConfig{}, FileConfig{})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if r.Domain != "" || r.Source.Domain != "default" {
		t.Errorf("Domain/Source.Domain = %q/%q, want empty/default", r.Domain, r.Source.Domain)
	}
	if r.Auth != "" || r.Source.Auth != "default" {
		t.Errorf("Auth/Source.Auth = %q/%q, want empty/default", r.Auth, r.Source.Auth)
	}
	if r.ProfileName != "default" {
		t.Errorf("ProfileName = %q, want %q", r.ProfileName, "default")
	}
}

func TestResolve_ProfileNotFoundError(t *testing.T) {
	t.Parallel()
	file := FileConfig{
		Profiles: map[string]ProfileBlock{
			"default": {Domain: "x", Auth: "api-token"},
		},
	}
	_, err := Resolve("prod", CLIConfig{}, EnvConfig{}, file)
	if err == nil {
		t.Fatalf("expected ProfileNotFoundError, got nil")
	}
	var pne *ProfileNotFoundError
	if !errors.As(err, &pne) {
		t.Fatalf("expected *ProfileNotFoundError, got %T: %v", err, err)
	}
	if pne.Name != "prod" {
		t.Errorf("ProfileNotFoundError.Name = %q, want %q", pne.Name, "prod")
	}
}

func TestResolve_DefaultProfileWithEmptyFile(t *testing.T) {
	t.Parallel()
	// profile="default" + file 空 → ProfileNotFoundError ではなく空 Resolved
	r, err := Resolve("default", CLIConfig{}, EnvConfig{}, FileConfig{})
	if err != nil {
		t.Fatalf("expected no error for default with empty file, got: %v", err)
	}
	if r.ProfileName != "default" {
		t.Errorf("ProfileName = %q, want %q", r.ProfileName, "default")
	}
}

func TestResolve_APITokenPropagated(t *testing.T) {
	t.Parallel()
	env := EnvConfig{APIToken: "abc"}
	r, err := Resolve("default", CLIConfig{}, env, FileConfig{})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if r.APIToken != "abc" {
		t.Errorf("APIToken = %q, want %q", r.APIToken, "abc")
	}
}

func TestResolve_AuthMode_OAuth(t *testing.T) {
	t.Parallel()
	env := EnvConfig{Auth: "oauth"}
	r, err := Resolve("default", CLIConfig{}, env, FileConfig{})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if r.Auth != AuthModeOAuth {
		t.Errorf("Auth = %q, want AuthModeOAuth", r.Auth)
	}
}

func TestResolve_InvalidAuthValuePassedThrough(t *testing.T) {
	t.Parallel()
	// バリデーションは M3 で。M02 では生値をそのまま格納
	env := EnvConfig{Auth: "xxx"}
	r, err := Resolve("default", CLIConfig{}, env, FileConfig{})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if string(r.Auth) != "xxx" {
		t.Errorf("Auth = %q, want %q", r.Auth, "xxx")
	}
}

func TestProfileNotFoundError_Error(t *testing.T) {
	t.Parallel()
	e1 := &ProfileNotFoundError{Name: "prod"}
	if !errorMessageContains(e1.Error(), "prod") {
		t.Errorf("Error() = %q, should contain name", e1.Error())
	}
	e2 := &ProfileNotFoundError{Name: "prod", Path: "/etc/x.toml"}
	if !errorMessageContains(e2.Error(), "/etc/x.toml") {
		t.Errorf("Error() = %q, should contain path when set", e2.Error())
	}
}

func errorMessageContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestResolve_CachePathPropagated(t *testing.T) {
	t.Parallel()
	env := EnvConfig{CachePath: "/tmp/cache.db"}
	r, err := Resolve("default", CLIConfig{}, env, FileConfig{})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if r.CachePath != "/tmp/cache.db" {
		t.Errorf("CachePath = %q, want %q", r.CachePath, "/tmp/cache.db")
	}
}
