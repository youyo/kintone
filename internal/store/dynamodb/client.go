package dynamodb

import (
	"context"

	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// dynamoDBAPI は本パッケージが利用する DynamoDB クライアントの最小サーフェス。
//
// *awsdynamodb.Client が満たすメソッドのうち、本 backend で実際に呼ぶものだけを
// interface 化することで、テストで fake を注入可能にする (dynamodb-local 不要)。
type dynamoDBAPI interface {
	GetItem(ctx context.Context, p *awsdynamodb.GetItemInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, p *awsdynamodb.PutItemInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.PutItemOutput, error)
	DeleteItem(ctx context.Context, p *awsdynamodb.DeleteItemInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.DeleteItemOutput, error)
	Query(ctx context.Context, p *awsdynamodb.QueryInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.QueryOutput, error)
	BatchWriteItem(ctx context.Context, p *awsdynamodb.BatchWriteItemInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.BatchWriteItemOutput, error)
	DescribeTable(ctx context.Context, p *awsdynamodb.DescribeTableInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.DescribeTableOutput, error)
	DescribeTimeToLive(ctx context.Context, p *awsdynamodb.DescribeTimeToLiveInput, opts ...func(*awsdynamodb.Options)) (*awsdynamodb.DescribeTimeToLiveOutput, error)
}
