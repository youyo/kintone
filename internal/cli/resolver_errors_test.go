// Package cli_test の M08 追加分: resolver / operations Err* のマッピングを検証する。
package cli_test

import (
	"testing"

	"github.com/youyo/kintone/internal/cli"
	"github.com/youyo/kintone/internal/resolver"
	"github.com/youyo/kintone/internal/service/operations"
)

func TestMapToOutputError_ResolverNotFound_App(t *testing.T) {
	err := &resolver.NotFoundError{Kind: "app", Ref: "x"}
	oe := cli.MapToOutputError(err)
	if oe == nil {
		t.Fatal("nil oe")
	}
	if oe.Code != "RESOLVER_APP_NOT_FOUND" {
		t.Errorf("Code=%q want RESOLVER_APP_NOT_FOUND", oe.Code)
	}
}

func TestMapToOutputError_ResolverNotFound_Field(t *testing.T) {
	err := &resolver.NotFoundError{Kind: "field", Ref: "x"}
	oe := cli.MapToOutputError(err)
	if oe.Code != "RESOLVER_FIELD_NOT_FOUND" {
		t.Errorf("Code=%q want RESOLVER_FIELD_NOT_FOUND", oe.Code)
	}
}

func TestMapToOutputError_ResolverAmbiguous_App(t *testing.T) {
	err := &resolver.AmbiguousError{
		Kind: "app", Ref: "営業",
		Candidates: []resolver.Candidate{
			{ID: "42", Code: "sales", Name: "営業 A"},
			{ID: "55", Code: "sales2", Name: "営業 B"},
		},
	}
	oe := cli.MapToOutputError(err)
	if oe.Code != "RESOLVER_APP_AMBIGUOUS" {
		t.Errorf("Code=%q want RESOLVER_APP_AMBIGUOUS", oe.Code)
	}
	candidates, ok := oe.Details["candidates"].([]map[string]any)
	if !ok {
		t.Fatalf("expected candidates as []map[string]any, got %T (%v)", oe.Details["candidates"], oe.Details["candidates"])
	}
	if len(candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(candidates))
	}
}

func TestMapToOutputError_ResolverAmbiguous_Field(t *testing.T) {
	err := &resolver.AmbiguousError{Kind: "field", Ref: "名前", Candidates: []resolver.Candidate{{Code: "name", Label: "名前"}}}
	oe := cli.MapToOutputError(err)
	if oe.Code != "RESOLVER_FIELD_AMBIGUOUS" {
		t.Errorf("Code=%q want RESOLVER_FIELD_AMBIGUOUS", oe.Code)
	}
}

func TestMapToOutputError_ResolverAppListTooLarge(t *testing.T) {
	oe := cli.MapToOutputError(resolver.ErrAppListTooLarge)
	if oe.Code != "RESOLVER_APP_LIST_TOO_LARGE" {
		t.Errorf("Code=%q want RESOLVER_APP_LIST_TOO_LARGE", oe.Code)
	}
}

func TestMapToOutputError_OperationsErrInvalidApp(t *testing.T) {
	oe := cli.MapToOutputError(operations.ErrInvalidApp)
	if oe.Code != "USAGE" {
		t.Errorf("Code=%q want USAGE", oe.Code)
	}
}

func TestMapToOutputError_OperationsErrConflictingAppRef(t *testing.T) {
	oe := cli.MapToOutputError(operations.ErrConflictingAppRef)
	if oe.Code != "USAGE" {
		t.Errorf("Code=%q want USAGE", oe.Code)
	}
}

func TestMapToOutputError_OperationsErrConflictingUpdateKeyFieldRef(t *testing.T) {
	oe := cli.MapToOutputError(operations.ErrConflictingUpdateKeyFieldRef)
	if oe.Code != "USAGE" {
		t.Errorf("Code=%q want USAGE", oe.Code)
	}
}

func TestMapToOutputError_OperationsErrResolverUnavailable(t *testing.T) {
	oe := cli.MapToOutputError(operations.ErrResolverUnavailable)
	if oe.Code != "INTERNAL" {
		t.Errorf("Code=%q want INTERNAL", oe.Code)
	}
}

func TestMapToOutputError_ResolverErrEmptyRef(t *testing.T) {
	oe := cli.MapToOutputError(resolver.ErrEmptyRef)
	if oe.Code != "USAGE" {
		t.Errorf("Code=%q want USAGE", oe.Code)
	}
}
