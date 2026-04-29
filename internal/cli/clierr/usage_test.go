package clierr_test

import (
	"errors"
	"testing"

	"github.com/youyo/kintone/internal/cli/clierr"
)

// CE-1: NewUsageError が正しい型と message を生成
func TestNewUsageError(t *testing.T) {
	t.Parallel()
	err := clierr.NewUsageError("invalid: %s=%d", "x", 42)
	if err == nil {
		t.Fatal("nil")
	}
	if err.Error() != "invalid: x=42" {
		t.Errorf("msg=%q", err.Error())
	}
}

// CE-2: errors.As で *UsageError として検出可能
func TestUsageError_As(t *testing.T) {
	t.Parallel()
	err := clierr.NewUsageError("ops: %s", "broken")
	var ue *clierr.UsageError
	if !errors.As(err, &ue) {
		t.Fatal("errors.As failed")
	}
	if ue.Msg != "ops: broken" {
		t.Errorf("msg=%q", ue.Msg)
	}
}
