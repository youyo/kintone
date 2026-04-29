package kintoneapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
)

// TestUpdateRecord は PUT /k/v1/record.json の挙動。
func TestUpdateRecord(t *testing.T) {
	t.Parallel()

	t.Run("KU-Update-1 ID 指定 + revision なし", func(t *testing.T) {
		t.Parallel()
		var gotPath, gotMethod string
		var gotBody map[string]any
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotMethod = r.Method
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			_, _ = io.WriteString(w, `{"revision":"3"}`)
		})
		resp, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 42, ID: 7,
			Record: map[string]any{"name": map[string]any{"value": "x"}},
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if gotPath != "/k/v1/record.json" {
			t.Errorf("path=%q", gotPath)
		}
		if gotMethod != http.MethodPut {
			t.Errorf("method=%q", gotMethod)
		}
		if got, want := gotBody["app"], float64(42); got != want {
			t.Errorf("app=%v", got)
		}
		if got, want := gotBody["id"], float64(7); got != want {
			t.Errorf("id=%v", got)
		}
		if _, has := gotBody["updateKey"]; has {
			t.Errorf("updateKey should be absent: %v", gotBody)
		}
		if _, has := gotBody["revision"]; has {
			t.Errorf("revision should be absent: %v", gotBody)
		}
		if resp.Revision != "3" {
			t.Errorf("resp.Revision=%q", resp.Revision)
		}
	})

	t.Run("KU-Update-2 ID 指定 + revision あり", func(t *testing.T) {
		t.Parallel()
		var gotBody map[string]any
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			_, _ = io.WriteString(w, `{"revision":"6"}`)
		})
		rev := int64(5)
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 42, ID: 7, Revision: &rev,
			Record: map[string]any{"x": map[string]any{"value": "v"}},
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got, want := gotBody["revision"], float64(5); got != want {
			t.Errorf("revision=%v", got)
		}
	})

	t.Run("KU-Update-3 updateKey 指定", func(t *testing.T) {
		t.Parallel()
		var gotBody map[string]any
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			_, _ = io.WriteString(w, `{"revision":"4"}`)
		})
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App:       42,
			UpdateKey: &UpdateKey{Field: "code", Value: "A1"},
			Record:    map[string]any{"x": map[string]any{"value": "v"}},
		})
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if _, has := gotBody["id"]; has {
			t.Errorf("id should be absent: %v", gotBody)
		}
		uk, ok := gotBody["updateKey"].(map[string]any)
		if !ok {
			t.Fatalf("updateKey type: %v", gotBody["updateKey"])
		}
		if uk["field"] != "code" || uk["value"] != "A1" {
			t.Errorf("updateKey=%v", uk)
		}
	})

	t.Run("KU-Update-4 App=0 → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 0, ID: 1, Record: map[string]any{"x": 1},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KU-Update-5 ID + UpdateKey 両方 → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 1, ID: 1, UpdateKey: &UpdateKey{Field: "c", Value: "v"},
			Record: map[string]any{"x": 1},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KU-Update-6 ID も UpdateKey も無し → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 1, Record: map[string]any{"x": 1},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KU-Update-7 UpdateKey の Field 空 → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 1, UpdateKey: &UpdateKey{Field: "", Value: "v"},
			Record: map[string]any{"x": 1},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KU-Update-8 Record 空 → error", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not call")
		})
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 1, ID: 1, Record: map[string]any{},
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("KU-Update-9 422 透過", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(422)
			_, _ = io.WriteString(w, `{"code":"CB_VA01"}`)
		}, RetryPolicy{MaxAttempts: 1})
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 1, ID: 1, Record: map[string]any{"x": 1},
		})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected APIError, got %v", err)
		}
		if apiErr.Category != CategoryValidation {
			t.Errorf("category=%v", apiErr.Category)
		}
	})

	t.Run("KU-Update-10 retry 無効化（書き込み系は MaxAttempts=1 デフォルト）", func(t *testing.T) {
		t.Parallel()
		// fx.client.retry は DefaultRetryPolicy だが、UpdateRecord は doJSONWithBody 経由で
		// MaxAttempts=1 に強制される。503 を返してもリトライしないことを確認。
		var attempts int32
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attempts, 1)
			w.WriteHeader(503)
		})
		_, err := fx.client.UpdateRecord(context.Background(), UpdateRecordRequest{
			App: 1, ID: 1, Record: map[string]any{"x": 1},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if got := atomic.LoadInt32(&attempts); got != 1 {
			t.Errorf("attempts=%d want 1", got)
		}
	})
}
