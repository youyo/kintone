package resolver

import (
	"errors"
	"strings"
	"testing"
)

func TestNotFoundError_UnwrapAndError(t *testing.T) {
	t.Run("app", func(t *testing.T) {
		err := &NotFoundError{Kind: "app", Ref: "営業"}
		if !errors.Is(err, ErrAppNotFound) {
			t.Fatalf("errors.Is should match ErrAppNotFound")
		}
		if errors.Is(err, ErrFieldNotFound) {
			t.Fatalf("should not match ErrFieldNotFound")
		}
		if !strings.Contains(err.Error(), "app") || !strings.Contains(err.Error(), "営業") {
			t.Fatalf("Error() should contain kind and ref, got %q", err.Error())
		}
	})
	t.Run("field", func(t *testing.T) {
		err := &NotFoundError{Kind: "field", Ref: "顧客名"}
		if !errors.Is(err, ErrFieldNotFound) {
			t.Fatalf("errors.Is should match ErrFieldNotFound")
		}
		if errors.Is(err, ErrAppNotFound) {
			t.Fatalf("should not match ErrAppNotFound")
		}
	})
}

func TestAmbiguousError_UnwrapAndError(t *testing.T) {
	t.Run("app", func(t *testing.T) {
		err := &AmbiguousError{
			Kind: "app",
			Ref:  "営業",
			Candidates: []Candidate{
				{ID: "42", Code: "sales-case", Name: "営業案件"},
				{ID: "55", Code: "sales-2024", Name: "営業 2024"},
			},
		}
		if !errors.Is(err, ErrAppAmbiguous) {
			t.Fatalf("errors.Is should match ErrAppAmbiguous")
		}
		msg := err.Error()
		if !strings.Contains(msg, "営業案件(id=42)") || !strings.Contains(msg, "営業 2024(id=55)") {
			t.Fatalf("Error() should list candidates, got %q", msg)
		}
		if !strings.Contains(msg, "2 candidates") {
			t.Fatalf("Error() should report count, got %q", msg)
		}
	})
	t.Run("field", func(t *testing.T) {
		err := &AmbiguousError{
			Kind: "field",
			Ref:  "名前",
			Candidates: []Candidate{
				{Code: "name", Label: "名前"},
				{Code: "full_name", Label: "氏名（フル名前）"},
			},
		}
		if !errors.Is(err, ErrFieldAmbiguous) {
			t.Fatalf("errors.Is should match ErrFieldAmbiguous")
		}
		msg := err.Error()
		if !strings.Contains(msg, "名前(code=name)") {
			t.Fatalf("Error() should list field candidates, got %q", msg)
		}
	})
}
