package cli_test

import (
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/cli"
)

// E-1: cobra の unknown command エラーが USAGE にマップされる
func TestMapToOutputError_UnknownCommand(t *testing.T) {
	err := errors.New(`unknown command "foo" for "kintone"`)
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
	err := errors.New("boom")
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
