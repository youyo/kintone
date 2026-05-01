package dynamodb

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/youyo/kintone/internal/store"
)

// DynamoDBCacheStore は CacheStore の DynamoDB 実装。
//
// 期限管理の二段構え:
//  1. ttl 属性 (秒単位 epoch) — DynamoDB 側 Auto-TTL による物理削除 (eventual)
//  2. expires_at 属性 (ナノ秒 epoch) — 本実装によるロジカル期限切れ判定 (即時)
//
// Auto-TTL は反映に最大 48 時間かかるため、本実装では Get 時に expires_at を見て
// 期限切れを ErrCacheMiss として返し、ベストエフォートで delete を走らせる。
type DynamoDBCacheStore struct {
	client dynamoDBAPI
	table  string
}

func newCacheStore(client dynamoDBAPI, table string) *DynamoDBCacheStore {
	return &DynamoDBCacheStore{client: client, table: table}
}

// Get はキーに対応する value を返す。不在 / 期限切れは store.ErrCacheMiss。
func (s *DynamoDBCacheStore) Get(ctx context.Context, key string) ([]byte, error) {
	pk := cachePK(key)
	out, err := s.client.GetItem(ctx, &awsdynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key:       map[string]types.AttributeValue{AttrPK: &types.AttributeValueMemberS{Value: pk}},
	})
	if err != nil {
		return nil, fmt.Errorf("store/dynamodb: cache get %s: %w", pk, err)
	}
	if len(out.Item) == 0 {
		return nil, store.ErrCacheMiss
	}
	if expN, ok := out.Item[AttrExpiresAt].(*types.AttributeValueMemberN); ok {
		if exp, err := strconv.ParseInt(expN.Value, 10, 64); err == nil {
			if exp > 0 && exp < time.Now().UnixNano() {
				// ベストエフォート delete (DynamoDB Auto-TTL を待たない)。
				_, _ = s.client.DeleteItem(ctx, &awsdynamodb.DeleteItemInput{
					TableName: aws.String(s.table),
					Key:       map[string]types.AttributeValue{AttrPK: &types.AttributeValueMemberS{Value: pk}},
				})
				return nil, store.ErrCacheMiss
			}
		}
	}
	if av, ok := out.Item[AttrData].(*types.AttributeValueMemberB); ok {
		return av.Value, nil
	}
	return nil, fmt.Errorf("store/dynamodb: cache data attr missing")
}

// Put は value を ttl 期限で保存する。ttl <= 0 のときは無期限 (expires_at=0、ttl 属性なし)。
func (s *DynamoDBCacheStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	pk := cachePK(key)
	item := map[string]types.AttributeValue{
		AttrPK:     &types.AttributeValueMemberS{Value: pk},
		AttrData:   &types.AttributeValueMemberB{Value: append([]byte(nil), value...)},
		AttrGSI2PK: &types.AttributeValueMemberS{Value: GSI2PKCache},
		AttrGSI2SK: &types.AttributeValueMemberS{Value: key},
	}
	if ttl > 0 {
		exp := time.Now().Add(ttl).UnixNano()
		item[AttrExpiresAt] = &types.AttributeValueMemberN{Value: strconv.FormatInt(exp, 10)}
		// DynamoDB Auto-TTL は秒単位 epoch を要求する。
		item[AttrTTL] = &types.AttributeValueMemberN{Value: strconv.FormatInt(exp/int64(time.Second), 10)}
	} else {
		item[AttrExpiresAt] = &types.AttributeValueMemberN{Value: "0"}
	}
	_, err := s.client.PutItem(ctx, &awsdynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("store/dynamodb: cache put: %w", err)
	}
	return nil
}

// Delete は単一 key を削除する。不在は no-op。
func (s *DynamoDBCacheStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteItem(ctx, &awsdynamodb.DeleteItemInput{
		TableName: aws.String(s.table),
		Key:       map[string]types.AttributeValue{AttrPK: &types.AttributeValueMemberS{Value: cachePK(key)}},
	})
	if err != nil {
		return fmt.Errorf("store/dynamodb: cache delete: %w", err)
	}
	return nil
}

// DeleteByPrefix は GSI2 を prefix Query で走査し、得た PK 一覧を BatchWriteItem で
// 25 件単位に削除する。UnprocessedItems は最大 3 回まで再送する。
func (s *DynamoDBCacheStore) DeleteByPrefix(ctx context.Context, prefix string) (int, error) {
	var lastKey map[string]types.AttributeValue
	deleted := 0
	var pks []string
	for {
		q, err := s.client.Query(ctx, &awsdynamodb.QueryInput{
			TableName:              aws.String(s.table),
			IndexName:              aws.String(IndexGSI2),
			KeyConditionExpression: aws.String("gsi2pk = :p AND begins_with(gsi2sk, :sk)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":p":  &types.AttributeValueMemberS{Value: GSI2PKCache},
				":sk": &types.AttributeValueMemberS{Value: prefix},
			},
			ProjectionExpression: aws.String("pk"),
			ExclusiveStartKey:    lastKey,
		})
		if err != nil {
			return deleted, fmt.Errorf("store/dynamodb: cache query gsi2: %w", err)
		}
		for _, item := range q.Items {
			if pkAv, ok := item[AttrPK].(*types.AttributeValueMemberS); ok {
				pks = append(pks, pkAv.Value)
			}
		}
		if len(q.LastEvaluatedKey) == 0 {
			break
		}
		lastKey = q.LastEvaluatedKey
	}
	for i := 0; i < len(pks); i += 25 {
		end := i + 25
		if end > len(pks) {
			end = len(pks)
		}
		reqs := make([]types.WriteRequest, 0, end-i)
		for _, p := range pks[i:end] {
			reqs = append(reqs, types.WriteRequest{
				DeleteRequest: &types.DeleteRequest{Key: map[string]types.AttributeValue{
					AttrPK: &types.AttributeValueMemberS{Value: p},
				}},
			})
		}
		retries := 0
		currentReqs := reqs
		for {
			out, err := s.client.BatchWriteItem(ctx, &awsdynamodb.BatchWriteItemInput{
				RequestItems: map[string][]types.WriteRequest{s.table: currentReqs},
			})
			if err != nil {
				return deleted, fmt.Errorf("store/dynamodb: batch delete: %w", err)
			}
			unprocessed := out.UnprocessedItems[s.table]
			deleted += len(currentReqs) - len(unprocessed)
			if len(unprocessed) == 0 {
				break
			}
			retries++
			if retries >= 3 {
				return deleted, fmt.Errorf("store/dynamodb: %d unprocessed items remain after retries", len(unprocessed))
			}
			currentReqs = unprocessed
		}
	}
	return deleted, nil
}

// Stats は Query (Select=COUNT) で全 cache エントリ数を集計する。
// expired_count は DynamoDB の Auto-TTL によりロジカル期限と物理削除のラグが
// 大きいため信頼できる値が出せない。よって nil 固定。
func (s *DynamoDBCacheStore) Stats(ctx context.Context) (store.Stats, error) {
	var entries int64
	var lastKey map[string]types.AttributeValue
	for {
		q, err := s.client.Query(ctx, &awsdynamodb.QueryInput{
			TableName:              aws.String(s.table),
			IndexName:              aws.String(IndexGSI2),
			KeyConditionExpression: aws.String("gsi2pk = :p"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":p": &types.AttributeValueMemberS{Value: GSI2PKCache},
			},
			Select:            types.SelectCount,
			ExclusiveStartKey: lastKey,
		})
		if err != nil {
			return store.Stats{}, fmt.Errorf("store/dynamodb: cache stats query: %w", err)
		}
		entries += int64(q.Count)
		if len(q.LastEvaluatedKey) == 0 {
			break
		}
		lastKey = q.LastEvaluatedKey
	}
	return store.Stats{
		Backend:      "dynamodb",
		Location:     "dynamodb://" + s.table,
		Reachable:    true,
		EntryCount:   entries,
		ExpiredCount: nil,
	}, nil
}

// Close は no-op (client は Container が単独所有)。
func (s *DynamoDBCacheStore) Close() error { return nil }
