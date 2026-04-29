package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/youyo/kintone/internal/auth"
	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// SA-1: NewFromKintone 正常生成
func TestNewFromKintone_OK(t *testing.T) {
	a, _ := auth.NewAPITokenAuthenticator("t")
	c, err := kintoneapi.New(kintoneapi.ClientOptions{Domain: "x.example.com", Authenticator: a})
	if err != nil {
		t.Fatalf("kintoneapi.New: %v", err)
	}
	sc, err := serviceapi.NewFromKintone(c)
	if err != nil {
		t.Fatalf("NewFromKintone: %v", err)
	}
	if sc == nil {
		t.Fatal("expected non-nil")
	}
}

// SA-2: NewFromKintone(nil) → ErrNilClient
func TestNewFromKintone_Nil(t *testing.T) {
	_, err := serviceapi.NewFromKintone(nil)
	if !errors.Is(err, serviceapi.ErrNilClient) {
		t.Errorf("got %v, want ErrNilClient", err)
	}
}

// rerouteTransport は任意のホスト宛 HTTP リクエストを httptest.Server に向ける。
// service/api 経由でも httptest を使った統合テストを書くために必要。
type rerouteTransport struct {
	target string // "http://127.0.0.1:NNNN"
	base   http.RoundTripper
}

func (r *rerouteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	host, err := parseHostPort(r.target)
	if err != nil {
		return nil, err
	}
	clone.URL.Host = host
	clone.Host = ""
	return r.base.RoundTrip(clone)
}

// parseHostPort は "http://host:port" から "host:port" を抜き出す。
func parseHostPort(target string) (string, error) {
	const prefix = "http://"
	if len(target) < len(prefix) || target[:len(prefix)] != prefix {
		return "", errors.New("invalid target")
	}
	return target[len(prefix):], nil
}

// newServiceAPIWithMock は httptest server へリクエストを誘導する serviceapi.Client を返す。
func newServiceAPIWithMock(t *testing.T, handler http.HandlerFunc) (*serviceapi.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	a, _ := auth.NewAPITokenAuthenticator("t")
	httpc := &http.Client{
		Transport: &rerouteTransport{target: srv.URL, base: http.DefaultTransport},
	}
	kc, err := kintoneapi.New(kintoneapi.ClientOptions{
		Domain:        "x.example.com",
		Authenticator: a,
		HTTPClient:    httpc,
	})
	if err != nil {
		t.Fatalf("kintoneapi.New: %v", err)
	}
	sc, err := serviceapi.NewFromKintone(kc)
	if err != nil {
		t.Fatalf("NewFromKintone: %v", err)
	}
	return sc, srv
}

// SA-3: GetRecords 透過
func TestClient_GetRecords(t *testing.T) {
	var gotPath string
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"records":[{"name":{"value":"foo"}}],"totalCount":"1"}`)
	})
	resp, err := sc.GetRecords(context.Background(), kintoneapi.GetRecordsRequest{App: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotPath != "/k/v1/records.json" {
		t.Errorf("path=%q", gotPath)
	}
	if len(resp.Records) != 1 {
		t.Errorf("records=%v", resp.Records)
	}
	if resp.TotalCount == nil || *resp.TotalCount != "1" {
		t.Errorf("totalCount=%v", resp.TotalCount)
	}
}

// SA-4: GetRecord 透過
func TestClient_GetRecord(t *testing.T) {
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"record":{"name":{"value":"foo"}}}`)
	})
	resp, err := sc.GetRecord(context.Background(), kintoneapi.GetRecordRequest{App: 1, ID: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Record == nil {
		t.Fatal("nil record")
	}
}

// SA-5: GetApp 透過
func TestClient_GetApp(t *testing.T) {
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"appId":"42","code":"x","name":"テスト"}`)
	})
	resp, err := sc.GetApp(context.Background(), kintoneapi.GetAppRequest{ID: 42})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.AppID != "42" || resp.Name != "テスト" {
		t.Errorf("resp=%+v", resp)
	}
}

// SA-6: GetFormFields 透過
func TestClient_GetFormFields(t *testing.T) {
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"properties":{"name":{"type":"SINGLE_LINE_TEXT"}},"revision":"3"}`)
	})
	resp, err := sc.GetFormFields(context.Background(), kintoneapi.GetFormFieldsRequest{App: 1})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Revision != "3" {
		t.Errorf("revision=%q", resp.Revision)
	}
	if _, ok := resp.Properties["name"]; !ok {
		t.Errorf("properties=%v", resp.Properties)
	}
}

// SA-7: エラー透過（kintoneapi.APIError がそのまま伝播する）
func TestClient_ErrorPassThrough(t *testing.T) {
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"code":"GAIA_AP01","message":"not found"}`)
	})
	_, err := sc.GetApp(context.Background(), kintoneapi.GetAppRequest{ID: 999})
	var apiErr *kintoneapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.Category != kintoneapi.CategoryNotFound {
		t.Errorf("category=%v", apiErr.Category)
	}
}

// SA-W-1: InsertRecords 透過
func TestClient_InsertRecords(t *testing.T) {
	var gotMethod, gotPath string
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"ids":["10"],"revisions":["1"]}`)
	})
	resp, err := sc.InsertRecords(context.Background(), kintoneapi.InsertRecordsRequest{
		App: 1, Records: []map[string]any{{"x": map[string]any{"value": "v"}}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/k/v1/records.json" {
		t.Errorf("method=%s path=%s", gotMethod, gotPath)
	}
	if len(resp.IDs) != 1 || resp.IDs[0] != "10" {
		t.Errorf("ids=%v", resp.IDs)
	}
}

// SA-W-2: UpdateRecord 透過
func TestClient_UpdateRecord(t *testing.T) {
	var gotMethod string
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_, _ = io.WriteString(w, `{"revision":"4"}`)
	})
	resp, err := sc.UpdateRecord(context.Background(), kintoneapi.UpdateRecordRequest{
		App: 1, ID: 1, Record: map[string]any{"x": map[string]any{"value": "v"}},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method=%s", gotMethod)
	}
	if resp.Revision != "4" {
		t.Errorf("revision=%q", resp.Revision)
	}
}

// SA-W-3: DeleteRecords 透過
func TestClient_DeleteRecords(t *testing.T) {
	var gotMethod, gotPath string
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{}`)
	})
	err := sc.DeleteRecords(context.Background(), kintoneapi.DeleteRecordsRequest{
		App: 1, IDs: []int64{7},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/k/v1/records.json" {
		t.Errorf("method=%s path=%s", gotMethod, gotPath)
	}
}

// SA-W-4: write 系のエラー透過（422）
func TestClient_InsertRecords_ErrorPassThrough(t *testing.T) {
	sc, _ := newServiceAPIWithMock(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_, _ = io.WriteString(w, `{"code":"CB_VA01","message":"validation"}`)
	})
	_, err := sc.InsertRecords(context.Background(), kintoneapi.InsertRecordsRequest{
		App: 1, Records: []map[string]any{{"x": 1}},
	})
	var apiErr *kintoneapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.Category != kintoneapi.CategoryValidation {
		t.Errorf("category=%v", apiErr.Category)
	}
}
