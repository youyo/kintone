package config

import "testing"

func mockGetenv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestLoadEnv_AllSet(t *testing.T) {
	t.Parallel()
	getenv := mockGetenv(map[string]string{
		"KINTONE_PROFILE":     "dev",
		"KINTONE_CONFIG_PATH": "/tmp/conf.toml",
		"KINTONE_CACHE_PATH":  "/tmp/cache.db",
		"KINTONE_DOMAIN":      "example.cybozu.com",
		"KINTONE_AUTH":        "api-token",
		"KINTONE_API_TOKEN":   "secret",
	})
	got := LoadEnv(getenv)
	want := EnvConfig{
		Profile:    "dev",
		ConfigPath: "/tmp/conf.toml",
		CachePath:  "/tmp/cache.db",
		Domain:     "example.cybozu.com",
		Auth:       "api-token",
		APIToken:   "secret",
	}
	if got != want {
		t.Errorf("LoadEnv() = %+v, want %+v", got, want)
	}
}

func TestLoadEnv_PartialOnly(t *testing.T) {
	t.Parallel()
	getenv := mockGetenv(map[string]string{
		"KINTONE_PROFILE": "dev",
	})
	got := LoadEnv(getenv)
	if got.Profile != "dev" {
		t.Errorf("Profile = %q, want %q", got.Profile, "dev")
	}
	if got.Domain != "" {
		t.Errorf("Domain = %q, want empty", got.Domain)
	}
	if got.APIToken != "" {
		t.Errorf("APIToken = %q, want empty", got.APIToken)
	}
}

func TestLoadEnv_AllEmpty(t *testing.T) {
	t.Parallel()
	getenv := mockGetenv(map[string]string{})
	got := LoadEnv(getenv)
	want := EnvConfig{}
	if got != want {
		t.Errorf("LoadEnv() = %+v, want zero value", got)
	}
}

func TestLoadEnv_APITokenSet(t *testing.T) {
	t.Parallel()
	getenv := mockGetenv(map[string]string{
		"KINTONE_API_TOKEN": "abc",
	})
	got := LoadEnv(getenv)
	if got.APIToken != "abc" {
		t.Errorf("APIToken = %q, want %q", got.APIToken, "abc")
	}
}

// EC-1: KINTONE_OAUTH_CLIENT_ID / KINTONE_OAUTH_CLIENT_SECRET / KINTONE_OAUTH_REDIRECT_URL を読み取ること。
func TestLoadEnv_OAuthFields(t *testing.T) {
	t.Parallel()
	getenv := mockGetenv(map[string]string{
		"KINTONE_OAUTH_CLIENT_ID":     "my-client-id",
		"KINTONE_OAUTH_CLIENT_SECRET": "my-client-secret",
		"KINTONE_OAUTH_REDIRECT_URL":  "http://127.0.0.1:8080/callback",
		"KINTONE_OAUTH_SCOPES":        "k:app_record:read k:app_record:write",
	})
	got := LoadEnv(getenv)
	if got.OAuthClientID != "my-client-id" {
		t.Errorf("OAuthClientID = %q, want %q", got.OAuthClientID, "my-client-id")
	}
	if got.OAuthClientSecret != "my-client-secret" {
		t.Errorf("OAuthClientSecret = %q, want %q", got.OAuthClientSecret, "my-client-secret")
	}
	if got.OAuthRedirectURL != "http://127.0.0.1:8080/callback" {
		t.Errorf("OAuthRedirectURL = %q", got.OAuthRedirectURL)
	}
	if got.OAuthScopes != "k:app_record:read k:app_record:write" {
		t.Errorf("OAuthScopes = %q", got.OAuthScopes)
	}
}

func TestLoadEnv_AuthInvalidValuePassedThrough(t *testing.T) {
	t.Parallel()
	// バリデーションは Resolve で行うため、env レイヤでは生値をそのまま格納する
	getenv := mockGetenv(map[string]string{
		"KINTONE_AUTH": "xxx",
	})
	got := LoadEnv(getenv)
	if got.Auth != "xxx" {
		t.Errorf("Auth = %q, want %q (no validation in env layer)", got.Auth, "xxx")
	}
}
