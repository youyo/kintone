package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/youyo/kintone/internal/cli"
	"github.com/youyo/kintone/internal/kintoneapi"
)

// ---- create ----

// CC-Create-1: 単件正常
func TestOpsRecordCreate_Single(t *testing.T) {
	stub := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"10"}, Revisions: []string{"1"}}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--app", "42",
		"--record-json", `{"name":{"value":"x"}}`,
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / out=%s", err, out.String())
	}
	if stub.gotInsert == nil || len(stub.gotInsert.Records) != 1 {
		t.Errorf("Records=%v", stub.gotInsert)
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			IDs       []int64 `json:"ids"`
			Revisions []int64 `json:"revisions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v / %s", err, out.String())
	}
	if !got.OK || len(got.Data.IDs) != 1 || got.Data.IDs[0] != 10 {
		t.Errorf("got=%+v out=%s", got, out.String())
	}
}

// CC-Create-2: 複数件正常
func TestOpsRecordCreate_Multiple(t *testing.T) {
	stub := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"10", "11"}, Revisions: []string{"1", "1"}}, nil
		},
	}
	withStubAPI(t, stub)

	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--app", "42",
		"--records-json", `[{"a":{"value":"1"}},{"b":{"value":"2"}}]`,
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(stub.gotInsert.Records) != 2 {
		t.Errorf("records len=%d", len(stub.gotInsert.Records))
	}
}

// CC-Create-3: 両方指定 → USAGE
func TestOpsRecordCreate_BothSpecified_Usage(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--app", "1",
		"--record-json", `{}`,
		"--records-json", `[{}]`,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CC-Create-4: どちらも未指定 → USAGE
func TestOpsRecordCreate_NoneSpecified_Usage(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"ops", "record", "create", "--app", "42"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CC-Create-5: --app 必須
func TestOpsRecordCreate_MissingApp(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"ops", "record", "create", "--record-json", `{}`}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CC-Create-6: dry-run
func TestOpsRecordCreate_DryRun(t *testing.T) {
	stub := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			t.Fatal("Insert should not be called for dry-run")
			return nil, nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--app", "42",
		"--record-json", `{"name":{"value":"x"}}`,
		"--dry-run",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / out=%s", err, out.String())
	}
	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			DryRun bool           `json:"dry_run"`
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json: %v / %s", err, out.String())
	}
	if !got.OK || !got.Data.DryRun || got.Data.Method != "POST" || got.Data.Path != "/k/v1/records.json" {
		t.Errorf("got=%+v", got)
	}
	if got.Data.Body == nil || got.Data.Body["app"] == nil {
		t.Errorf("body missing app: %v", got.Data.Body)
	}
}

// CC-Create-7: 不正 JSON
func TestOpsRecordCreate_InvalidJSON(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "create",
		"--app", "42",
		"--record-json", `not json`,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// ---- update ----

// CC-Update-1: ID 指定正常
func TestOpsRecordUpdate_ByID(t *testing.T) {
	stub := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "3"}, nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "42", "--id", "7",
		"--record-json", `{"x":{"value":"v"}}`,
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / out=%s", err, out.String())
	}
	if stub.gotUpdate.ID != 7 {
		t.Errorf("ID=%d", stub.gotUpdate.ID)
	}
}

// CC-Update-2: updateKey 指定正常
func TestOpsRecordUpdate_ByUpdateKey(t *testing.T) {
	stub := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "1"}, nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "42",
		"--update-key-field", "code", "--update-key-value", "A1",
		"--record-json", `{"x":{"value":"v"}}`,
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stub.gotUpdate.UpdateKey == nil || stub.gotUpdate.UpdateKey.Field != "code" || stub.gotUpdate.UpdateKey.Value != "A1" {
		t.Errorf("UpdateKey=%+v", stub.gotUpdate.UpdateKey)
	}
}

// CC-Update-3: revision 指定
func TestOpsRecordUpdate_WithRevision(t *testing.T) {
	stub := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return &kintoneapi.UpdateRecordResponse{Revision: "4"}, nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "42", "--id", "7",
		"--revision", "3",
		"--record-json", `{"x":{"value":"v"}}`,
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if stub.gotUpdate.Revision == nil || *stub.gotUpdate.Revision != 3 {
		t.Errorf("Revision=%v", stub.gotUpdate.Revision)
	}
}

// CC-Update-4: --id と --update-key-* 両方 → USAGE
func TestOpsRecordUpdate_Conflicting_Usage(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "1", "--id", "1",
		"--update-key-field", "c", "--update-key-value", "v",
		"--record-json", `{"x":1}`,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CC-Update-5: --id も --update-key-* も無し → USAGE
func TestOpsRecordUpdate_Missing_Usage(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "1",
		"--record-json", `{"x":1}`,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CC-Update-6: --record-json 必須
func TestOpsRecordUpdate_MissingRecordJSON(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "1", "--id", "7",
	}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CC-Update-7: dry-run
func TestOpsRecordUpdate_DryRun(t *testing.T) {
	stub := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			t.Fatal("Update should not be called for dry-run")
			return nil, nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "42", "--id", "7",
		"--record-json", `{"x":{"value":"v"}}`,
		"--dry-run",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), `"method":"PUT"`) {
		t.Errorf("expected method=PUT: %s", out.String())
	}
}

// CC-Update-8: API エラー透過 → KINTONE_VALIDATION
func TestOpsRecordUpdate_APIError(t *testing.T) {
	apiErr := &kintoneapi.APIError{HTTPStatus: 422, Category: kintoneapi.CategoryValidation, Message: "validation"}
	stub := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			return nil, apiErr
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "update",
		"--app", "1", "--id", "1",
		"--record-json", `{"x":{"value":"v"}}`,
	}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"KINTONE_VALIDATION"`) {
		t.Errorf("expected KINTONE_VALIDATION: %s", out.String())
	}
}

// ---- delete ----

// CC-Delete-1 / CC-Delete-2: 単一・複数
func TestOpsRecordDelete_OK(t *testing.T) {
	stub := &stubAPI{}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "delete",
		"--app", "42",
		"--id", "7", "--id", "8",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v / out=%s", err, out.String())
	}
	if len(stub.gotDelete.IDs) != 2 || stub.gotDelete.IDs[0] != 7 || stub.gotDelete.IDs[1] != 8 {
		t.Errorf("IDs=%v", stub.gotDelete.IDs)
	}
	if !strings.Contains(out.String(), `"deleted":2`) {
		t.Errorf("expected deleted:2 in %s", out.String())
	}
}

// CC-Delete-3: revisions 指定
func TestOpsRecordDelete_WithRevisions(t *testing.T) {
	stub := &stubAPI{}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "delete",
		"--app", "42",
		"--id", "7", "--id", "8",
		"--revision", "3", "--revision", "4",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(stub.gotDelete.Revisions) != 2 || stub.gotDelete.Revisions[0] != 3 {
		t.Errorf("Revisions=%v", stub.gotDelete.Revisions)
	}
}

// CC-Delete-4: revisions 数不一致 → USAGE
func TestOpsRecordDelete_RevisionsMismatch_Usage(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{
		"ops", "record", "delete",
		"--app", "42",
		"--id", "7", "--id", "8",
		"--revision", "3",
	}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CC-Delete-5: --id 必須（明示的 RunE 判定）
func TestOpsRecordDelete_MissingID(t *testing.T) {
	withStubAPI(t, &stubAPI{})
	var out, errOut bytes.Buffer
	err := cli.ExecuteWith([]string{"ops", "record", "delete", "--app", "42"}, &out, &errOut)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(out.String(), `"USAGE"`) {
		t.Errorf("expected USAGE: %s", out.String())
	}
}

// CC-Delete-6: dry-run
func TestOpsRecordDelete_DryRun(t *testing.T) {
	stub := &stubAPI{
		deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
			t.Fatal("Delete should not be called for dry-run")
			return nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith([]string{
		"ops", "record", "delete",
		"--app", "42", "--id", "7",
		"--dry-run",
	}, &out, &errOut); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), `"method":"DELETE"`) {
		t.Errorf("expected method=DELETE: %s", out.String())
	}
}

// ---- dry-run body byte equivalence（advisor 指摘 #2）----
//
// dry-run の body と実 API 送信時の body が **完全一致** することを検証する。
// 万一片方が独自に body を組み立てた場合、本テストが失敗してリグレッションを検知する。

// runOpsAndCaptureDryRunBody は --dry-run で得られる data.body を返す。
func runOpsAndCaptureDryRunBody(t *testing.T, args []string) map[string]any {
	t.Helper()
	withStubAPI(t, &stubAPI{}) // 通常実行されない（dry-run のため）
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith(append(args, "--dry-run"), &out, &errOut); err != nil {
		t.Fatalf("dry-run Execute: %v / out=%s", err, out.String())
	}
	var got struct {
		Data struct {
			Body map[string]any `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("dry-run json: %v", err)
	}
	return got.Data.Body
}

// CC-DryRunEquiv-Create: dry-run body == 実送信時に kintoneapi に渡される body
func TestOpsRecordCreate_DryRunBodyEquivalence(t *testing.T) {
	args := []string{
		"ops", "record", "create",
		"--app", "42",
		"--record-json", `{"name":{"value":"x"}}`,
	}

	// 1. dry-run の body を取得
	dryBody := runOpsAndCaptureDryRunBody(t, append([]string(nil), args...))

	// 2. 実送信時の body を kintoneapi.BuildInsertRecordsBody から取得
	var sentBody map[string]any
	stub := &stubAPI{
		insertRecordsFn: func(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
			sentBody = kintoneapi.BuildInsertRecordsBody(req)
			return &kintoneapi.InsertRecordsResponse{IDs: []string{"1"}, Revisions: []string{"1"}}, nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith(args, &out, &errOut); err != nil {
		t.Fatalf("real Execute: %v", err)
	}

	// 3. JSON 表現でバイト一致を確認（map の順序差を吸収）
	dryJSON, _ := json.Marshal(dryBody)
	sentJSON, _ := json.Marshal(sentBody)
	if string(dryJSON) != string(sentJSON) {
		t.Errorf("body diverged:\n  dry-run = %s\n  sent    = %s", dryJSON, sentJSON)
	}
}

// CC-DryRunEquiv-Update: 同上（PUT）
func TestOpsRecordUpdate_DryRunBodyEquivalence(t *testing.T) {
	args := []string{
		"ops", "record", "update",
		"--app", "42", "--id", "7",
		"--revision", "3",
		"--record-json", `{"name":{"value":"x"}}`,
	}

	dryBody := runOpsAndCaptureDryRunBody(t, append([]string(nil), args...))

	var sentBody map[string]any
	stub := &stubAPI{
		updateRecordFn: func(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
			sentBody = kintoneapi.BuildUpdateRecordBody(req)
			return &kintoneapi.UpdateRecordResponse{Revision: "4"}, nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith(args, &out, &errOut); err != nil {
		t.Fatalf("real Execute: %v", err)
	}

	dryJSON, _ := json.Marshal(dryBody)
	sentJSON, _ := json.Marshal(sentBody)
	if string(dryJSON) != string(sentJSON) {
		t.Errorf("body diverged:\n  dry-run = %s\n  sent    = %s", dryJSON, sentJSON)
	}
}

// CC-DryRunEquiv-Delete: 同上（DELETE）
func TestOpsRecordDelete_DryRunBodyEquivalence(t *testing.T) {
	args := []string{
		"ops", "record", "delete",
		"--app", "42",
		"--id", "7", "--id", "8",
		"--revision", "3", "--revision", "4",
	}

	dryBody := runOpsAndCaptureDryRunBody(t, append([]string(nil), args...))

	var sentBody map[string]any
	stub := &stubAPI{
		deleteRecordsFn: func(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
			sentBody = kintoneapi.BuildDeleteRecordsBody(req)
			return nil
		},
	}
	withStubAPI(t, stub)
	var out, errOut bytes.Buffer
	if err := cli.ExecuteWith(args, &out, &errOut); err != nil {
		t.Fatalf("real Execute: %v", err)
	}

	dryJSON, _ := json.Marshal(dryBody)
	sentJSON, _ := json.Marshal(sentBody)
	if string(dryJSON) != string(sentJSON) {
		t.Errorf("body diverged:\n  dry-run = %s\n  sent    = %s", dryJSON, sentJSON)
	}
}
