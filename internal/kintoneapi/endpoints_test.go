package kintoneapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestGetRecords(t *testing.T) {
	t.Parallel()

	t.Run("EP-Records-1 全 query 送信", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		var gotPath string
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"records":[],"totalCount":"3"}`)
		})
		resp, err := fx.client.GetRecords(context.Background(), GetRecordsRequest{
			App: 42, Query: "name = \"foo\"", Fields: []string{"name", "age"}, TotalCount: true,
		})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotPath != "/k/v1/records.json" {
			t.Fatalf("path=%q", gotPath)
		}
		if gotQuery.Get("app") != "42" {
			t.Fatalf("app=%v", gotQuery)
		}
		if gotQuery.Get("query") != `name = "foo"` {
			t.Fatalf("query=%v", gotQuery)
		}
		if !reflect.DeepEqual(gotQuery["fields"], []string{"name", "age"}) {
			t.Fatalf("fields=%v", gotQuery["fields"])
		}
		if gotQuery.Get("totalCount") != "true" {
			t.Fatalf("totalCount=%v", gotQuery)
		}
		if resp.TotalCount == nil || *resp.TotalCount != "3" {
			t.Fatalf("totalCount=%v", resp.TotalCount)
		}
	})

	t.Run("EP-Records-2 最小", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"records":[]}`)
		})
		_, err := fx.client.GetRecords(context.Background(), GetRecordsRequest{App: 1})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotQuery.Get("app") != "1" {
			t.Fatalf("app=%v", gotQuery)
		}
		if _, has := gotQuery["query"]; has {
			t.Fatalf("query should not be set: %v", gotQuery)
		}
		if _, has := gotQuery["fields"]; has {
			t.Fatalf("fields should not be set: %v", gotQuery)
		}
		if _, has := gotQuery["totalCount"]; has {
			t.Fatalf("totalCount should not be set: %v", gotQuery)
		}
	})

	t.Run("EP-Records-3 422 validation", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(422)
			_, _ = io.WriteString(w, `{"code":"CB_VA01","message":"validation error"}`)
		}, RetryPolicy{MaxAttempts: 1, RetryOn: []int{}})
		_, err := fx.client.GetRecords(context.Background(), GetRecordsRequest{App: 1})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected APIError, got %v", err)
		}
		if apiErr.Category != CategoryValidation {
			t.Fatalf("cat=%v", apiErr.Category)
		}
	})

	t.Run("App 必須バリデーション", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") })
		_, err := fx.client.GetRecords(context.Background(), GetRecordsRequest{App: 0})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGetRecord(t *testing.T) {
	t.Parallel()

	t.Run("EP-Record-1", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		var gotPath string
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"record":{"name":{"value":"foo"}}}`)
		})
		resp, err := fx.client.GetRecord(context.Background(), GetRecordRequest{App: 42, ID: 7})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotPath != "/k/v1/record.json" {
			t.Fatalf("path=%q", gotPath)
		}
		if gotQuery.Get("app") != "42" || gotQuery.Get("id") != "7" {
			t.Fatalf("query=%v", gotQuery)
		}
		if resp.Record == nil {
			t.Fatal("nil record")
		}
	})

	t.Run("App/ID 必須", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") })
		if _, err := fx.client.GetRecord(context.Background(), GetRecordRequest{App: 0, ID: 1}); err == nil {
			t.Fatal("expected error")
		}
		if _, err := fx.client.GetRecord(context.Background(), GetRecordRequest{App: 1, ID: 0}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGetApp(t *testing.T) {
	t.Parallel()

	t.Run("EP-App-1", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		var gotPath string
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"appId":"42","code":"foo","name":"テスト"}`)
		})
		resp, err := fx.client.GetApp(context.Background(), GetAppRequest{ID: 42})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotPath != "/k/v1/app.json" {
			t.Fatalf("path=%q", gotPath)
		}
		if gotQuery.Get("id") != "42" {
			t.Fatalf("query=%v", gotQuery)
		}
		if resp.AppID != "42" || resp.Name != "テスト" {
			t.Fatalf("resp=%+v", resp)
		}
	})

	t.Run("ID 必須", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") })
		if _, err := fx.client.GetApp(context.Background(), GetAppRequest{ID: 0}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGetFormFields(t *testing.T) {
	t.Parallel()

	t.Run("EP-Fields-1 lang 指定", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		var gotPath string
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"properties":{},"revision":"1"}`)
		})
		_, err := fx.client.GetFormFields(context.Background(), GetFormFieldsRequest{App: 42, Lang: "ja"})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotPath != "/k/v1/app/form/fields.json" {
			t.Fatalf("path=%q", gotPath)
		}
		if gotQuery.Get("app") != "42" || gotQuery.Get("lang") != "ja" {
			t.Fatalf("query=%v", gotQuery)
		}
	})

	t.Run("EP-Fields-2 lang 省略", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"properties":{}}`)
		})
		_, err := fx.client.GetFormFields(context.Background(), GetFormFieldsRequest{App: 42})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if _, has := gotQuery["lang"]; has {
			t.Fatalf("lang should not be set: %v", gotQuery)
		}
	})

	t.Run("App 必須", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not call") })
		if _, err := fx.client.GetFormFields(context.Background(), GetFormFieldsRequest{App: 0}); err == nil {
			t.Fatal("expected error")
		}
	})
}
