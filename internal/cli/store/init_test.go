package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	clistore "github.com/youyo/kintone/internal/cli/store"
)

// fakeClient は dynamoDBAPI を実装するテスト用 fake。
type fakeClient struct {
	describeTableOutput *awsdynamodb.DescribeTableOutput
	describeTableErr    error
	describeTTLOutput   *awsdynamodb.DescribeTimeToLiveOutput
	describeTTLErr      error
}

func (f *fakeClient) DescribeTable(_ context.Context, _ *awsdynamodb.DescribeTableInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.DescribeTableOutput, error) {
	return f.describeTableOutput, f.describeTableErr
}

func (f *fakeClient) DescribeTimeToLive(_ context.Context, _ *awsdynamodb.DescribeTimeToLiveInput, _ ...func(*awsdynamodb.Options)) (*awsdynamodb.DescribeTimeToLiveOutput, error) {
	return f.describeTTLOutput, f.describeTTLErr
}

// buildTableDesc は完全な TableDescription を生成するヘルパー。
func buildTableDesc(attrs []string, indexes []string) *awsdynamodb.DescribeTableOutput {
	attrDefs := make([]types.AttributeDefinition, 0, len(attrs))
	for _, a := range attrs {
		name := a
		attrDefs = append(attrDefs, types.AttributeDefinition{
			AttributeName: &name,
			AttributeType: types.ScalarAttributeTypeS,
		})
	}
	gsis := make([]types.GlobalSecondaryIndexDescription, 0, len(indexes))
	for _, idx := range indexes {
		name := idx
		gsis = append(gsis, types.GlobalSecondaryIndexDescription{
			IndexName: &name,
		})
	}
	tableName := "test-table"
	return &awsdynamodb.DescribeTableOutput{
		Table: &types.TableDescription{
			TableName:              &tableName,
			AttributeDefinitions:   attrDefs,
			GlobalSecondaryIndexes: gsis,
		},
	}
}

func enabledTTL() *awsdynamodb.DescribeTimeToLiveOutput {
	status := types.TimeToLiveStatusEnabled
	return &awsdynamodb.DescribeTimeToLiveOutput{
		TimeToLiveDescription: &types.TimeToLiveDescription{
			TimeToLiveStatus: status,
		},
	}
}

func disabledTTL() *awsdynamodb.DescribeTimeToLiveOutput {
	status := types.TimeToLiveStatusDisabled
	return &awsdynamodb.DescribeTimeToLiveOutput{
		TimeToLiveDescription: &types.TimeToLiveDescription{
			TimeToLiveStatus: status,
		},
	}
}

// parseOutput は runInit が out に書いた JSON をデコードするヘルパー。
func parseOutput(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil {
		t.Fatalf("JSON parse error: %v\nraw: %s", err, buf.String())
	}
	return m
}

// fullAttrs は capability=full に必要な全属性。
var fullAttrs = []string{"pk", "gsi1pk", "gsi1sk", "gsi2pk", "gsi2sk", "ttl"}

// fullIndexes は capability=full に必要な全 GSI。
var fullIndexes = []string{"gsi1", "gsi2"}

// --- Test cases ---

// IT-1: テーブル不在 → STORE_TABLE_NOT_FOUND
func TestRunInit_TableNotFound(t *testing.T) {
	nfe := &types.ResourceNotFoundException{}
	fake := &fakeClient{describeTableErr: nfe}

	opts := clistore.InitOptions{
		Table:      "missing-table",
		Region:     "ap-northeast-1",
		Capability: "full",
		Client:     fake,
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	m := parseOutput(t, &buf)
	if ok, _ := m["ok"].(bool); ok {
		t.Errorf("expected ok=false, got ok=true")
	}
	errMap, _ := m["error"].(map[string]any)
	if errMap == nil {
		t.Fatal("expected error object in JSON")
	}
	if code, _ := errMap["code"].(string); code != "STORE_TABLE_NOT_FOUND" {
		t.Errorf("code=%q want STORE_TABLE_NOT_FOUND", code)
	}
	details, _ := errMap["details"].(map[string]any)
	if details == nil {
		t.Fatal("expected details in error")
	}
	if table, _ := details["table"].(string); table != "missing-table" {
		t.Errorf("details.table=%q want missing-table", table)
	}
}

// IT-2: GSI1 不足 (capability=token) → STORE_GSI_MISSING + missing_indexes に gsi1
func TestRunInit_GSI1Missing_Token(t *testing.T) {
	fake := &fakeClient{
		describeTableOutput: buildTableDesc([]string{"pk"}, nil), // gsi1pk/gsi1sk/gsi1 なし
		describeTTLOutput:   enabledTTL(),
	}

	opts := clistore.InitOptions{
		Table:      "test-table",
		Capability: "token",
		Client:     fake,
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	m := parseOutput(t, &buf)
	errMap, _ := m["error"].(map[string]any)
	if errMap == nil {
		t.Fatal("expected error object")
	}
	if code, _ := errMap["code"].(string); code != "STORE_GSI_MISSING" {
		t.Errorf("code=%q want STORE_GSI_MISSING", code)
	}
	details, _ := errMap["details"].(map[string]any)
	if details == nil {
		t.Fatal("expected details")
	}
	if cap, _ := details["capability"].(string); cap != "token" {
		t.Errorf("details.capability=%q want token", cap)
	}
	missingIndexes, _ := details["missing_indexes"].([]any)
	if len(missingIndexes) == 0 {
		t.Errorf("expected missing_indexes to be non-empty, got %v", details["missing_indexes"])
	}
}

// IT-3: GSI2 不足 (capability=cache) → STORE_GSI_MISSING + missing_indexes に gsi2
func TestRunInit_GSI2Missing_Cache(t *testing.T) {
	fake := &fakeClient{
		describeTableOutput: buildTableDesc([]string{"pk"}, nil), // gsi2* なし
		describeTTLOutput:   enabledTTL(),
	}

	opts := clistore.InitOptions{
		Table:      "test-table",
		Capability: "cache",
		Client:     fake,
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	m := parseOutput(t, &buf)
	errMap, _ := m["error"].(map[string]any)
	if code, _ := errMap["code"].(string); code != "STORE_GSI_MISSING" {
		t.Errorf("code=%q want STORE_GSI_MISSING", code)
	}
	details, _ := errMap["details"].(map[string]any)
	if cap, _ := details["capability"].(string); cap != "cache" {
		t.Errorf("details.capability=%q want cache", cap)
	}
}

// IT-4: TTL 無効 (capability=idproxy) → STORE_TTL_DISABLED
func TestRunInit_TTLDisabled_IDProxy(t *testing.T) {
	fake := &fakeClient{
		describeTableOutput: buildTableDesc([]string{"pk", "ttl"}, nil),
		describeTTLOutput:   disabledTTL(),
	}

	opts := clistore.InitOptions{
		Table:      "test-table",
		Region:     "us-east-1",
		Capability: "idproxy",
		Client:     fake,
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	m := parseOutput(t, &buf)
	errMap, _ := m["error"].(map[string]any)
	if code, _ := errMap["code"].(string); code != "STORE_TTL_DISABLED" {
		t.Errorf("code=%q want STORE_TTL_DISABLED", code)
	}
	details, _ := errMap["details"].(map[string]any)
	if details == nil {
		t.Fatal("expected details")
	}
	if _, ok := details["suggested_command"]; !ok {
		t.Errorf("expected suggested_command in details")
	}
}

// IT-5: capability=signingkey でテーブル存在のみ → 成功
func TestRunInit_SigningKeySuccess(t *testing.T) {
	fake := &fakeClient{
		describeTableOutput: buildTableDesc([]string{"pk"}, nil),
		describeTTLOutput:   enabledTTL(),
	}

	opts := clistore.InitOptions{
		Table:      "test-table",
		Region:     "ap-northeast-1",
		Capability: "signingkey",
		Client:     fake,
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	m := parseOutput(t, &buf)
	if ok, _ := m["ok"].(bool); !ok {
		t.Errorf("expected ok=true, got ok=false")
	}
	data, _ := m["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected data in response")
	}
	if verified, _ := data["verified"].(bool); !verified {
		t.Errorf("expected verified=true")
	}
	if cap, _ := data["capability"].(string); cap != "signingkey" {
		t.Errorf("data.capability=%q want signingkey", cap)
	}
}

// IT-6: capability=full で全属性・全GSI・TTL有効 → 成功
func TestRunInit_FullSuccess(t *testing.T) {
	fake := &fakeClient{
		describeTableOutput: buildTableDesc(fullAttrs, fullIndexes),
		describeTTLOutput:   enabledTTL(),
	}

	opts := clistore.InitOptions{
		Table:      "kintone-store",
		Region:     "ap-northeast-1",
		Capability: "full",
		Client:     fake,
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	m := parseOutput(t, &buf)
	if ok, _ := m["ok"].(bool); !ok {
		t.Errorf("expected ok=true")
	}
	data, _ := m["data"].(map[string]any)
	if table, _ := data["table"].(string); table != "kintone-store" {
		t.Errorf("data.table=%q want kintone-store", table)
	}
	if region, _ := data["region"].(string); region != "ap-northeast-1" {
		t.Errorf("data.region=%q want ap-northeast-1", region)
	}
}

// IT-7: --table 省略 → エラー（USAGE）
func TestRunInit_TableRequired(t *testing.T) {
	opts := clistore.InitOptions{
		Table:      "",
		Capability: "full",
		Client:     &fakeClient{},
	}
	var buf bytes.Buffer
	err := clistore.RunInit(context.Background(), &buf, opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
