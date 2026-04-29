package kintoneapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"
)

// TestInsertRecords は POST /k/v1/records.json の挙動。
func TestInsertRecords(t *testing.T) {
	t.Parallel()

	t.Run("KW-Insert-1 正常 (複数件)", func(t *testing.T) {
		t.Parallel()
		var gotPath, gotMethod, gotContentType string
		var gotBody map[string]any
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			gotContentType = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			_, _ = io.WriteString(w, `{"ids":["1","2"],"revisions":["1","1"]}`)
		})
		resp, err := fx.client.InsertRecords(context.Background(), InsertRecordsRequest{
			App: 42,
			Records: []map[string]any{
				{"name": map[string]any{"value": "a"}},
				{"name": map[string]any{"value": "b"}},
			},
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if gotPath != "/k/v1/records.json" {
			t.Errorf("path=%q", gotPath)
		}
		if gotMethod != http.MethodPost {
			t.Errorf("method=%q", gotMethod)
		}
		if gotContentType != "application/json" {
			t.Errorf("ct=%q", gotContentType)
		}
		if got, want := gotBody["app"], float64(42); got != want {
			t.Errorf("body app=%v want %v", got, want)
		}
		if recs, ok := gotBody["records"].([]any); !ok || len(recs) != 2 {
			t.Errorf("records=%v", gotBody["records"])
		}
		if !reflect.DeepEqual(resp.IDs, []string{"1", "2"}) {
			t.Errorf("ids=%v", resp.IDs)
		}
		if !reflect.DeepEqual(resp.Revisions, []string{"1", "1"}) {
			t.Errorf("revs=%v", resp.Revisions)
		}
	})

	t.Run("KW-Insert-2 App=0 → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		_, err := fx.client.InsertRecords(context.Background(), InsertRecordsRequest{
			App: 0, Records: []map[string]any{{}},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KW-Insert-3 Records 空 → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		_, err := fx.client.InsertRecords(context.Background(), InsertRecordsRequest{
			App: 1, Records: []map[string]any{},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KW-Insert-4 422 validation 透過", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(422)
			_, _ = io.WriteString(w, `{"code":"CB_VA01","message":"validation"}`)
		}, RetryPolicy{MaxAttempts: 1})
		_, err := fx.client.InsertRecords(context.Background(), InsertRecordsRequest{
			App: 1, Records: []map[string]any{{}},
		})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected APIError, got %v", err)
		}
		if apiErr.Category != CategoryValidation {
			t.Errorf("category=%v", apiErr.Category)
		}
	})
}

// TestDeleteRecords は DELETE /k/v1/records.json の挙動。
func TestDeleteRecords(t *testing.T) {
	t.Parallel()

	t.Run("KW-Delete-1 正常 (revisions あり)", func(t *testing.T) {
		t.Parallel()
		var gotPath, gotMethod string
		var gotBody map[string]any
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			_, _ = io.WriteString(w, `{}`)
		})
		err := fx.client.DeleteRecords(context.Background(), DeleteRecordsRequest{
			App: 42, IDs: []int64{1, 2}, Revisions: []int64{10, 11},
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if gotPath != "/k/v1/records.json" {
			t.Errorf("path=%q", gotPath)
		}
		if gotMethod != http.MethodDelete {
			t.Errorf("method=%q", gotMethod)
		}
		if got, want := gotBody["app"], float64(42); got != want {
			t.Errorf("body app=%v", got)
		}
		ids, _ := gotBody["ids"].([]any)
		if len(ids) != 2 || ids[0] != float64(1) || ids[1] != float64(2) {
			t.Errorf("ids=%v", ids)
		}
		revs, _ := gotBody["revisions"].([]any)
		if len(revs) != 2 || revs[0] != float64(10) || revs[1] != float64(11) {
			t.Errorf("revisions=%v", revs)
		}
	})

	t.Run("KW-Delete-2 正常 (revisions なし)", func(t *testing.T) {
		t.Parallel()
		var gotBody map[string]any
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			_, _ = io.WriteString(w, `{}`)
		})
		err := fx.client.DeleteRecords(context.Background(), DeleteRecordsRequest{
			App: 1, IDs: []int64{7},
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if _, has := gotBody["revisions"]; has {
			t.Errorf("revisions should not be set: %v", gotBody)
		}
	})

	t.Run("KW-Delete-3 App=0 → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		err := fx.client.DeleteRecords(context.Background(), DeleteRecordsRequest{
			App: 0, IDs: []int64{1},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KW-Delete-4 IDs 空 → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		err := fx.client.DeleteRecords(context.Background(), DeleteRecordsRequest{
			App: 1, IDs: nil,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KW-Delete-5 401 透過", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"code":"CB_AU01","message":"auth"}`)
		}, RetryPolicy{MaxAttempts: 1})
		err := fx.client.DeleteRecords(context.Background(), DeleteRecordsRequest{
			App: 1, IDs: []int64{1},
		})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected APIError, got %v", err)
		}
		if apiErr.Category != CategoryUnauthorized {
			t.Errorf("category=%v", apiErr.Category)
		}
	})
}
