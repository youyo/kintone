package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/spf13/cobra"

	"github.com/youyo/kintone/internal/output"
	storedynamo "github.com/youyo/kintone/internal/store/dynamodb"
)

// DynamoDBInitAPI は store init が必要とする DynamoDB クライアントの最小サーフェス。
// テストで fake を注入可能にするため export する。
// *awsdynamodb.Client はこの interface を満たす。
type DynamoDBInitAPI interface {
	DescribeTable(ctx context.Context, p *awsdynamodb.DescribeTableInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.DescribeTableOutput, error)
	DescribeTimeToLive(ctx context.Context, p *awsdynamodb.DescribeTimeToLiveInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.DescribeTimeToLiveOutput, error)
}

// InitOptions は RunInit への依存注入コンテナ。
// Client が nil の場合は AWS SDK から実際のクライアントを構築する。
type InitOptions struct {
	Table      string
	Region     string
	Capability string
	// Client はテスト用 fake 注入ポイント。nil なら LoadDefaultConfig で構築する。
	Client DynamoDBInitAPI
}

// missingReqs は capability チェックで検出された不足要件。
type missingReqs struct {
	Indexes     []string
	Attributes  []string
	TTLRequired bool
}

// newInitCmd は "kintone store init <backend>" サブコマンドを返す。
func newInitCmd() *cobra.Command {
	var table, region, capability string
	cmd := &cobra.Command{
		Use:   "init <backend>",
		Short: "Storage backend を初期化・検証する",
		Long: `DynamoDB テーブルのスキーマ（GSI / TTL）を capability 別に検証する。

capability:
  full        全機能 (token + cache + signingkey + idproxy)
  token       OAuth トークン管理のみ
  cache       API キャッシュのみ
  signingkey  OIDC 署名鍵のみ
  idproxy     idproxy セッション管理のみ`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backend := args[0]
			if backend != "dynamodb" {
				return fmt.Errorf("store init: unsupported backend %q (only dynamodb is supported)", backend)
			}
			opts := InitOptions{
				Table:      table,
				Region:     region,
				Capability: capability,
			}
			return RunInit(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVar(&table, "table", "", "DynamoDB テーブル名 (必須)")
	cmd.Flags().StringVar(&region, "region", "", "AWS リージョン (省略時は AWS 設定にフォールバック)")
	cmd.Flags().StringVar(&capability, "capability", "full", "検証範囲 (full|token|cache|signingkey|idproxy)")
	return cmd
}

// RunInit は DynamoDB テーブルの capability 別スキーマ検証を実行する。
// テストからは直接呼び出し、opts.Client で fake を注入する。
// 失敗時は out に output.Failure JSON を書き、non-nil error を返す。
// 成功時は out に output.Success JSON を書き、nil を返す。
func RunInit(ctx context.Context, out io.Writer, opts InitOptions) error {
	if opts.Table == "" {
		return writeFailure(out, &output.Error{
			Code:    "USAGE",
			Message: "store init: --table is required",
		})
	}

	cap := normalizeCapability(opts.Capability)

	client, resolvedRegion, err := resolveClient(ctx, opts)
	if err != nil {
		return writeFailure(out, &output.Error{
			Code:    "STORE_CONNECTION_FAILED",
			Message: fmt.Sprintf("store init: load aws config: %v", err),
		})
	}

	// DescribeTable
	descOut, err := client.DescribeTable(ctx, &awsdynamodb.DescribeTableInput{
		TableName: aws.String(opts.Table),
	})
	if err != nil {
		var nfe *types.ResourceNotFoundException
		if errors.As(err, &nfe) {
			return writeFailure(out, &output.Error{
				Code:    "STORE_TABLE_NOT_FOUND",
				Message: fmt.Sprintf("store init: table %q not found", opts.Table),
				Details: map[string]any{
					"table":  opts.Table,
					"region": resolvedRegion,
				},
			})
		}
		return writeFailure(out, &output.Error{
			Code:    "STORE_CONNECTION_FAILED",
			Message: fmt.Sprintf("store init: describe table: %v", err),
		})
	}

	// DescribeTimeToLive
	ttlOut, err := client.DescribeTimeToLive(ctx, &awsdynamodb.DescribeTimeToLiveInput{
		TableName: aws.String(opts.Table),
	})
	if err != nil {
		return writeFailure(out, &output.Error{
			Code:    "STORE_CONNECTION_FAILED",
			Message: fmt.Sprintf("store init: describe ttl: %v", err),
		})
	}

	// capability 別要件検証
	missing := checkRequirements(cap, descOut.Table, ttlOut.TimeToLiveDescription)

	if len(missing.Indexes) > 0 || len(missing.Attributes) > 0 {
		return writeFailure(out, &output.Error{
			Code:    "STORE_GSI_MISSING",
			Message: fmt.Sprintf("store init: DynamoDB table %q is missing required GSI/attributes for capability=%s", opts.Table, cap),
			Details: map[string]any{
				"capability":         cap,
				"missing_indexes":    missing.Indexes,
				"missing_attributes": missing.Attributes,
				"suggested_ddl":      buildDDLHint(opts.Table, cap, missing),
			},
		})
	}

	if missing.TTLRequired {
		return writeFailure(out, &output.Error{
			Code:    "STORE_TTL_DISABLED",
			Message: fmt.Sprintf("store init: TTL is not enabled on table %q (required for capability=%s)", opts.Table, cap),
			Details: map[string]any{
				"table": opts.Table,
				"suggested_command": fmt.Sprintf(
					"aws dynamodb update-time-to-live --table-name %s --time-to-live-specification 'Enabled=true,AttributeName=ttl'",
					opts.Table,
				),
			},
		})
	}

	// 成功
	result := map[string]any{
		"capability": cap,
		"verified":   true,
		"table":      opts.Table,
		"region":     resolvedRegion,
	}
	payload, err := output.Success(result)
	if err != nil {
		return fmt.Errorf("store init: encode success: %w", err)
	}
	return output.Write(out, payload)
}

// resolveClient は opts.Client が設定されていればそれを使い、
// nil の場合は AWS SDK で実際のクライアントを構築する。
// 解決済みリージョン文字列も返す。
func resolveClient(ctx context.Context, opts InitOptions) (DynamoDBInitAPI, string, error) {
	if opts.Client != nil {
		return opts.Client, opts.Region, nil
	}
	var optFns []func(*awsconfig.LoadOptions) error
	if opts.Region != "" {
		optFns = append(optFns, awsconfig.WithRegion(opts.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, "", err
	}
	return awsdynamodb.NewFromConfig(awsCfg), awsCfg.Region, nil
}

// normalizeCapability は空文字を "full" に正規化し、小文字に変換する。
func normalizeCapability(cap string) string {
	if cap == "" {
		return "full"
	}
	return strings.ToLower(cap)
}

// checkRequirements は capability に応じた DynamoDB スキーマ要件を検証する。
func checkRequirements(cap string, table *types.TableDescription, ttl *types.TimeToLiveDescription) missingReqs {
	var m missingReqs
	if table == nil {
		return m
	}

	attrs := make(map[string]bool, len(table.AttributeDefinitions))
	for _, a := range table.AttributeDefinitions {
		attrs[aws.ToString(a.AttributeName)] = true
	}
	indexes := make(map[string]bool, len(table.GlobalSecondaryIndexes))
	for _, gsi := range table.GlobalSecondaryIndexes {
		indexes[aws.ToString(gsi.IndexName)] = true
	}
	ttlEnabled := ttl != nil && ttl.TimeToLiveStatus == types.TimeToLiveStatusEnabled

	requireAttr := func(name string) {
		if !attrs[name] {
			m.Attributes = append(m.Attributes, name)
		}
	}
	requireIndex := func(name string) {
		if !indexes[name] {
			m.Indexes = append(m.Indexes, name)
		}
	}
	requireTTL := func() {
		if !ttlEnabled {
			m.TTLRequired = true
		}
	}

	// pk は全 capability 共通
	requireAttr(storedynamo.AttrPK)

	switch cap {
	case "token":
		requireAttr(storedynamo.AttrGSI1PK)
		requireAttr(storedynamo.AttrGSI1SK)
		requireIndex(storedynamo.IndexGSI1)
	case "cache":
		requireAttr(storedynamo.AttrGSI2PK)
		requireAttr(storedynamo.AttrGSI2SK)
		requireAttr(storedynamo.AttrTTL)
		requireIndex(storedynamo.IndexGSI2)
		requireTTL()
	case "signingkey":
		// pk のみ（共通 requireAttr で済み）
	case "idproxy":
		requireAttr(storedynamo.AttrTTL)
		requireTTL()
	default: // "full" およびその他
		requireAttr(storedynamo.AttrGSI1PK)
		requireAttr(storedynamo.AttrGSI1SK)
		requireAttr(storedynamo.AttrGSI2PK)
		requireAttr(storedynamo.AttrGSI2SK)
		requireAttr(storedynamo.AttrTTL)
		requireIndex(storedynamo.IndexGSI1)
		requireIndex(storedynamo.IndexGSI2)
		requireTTL()
	}
	return m
}

// buildDDLHint は不足要件に対する aws CLI コマンドヒントを返す。
func buildDDLHint(table, cap string, m missingReqs) string {
	if len(m.Indexes) == 0 && len(m.Attributes) == 0 {
		return ""
	}
	// 属性定義部
	var parts []string
	parts = append(parts, fmt.Sprintf("# capability=%s に必要な属性/GSI が不足しています", cap))
	if len(m.Attributes) > 0 {
		parts = append(parts, fmt.Sprintf("# 不足属性: %s", strings.Join(m.Attributes, ", ")))
	}
	if len(m.Indexes) > 0 {
		parts = append(parts, fmt.Sprintf("# 不足 GSI: %s", strings.Join(m.Indexes, ", ")))
		parts = append(parts, fmt.Sprintf(
			"aws dynamodb update-table --table-name %s --billing-mode PAY_PER_REQUEST"+
				" --attribute-definitions ... --global-secondary-index-updates ...",
			table,
		))
	}
	return strings.Join(parts, "\n")
}

// writeFailure は output.Failure を out に書き、sentinel error を返す。
// Cobra の SilenceErrors=true により、この error は stderr には出力されない。
// ExecuteWith は MapToOutputError を呼ぶが、store init は直接書き込み済みのため
// 二重書きを防ぐため、専用の errStoreInitHandled sentinel を返す。
func writeFailure(out io.Writer, oe *output.Error) error {
	payload, _ := output.Failure(oe)
	_ = output.Write(out, payload)
	return errStoreInitHandled{oe: oe}
}

// errStoreInitHandled は store init 専用の handled エラー。
// output.Failure を out に書き込み済みであることを示す。
// ExecuteWith の MapToOutputError 経路でこの型を検出した場合、
// 二重書きを防ぐため nil を返す（InitHandled() メソッドで識別）。
//
// また errorWithDetails interface を実装し、MapToOutputError で
// details を直接取り出せるようにする（循環 import 防止のため interface 経由）。
type errStoreInitHandled struct {
	oe *output.Error
}

func (e errStoreInitHandled) Error() string {
	if e.oe != nil {
		return e.oe.Code + ": " + e.oe.Message
	}
	return "store init: handled error"
}

// DetailMap は errorWithDetails interface を満たす。
// cli.MapToOutputError が errors.As で取り出して details を組み立てる。
func (e errStoreInitHandled) DetailMap() map[string]any {
	if e.oe != nil {
		return e.oe.Details
	}
	return nil
}

// IsHandled は store init が output を書き込み済みであることを示す。
// cli.MapToOutputError がこれを確認して二重書きを防ぐ。
func (e errStoreInitHandled) IsHandled() bool { return true }
