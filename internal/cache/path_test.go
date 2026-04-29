package cache

import (
	"errors"
	"path/filepath"
	"testing"
)

// C-1: env override.
func TestDefaultCachePath_EnvOverride(t *testing.T) {
	getenv := func(k string) string {
		if k == "KINTONE_CACHE_PATH" {
			return "/foo/bar.db"
		}
		return ""
	}
	got, err := DefaultCachePath(getenv, func() (string, error) { return "/h", nil })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "/foo/bar.db" {
		t.Errorf("got %q, want %q", got, "/foo/bar.db")
	}
}

// C-2: HOME=/h でのデフォルト（自動検出ヒューリスティックなし）.
func TestDefaultCachePath_HostDefault(t *testing.T) {
	getenv := func(k string) string { return "" }
	got, err := DefaultCachePath(getenv, func() (string, error) { return "/h", nil })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := filepath.Join("/h", ".cache", "kintone", "cache.db")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// C-3: HOME 取得失敗.
func TestDefaultCachePath_HomeError(t *testing.T) {
	getenv := func(k string) string { return "" }
	wantErr := errors.New("no home")
	_, err := DefaultCachePath(getenv, func() (string, error) { return "", wantErr })
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wrap of %v", err, wantErr)
	}
}

// Tokens 用 path.
func TestDefaultTokensPath_EnvOverride(t *testing.T) {
	getenv := func(k string) string {
		if k == "KINTONE_TOKENS_PATH" {
			return "/secret/tok.db"
		}
		return ""
	}
	got, err := DefaultTokensPath(getenv, func() (string, error) { return "/h", nil })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "/secret/tok.db" {
		t.Errorf("got %q, want %q", got, "/secret/tok.db")
	}
}

func TestDefaultTokensPath_HostDefault(t *testing.T) {
	getenv := func(k string) string { return "" }
	got, err := DefaultTokensPath(getenv, func() (string, error) { return "/h", nil })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := filepath.Join("/h", ".cache", "kintone", "tokens.db")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultTokensPath_HomeError(t *testing.T) {
	getenv := func(k string) string { return "" }
	wantErr := errors.New("no home")
	_, err := DefaultTokensPath(getenv, func() (string, error) { return "", wantErr })
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wrap of %v", err, wantErr)
	}
}
