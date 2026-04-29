package cli_test

import (
	stderrors "errors"
	"fmt"
	"testing"

	"github.com/youyo/kintone/internal/cli"
	"github.com/youyo/kintone/internal/config"
)

// E-1: cobra の unknown command エラーが USAGE にマップされる
func TestMapToOutputError_UnknownCommand(t *testing.T) {
	err := stderrors.New(`unknown command "foo" for "kintone"`)
	oe := cli.MapToOutputError(err)
	if oe == nil {
		t.Fatal("expected non-nil *output.Error")
	}
	if oe.Code != "USAGE" {
		t.Errorf("expected code=USAGE, got %q", oe.Code)
	}
}

// E-2: 不明エラーが INTERNAL にマップされる
func TestMapToOutputError_Unknown(t *testing.T) {
	err := stderrors.New("boom")
	oe := cli.MapToOutputError(err)
	if oe == nil {
		t.Fatal("expected non-nil *output.Error")
	}
	if oe.Code != "INTERNAL" {
		t.Errorf("expected code=INTERNAL, got %q", oe.Code)
	}
	if oe.Message != "boom" {
		t.Errorf("expected message=boom, got %q", oe.Message)
	}
}

// E-3: nil 入力は nil を返す
func TestMapToOutputError_Nil(t *testing.T) {
	oe := cli.MapToOutputError(nil)
	if oe != nil {
		t.Errorf("expected nil for nil input, got %v", oe)
	}
}

// E-4: ProfileNotFoundError → CONFIG_PROFILE_NOT_FOUND にマップ
func TestMapToOutputError_ProfileNotFound(t *testing.T) {
	err := &config.ProfileNotFoundError{Name: "prod", Path: "/etc/x.toml"}
	oe := cli.MapToOutputError(err)
	if oe == nil {
		t.Fatal("expected non-nil *output.Error")
	}
	if oe.Code != "CONFIG_PROFILE_NOT_FOUND" {
		t.Errorf("Code = %q, want CONFIG_PROFILE_NOT_FOUND", oe.Code)
	}
	if got, _ := oe.Details["name"].(string); got != "prod" {
		t.Errorf("Details.name = %v, want prod", oe.Details["name"])
	}
	if got, _ := oe.Details["path"].(string); got != "/etc/x.toml" {
		t.Errorf("Details.path = %v, want /etc/x.toml", oe.Details["path"])
	}
}

// E-5: ProfileNotFoundError でも Path 空のケース
func TestMapToOutputError_ProfileNotFoundNoPath(t *testing.T) {
	err := &config.ProfileNotFoundError{Name: "x"}
	oe := cli.MapToOutputError(err)
	if oe.Code != "CONFIG_PROFILE_NOT_FOUND" {
		t.Errorf("Code = %q", oe.Code)
	}
	// Path 空時は details に含まれない
	if _, exists := oe.Details["path"]; exists {
		t.Errorf("expected no path key when empty, got %v", oe.Details)
	}
}

// E-6: ParseError → CONFIG_PARSE_ERROR
func TestMapToOutputError_ParseError(t *testing.T) {
	cause := stderrors.New("syntax")
	err := &config.ParseError{Path: "/tmp/x.toml", Err: cause}
	oe := cli.MapToOutputError(err)
	if oe.Code != "CONFIG_PARSE_ERROR" {
		t.Errorf("Code = %q, want CONFIG_PARSE_ERROR", oe.Code)
	}
	if got, _ := oe.Details["path"].(string); got != "/tmp/x.toml" {
		t.Errorf("Details.path = %v", oe.Details["path"])
	}
	if got, _ := oe.Details["cause"].(string); got != "syntax" {
		t.Errorf("Details.cause = %v", oe.Details["cause"])
	}
}

// E-7: AlreadyExistsError → CONFIG_ALREADY_EXISTS
func TestMapToOutputError_AlreadyExists(t *testing.T) {
	err := &config.AlreadyExistsError{Path: "/tmp/x.toml"}
	oe := cli.MapToOutputError(err)
	if oe.Code != "CONFIG_ALREADY_EXISTS" {
		t.Errorf("Code = %q, want CONFIG_ALREADY_EXISTS", oe.Code)
	}
	if got, _ := oe.Details["path"].(string); got != "/tmp/x.toml" {
		t.Errorf("Details.path = %v", oe.Details["path"])
	}
}

// E-8: NotFoundError → CONFIG_NOT_FOUND
func TestMapToOutputError_NotFound(t *testing.T) {
	err := &config.NotFoundError{Path: "/tmp/x.toml"}
	oe := cli.MapToOutputError(err)
	if oe.Code != "CONFIG_NOT_FOUND" {
		t.Errorf("Code = %q, want CONFIG_NOT_FOUND", oe.Code)
	}
	if got, _ := oe.Details["path"].(string); got != "/tmp/x.toml" {
		t.Errorf("Details.path = %v", oe.Details["path"])
	}
}

// E-9: ラップされた config エラーも errors.As で検出される
func TestMapToOutputError_WrappedConfigError(t *testing.T) {
	inner := &config.ParseError{Path: "/x.toml", Err: stderrors.New("e")}
	wrapped := fmt.Errorf("config: load: %w", inner)
	oe := cli.MapToOutputError(wrapped)
	if oe.Code != "CONFIG_PARSE_ERROR" {
		t.Errorf("expected CONFIG_PARSE_ERROR for wrapped error, got %q", oe.Code)
	}
}
