package config

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestLoad_AllDefaults(t *testing.T) {
	t.Parallel()
	opts := LoadOptions{
		Getenv:   mockGetenv(map[string]string{}),
		ReadFile: mockReadFile(nil, fs.ErrNotExist),
		UserHomeDir: func() (string, error) {
			return "/home/test", nil
		},
	}
	r, err := Load(opts)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if r.ProfileName != "default" {
		t.Errorf("ProfileName = %q, want default", r.ProfileName)
	}
	if r.Source.Profile != "default" || r.Source.Domain != "default" {
		t.Errorf("Source = %+v, want all default", r.Source)
	}
	if r.ConfigPath != filepath.Join("/home/test", ".config", "kintone", "config.toml") {
		t.Errorf("ConfigPath = %q, want default-resolved path", r.ConfigPath)
	}
}

func TestLoad_HomeResolutionFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("HOME unset")
	opts := LoadOptions{
		Getenv:      mockGetenv(map[string]string{}),
		ReadFile:    mockReadFile(nil, fs.ErrNotExist),
		UserHomeDir: func() (string, error) { return "", wantErr },
	}
	_, err := Load(opts)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error chain to contain %v, got %v", wantErr, err)
	}
}

func TestLoad_ConfigPathFromENV(t *testing.T) {
	t.Parallel()
	content := []byte(`
[profiles.default]
domain = "from-env-path.cybozu.com"
auth   = "api-token"
`)
	calledPath := ""
	opts := LoadOptions{
		Getenv: mockGetenv(map[string]string{
			"KINTONE_CONFIG_PATH": "/custom/path.toml",
		}),
		ReadFile: func(path string) ([]byte, error) {
			calledPath = path
			return content, nil
		},
		UserHomeDir: func() (string, error) { return "/home/test", nil },
	}
	r, err := Load(opts)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if calledPath != "/custom/path.toml" {
		t.Errorf("ReadFile called with %q, want %q", calledPath, "/custom/path.toml")
	}
	if r.Domain != "from-env-path.cybozu.com" {
		t.Errorf("Domain = %q, want from env-specified file", r.Domain)
	}
}

func TestLoad_CLIOverridesENVConfigPath(t *testing.T) {
	t.Parallel()
	content := []byte(`
[profiles.default]
domain = "from-cli-path.cybozu.com"
auth   = "api-token"
`)
	calledPath := ""
	opts := LoadOptions{
		CLI: CLIConfig{ConfigPath: "/cli/path.toml"},
		Getenv: mockGetenv(map[string]string{
			"KINTONE_CONFIG_PATH": "/env/path.toml",
		}),
		ReadFile: func(path string) ([]byte, error) {
			calledPath = path
			return content, nil
		},
		UserHomeDir: func() (string, error) { return "/home/test", nil },
	}
	r, err := Load(opts)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if calledPath != "/cli/path.toml" {
		t.Errorf("ReadFile called with %q, want %q", calledPath, "/cli/path.toml")
	}
	if r.Domain != "from-cli-path.cybozu.com" {
		t.Errorf("Domain = %q, want from cli-specified file", r.Domain)
	}
}

func TestLoad_ProfileFromEnv(t *testing.T) {
	t.Parallel()
	content := []byte(`
[default_profile]
name = "default"

[profiles.default]
domain = "default.cybozu.com"
auth   = "api-token"

[profiles.dev]
domain = "dev.cybozu.com"
auth   = "oauth"
`)
	opts := LoadOptions{
		Getenv: mockGetenv(map[string]string{
			"KINTONE_PROFILE": "dev",
		}),
		ReadFile:    mockReadFile(content, nil),
		UserHomeDir: func() (string, error) { return "/home/test", nil },
	}
	r, err := Load(opts)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if r.ProfileName != "dev" {
		t.Errorf("ProfileName = %q, want %q", r.ProfileName, "dev")
	}
	if r.Domain != "dev.cybozu.com" {
		t.Errorf("Domain = %q, want %q", r.Domain, "dev.cybozu.com")
	}
	if r.Auth != AuthModeOAuth {
		t.Errorf("Auth = %q, want oauth", r.Auth)
	}
}

func TestLoad_ParseErrorPropagated(t *testing.T) {
	t.Parallel()
	broken := []byte(`broken =`)
	opts := LoadOptions{
		Getenv:      mockGetenv(map[string]string{}),
		ReadFile:    mockReadFile(broken, nil),
		UserHomeDir: func() (string, error) { return "/home/test", nil },
	}
	_, err := Load(opts)
	if err == nil {
		t.Fatalf("expected ParseError, got nil")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
}

func TestLoad_ExplicitConfigPathNotFound(t *testing.T) {
	t.Parallel()
	// 明示的に --config 指定して、かつそのファイルが存在しない場合は NotFoundError
	opts := LoadOptions{
		CLI:    CLIConfig{ConfigPath: "/no/such/file.toml"},
		Getenv: mockGetenv(map[string]string{}),
		ReadFile: func(path string) ([]byte, error) {
			return nil, fs.ErrNotExist
		},
		UserHomeDir: func() (string, error) { return "/home/test", nil },
	}
	_, err := Load(opts)
	if err == nil {
		t.Fatalf("expected NotFoundError, got nil")
	}
	var nfe *NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
	if nfe.Path != "/no/such/file.toml" {
		t.Errorf("NotFoundError.Path = %q, want %q", nfe.Path, "/no/such/file.toml")
	}
}

func TestLoad_DefaultPathNotFoundIsNotError(t *testing.T) {
	t.Parallel()
	// 明示指定なしでデフォルトパスに何もない場合はエラーにしない（spec 完了条件 #2）
	opts := LoadOptions{
		Getenv:      mockGetenv(map[string]string{}),
		ReadFile:    mockReadFile(nil, fs.ErrNotExist),
		UserHomeDir: func() (string, error) { return "/home/test", nil },
	}
	r, err := Load(opts)
	if err != nil {
		t.Fatalf("expected no error for missing default path, got: %v", err)
	}
	if r.ProfileName != "default" {
		t.Errorf("ProfileName = %q, want %q", r.ProfileName, "default")
	}
}
