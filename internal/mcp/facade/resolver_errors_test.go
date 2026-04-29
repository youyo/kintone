// Package facade_test の M08 追加分: resolver / operations Err* 系のマッピング検証。
package facade_test

import (
	"testing"

	"github.com/youyo/kintone/internal/mcp/facade"
	"github.com/youyo/kintone/internal/resolver"
	"github.com/youyo/kintone/internal/service/operations"
)

func TestMapError_ResolverNotFound_App(t *testing.T) {
	oe := facade.MapError(&resolver.NotFoundError{Kind: "app", Ref: "x"})
	if oe.Code != "RESOLVER_APP_NOT_FOUND" {
		t.Errorf("Code=%q", oe.Code)
	}
}

func TestMapError_ResolverNotFound_Field(t *testing.T) {
	oe := facade.MapError(&resolver.NotFoundError{Kind: "field", Ref: "x"})
	if oe.Code != "RESOLVER_FIELD_NOT_FOUND" {
		t.Errorf("Code=%q", oe.Code)
	}
}

func TestMapError_ResolverAmbiguous_App(t *testing.T) {
	oe := facade.MapError(&resolver.AmbiguousError{
		Kind: "app", Ref: "営業",
		Candidates: []resolver.Candidate{{ID: "42", Code: "sales", Name: "営業 A"}},
	})
	if oe.Code != "RESOLVER_APP_AMBIGUOUS" {
		t.Errorf("Code=%q", oe.Code)
	}
	candidates, ok := oe.Details["candidates"].([]map[string]any)
	if !ok || len(candidates) != 1 {
		t.Errorf("expected 1 candidate, got %v", oe.Details["candidates"])
	}
}

func TestMapError_ResolverAmbiguous_Field(t *testing.T) {
	oe := facade.MapError(&resolver.AmbiguousError{
		Kind: "field", Ref: "名前",
		Candidates: []resolver.Candidate{{Code: "name", Label: "名前"}},
	})
	if oe.Code != "RESOLVER_FIELD_AMBIGUOUS" {
		t.Errorf("Code=%q", oe.Code)
	}
}

func TestMapError_ResolverAppListTooLarge(t *testing.T) {
	oe := facade.MapError(resolver.ErrAppListTooLarge)
	if oe.Code != "RESOLVER_APP_LIST_TOO_LARGE" {
		t.Errorf("Code=%q", oe.Code)
	}
}

func TestMapError_ConflictingAppRef(t *testing.T) {
	oe := facade.MapError(operations.ErrConflictingAppRef)
	if oe.Code != "INVALID_PARAMS" {
		t.Errorf("Code=%q", oe.Code)
	}
}

func TestMapError_ConflictingUpdateKeyFieldRef(t *testing.T) {
	oe := facade.MapError(operations.ErrConflictingUpdateKeyFieldRef)
	if oe.Code != "INVALID_PARAMS" {
		t.Errorf("Code=%q", oe.Code)
	}
}

func TestMapError_ResolverUnavailable(t *testing.T) {
	oe := facade.MapError(operations.ErrResolverUnavailable)
	if oe.Code != "INTERNAL" {
		t.Errorf("Code=%q", oe.Code)
	}
}

func TestMapError_ResolverErrEmptyRef(t *testing.T) {
	oe := facade.MapError(resolver.ErrEmptyRef)
	if oe.Code != "INVALID_PARAMS" {
		t.Errorf("Code=%q", oe.Code)
	}
}
