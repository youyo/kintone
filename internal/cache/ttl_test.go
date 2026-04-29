package cache

import (
	"testing"
	"time"
)

// C-14: TTL 定数。
func TestTTLConstants(t *testing.T) {
	want := 365 * 24 * time.Hour
	if TTLApps != want {
		t.Errorf("TTLApps = %v, want %v", TTLApps, want)
	}
	if TTLFields != want {
		t.Errorf("TTLFields = %v, want %v", TTLFields, want)
	}
	if TTLListApps != want {
		t.Errorf("TTLListApps = %v, want %v", TTLListApps, want)
	}
}
