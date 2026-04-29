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

// TestListApps は GET /k/v1/apps.json を確認する。
func TestListApps(t *testing.T) {
	t.Parallel()

	t.Run("AS-1 クエリなし", func(t *testing.T) {
		t.Parallel()
		var gotPath string
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"apps":[]}`)
		})
		resp, err := fx.client.ListApps(context.Background(), ListAppsRequest{})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotPath != "/k/v1/apps.json" {
			t.Fatalf("path=%q", gotPath)
		}
		// limit/offset/その他クエリパラメータ無し
		for _, k := range []string{"limit", "offset", "name"} {
			if _, has := gotQuery[k]; has {
				t.Fatalf("%s should not be set: %v", k, gotQuery)
			}
		}
		for _, k := range []string{"ids[0]", "codes[0]", "spaceIds[0]"} {
			if _, has := gotQuery[k]; has {
				t.Fatalf("%s should not be set: %v", k, gotQuery)
			}
		}
		if resp == nil || len(resp.Apps) != 0 {
			t.Fatalf("resp=%+v", resp)
		}
	})

	t.Run("AS-2 IDs", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"apps":[]}`)
		})
		_, err := fx.client.ListApps(context.Background(), ListAppsRequest{IDs: []int64{1, 2, 3}})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if got := []string{gotQuery.Get("ids[0]"), gotQuery.Get("ids[1]"), gotQuery.Get("ids[2]")}; !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
			t.Fatalf("ids=%v full=%v", got, gotQuery)
		}
	})

	t.Run("AS-3 Codes", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"apps":[]}`)
		})
		_, err := fx.client.ListApps(context.Background(), ListAppsRequest{Codes: []string{"A", "B"}})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotQuery.Get("codes[0]") != "A" || gotQuery.Get("codes[1]") != "B" {
			t.Fatalf("codes=%v", gotQuery)
		}
	})

	t.Run("AS-4 Name", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"apps":[]}`)
		})
		_, err := fx.client.ListApps(context.Background(), ListAppsRequest{Name: "hr"})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotQuery.Get("name") != "hr" {
			t.Fatalf("name=%v", gotQuery)
		}
	})

	t.Run("AS-5 SpaceIDs", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"apps":[]}`)
		})
		_, err := fx.client.ListApps(context.Background(), ListAppsRequest{SpaceIDs: []int64{10}})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotQuery.Get("spaceIds[0]") != "10" {
			t.Fatalf("spaceIds=%v", gotQuery)
		}
	})

	t.Run("AS-6 Limit/Offset", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"apps":[]}`)
		})
		_, err := fx.client.ListApps(context.Background(), ListAppsRequest{Limit: 50, Offset: 10})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if gotQuery.Get("limit") != "50" || gotQuery.Get("offset") != "10" {
			t.Fatalf("limit/offset=%v", gotQuery)
		}
	})

	t.Run("AS-7 Limit=0/Offset=0 はクエリ未指定", func(t *testing.T) {
		t.Parallel()
		var gotQuery url.Values
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query()
			_, _ = io.WriteString(w, `{"apps":[]}`)
		})
		_, err := fx.client.ListApps(context.Background(), ListAppsRequest{Limit: 0, Offset: 0})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if _, has := gotQuery["limit"]; has {
			t.Fatalf("limit should not be set: %v", gotQuery)
		}
		if _, has := gotQuery["offset"]; has {
			t.Fatalf("offset should not be set: %v", gotQuery)
		}
	})

	t.Run("AS-8 レスポンスデコード", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"apps":[{"appId":"1","code":"hr","name":"人事","description":"","spaceId":"5","threadId":"6","createdAt":"2025-01-01T00:00:00Z","creator":{"code":"u1","name":"ユーザー1"},"modifiedAt":"2025-02-01T00:00:00Z","modifier":{"code":"u1","name":"ユーザー1"}}]}`)
		})
		resp, err := fx.client.ListApps(context.Background(), ListAppsRequest{})
		if err != nil {
			t.Fatalf("%v", err)
		}
		if len(resp.Apps) != 1 {
			t.Fatalf("apps=%v", resp.Apps)
		}
		a := resp.Apps[0]
		if a.AppID != "1" || a.Code != "hr" || a.Name != "人事" || a.SpaceID != "5" || a.ThreadID != "6" {
			t.Fatalf("app=%+v", a)
		}
		if a.Creator["code"] != "u1" {
			t.Fatalf("creator=%v", a.Creator)
		}
	})

	t.Run("AS-9 401 → APIError(Unauthorized)", func(t *testing.T) {
		t.Parallel()
		fx := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"code":"CB_AU01","message":"unauthorized"}`)
		}, RetryPolicy{MaxAttempts: 1, RetryOn: []int{}})
		_, err := fx.client.ListApps(context.Background(), ListAppsRequest{})
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("expected APIError, got %v", err)
		}
		if apiErr.Category != CategoryUnauthorized {
			t.Fatalf("cat=%v", apiErr.Category)
		}
	})
}
