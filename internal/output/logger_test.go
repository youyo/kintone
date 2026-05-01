package output

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewLoggerLevels は newLogger のレベル境界を検証する。
func TestNewLoggerLevels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		levelEnv string
		debugOK  bool
		infoOK   bool
		warnOK   bool
		errorOK  bool
	}{
		{"debug", "debug", true, true, true, true},
		{"info", "info", false, true, true, true},
		{"warn-default", "", false, false, true, true},
		{"warn-explicit-uppercase", "WARN", false, false, true, true},
		{"error", "error", false, false, false, true},
		{"unknown-falls-back-to-warn", "trace", false, false, true, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			lg := newLogger(&buf, tc.levelEnv)

			lg.Debug("dbg-marker")
			lg.Info("info-marker")
			lg.Warn("warn-marker")
			lg.Error("error-marker")

			out := buf.String()
			assertContains(t, out, "dbg-marker", tc.debugOK)
			assertContains(t, out, "info-marker", tc.infoOK)
			assertContains(t, out, "warn-marker", tc.warnOK)
			assertContains(t, out, "error-marker", tc.errorOK)
		})
	}
}

func assertContains(t *testing.T, haystack, needle string, want bool) {
	t.Helper()
	got := strings.Contains(haystack, needle)
	if got != want {
		t.Fatalf("contains(%q, %q) = %v, want %v\nfull output: %s", haystack, needle, got, want, haystack)
	}
}

// TestLoggerSingleton は Logger() が同じインスタンスを返すことを確認する。
func TestLoggerSingleton(t *testing.T) {
	a := Logger()
	b := Logger()
	if a != b {
		t.Fatalf("Logger() returned different instances: %p vs %p", a, b)
	}
}
