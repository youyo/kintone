package dynamodb

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/youyo/kintone/internal/store"
)

// Open は store.RegisterOpener から呼ばれる factory entry。
//
// KINTONE_STORE_DYNAMODB_TABLE は必須。KINTONE_STORE_DYNAMODB_REGION は省略可
// (省略時は AWS SDK 既定の解決ロジック: 環境変数 / SSO profile / EC2 IMDS)。
func Open(cfg *store.Config) (store.Container, error) {
	if cfg == nil {
		return nil, fmt.Errorf("store/dynamodb: nil config")
	}
	if cfg.DynamoDBTable == "" {
		return nil, fmt.Errorf("store/dynamodb: KINTONE_STORE_DYNAMODB_TABLE is required")
	}
	ctx := context.Background()
	var optFns []func(*config.LoadOptions) error
	if cfg.DynamoDBRegion != "" {
		optFns = append(optFns, config.WithRegion(cfg.DynamoDBRegion))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("%w: load aws config: %v", store.ErrConnectionFailed, err)
	}
	client := awsdynamodb.NewFromConfig(awsCfg)
	return newContainer(client, cfg.DynamoDBTable)
}

// describeMandatory は Container 構築時に最低限の前提を検証する。
//
// 検証範囲:
//   - テーブル存在 (ResourceNotFoundException は ErrTableNotFound に変換)
//   - PK 属性 (pk) の存在
//   - TimeToLive が ENABLED
//
// GSI1 / GSI2 の存在は ListByDomain / DeleteByPrefix の最初の呼び出しで
// 自然に判明するため lazy validation とし、ここでは強制チェックしない
// (運用初期の段階的有効化を許容)。
func describeMandatory(ctx context.Context, client dynamoDBAPI, table string) error {
	out, err := client.DescribeTable(ctx, &awsdynamodb.DescribeTableInput{TableName: aws.String(table)})
	if err != nil {
		var nfe *types.ResourceNotFoundException
		if errors.As(err, &nfe) {
			return fmt.Errorf("%w: %s", store.ErrTableNotFound, table)
		}
		return fmt.Errorf("%w: describe %s: %v", store.ErrConnectionFailed, table, err)
	}
	if out.Table == nil {
		return fmt.Errorf("%w: describe %s: empty table description", store.ErrConnectionFailed, table)
	}
	hasPK := false
	for _, attr := range out.Table.AttributeDefinitions {
		if aws.ToString(attr.AttributeName) == AttrPK {
			hasPK = true
			break
		}
	}
	if !hasPK {
		return fmt.Errorf("%w: %s missing %q attribute", store.ErrGSIMissing, table, AttrPK)
	}

	ttlOut, err := client.DescribeTimeToLive(ctx, &awsdynamodb.DescribeTimeToLiveInput{TableName: aws.String(table)})
	if err != nil {
		return fmt.Errorf("%w: describe ttl on %s: %v", store.ErrConnectionFailed, table, err)
	}
	if ttlOut.TimeToLiveDescription == nil ||
		ttlOut.TimeToLiveDescription.TimeToLiveStatus != types.TimeToLiveStatusEnabled {
		return fmt.Errorf("%w: %s ttl not enabled", store.ErrTTLDisabled, table)
	}
	return nil
}
