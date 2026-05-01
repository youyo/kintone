package dynamodb

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/youyo/kintone/internal/store"
)

// DynamoDBTokenStore は TokenStore の DynamoDB 実装。
//
// レイアウト:
//   - PK: kintone:tokens:<domain>:<principalID>:<authType>
//   - data: Token を JSON encode したバイト列 (B 属性)
//   - gsi1pk = domain / gsi1sk = "<principalID>:<authType>" で GSI1 を構成し、
//     ListByDomain は Scan を使わず Query する。
type DynamoDBTokenStore struct {
	client dynamoDBAPI
	table  string
}

func newTokenStore(client dynamoDBAPI, table string) *DynamoDBTokenStore {
	return &DynamoDBTokenStore{client: client, table: table}
}

// Get はキーに対応する Token を返す。不在は store.ErrNotFound。
func (s *DynamoDBTokenStore) Get(ctx context.Context, domain, principalID string, t store.AuthType) (*store.Token, error) {
	pk := tokenPK(domain, principalID, string(t))
	out, err := s.client.GetItem(ctx, &awsdynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key:       map[string]types.AttributeValue{AttrPK: &types.AttributeValueMemberS{Value: pk}},
	})
	if err != nil {
		return nil, fmt.Errorf("store/dynamodb: get token %s: %w", pk, err)
	}
	if len(out.Item) == 0 {
		return nil, store.ErrNotFound
	}
	return decodeToken(out.Item)
}

// Put は Token を保存する。UpdatedAt が zero のときは現在時刻を自動設定する。
func (s *DynamoDBTokenStore) Put(ctx context.Context, tok store.Token) error {
	if tok.UpdatedAt.IsZero() {
		tok.UpdatedAt = time.Now()
	}
	item, err := encodeToken(tok)
	if err != nil {
		return err
	}
	_, err = s.client.PutItem(ctx, &awsdynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("store/dynamodb: put token: %w", err)
	}
	return nil
}

// Delete は単一 Token を削除する。不在は no-op。
func (s *DynamoDBTokenStore) Delete(ctx context.Context, domain, principalID string, t store.AuthType) error {
	_, err := s.client.DeleteItem(ctx, &awsdynamodb.DeleteItemInput{
		TableName: aws.String(s.table),
		Key:       map[string]types.AttributeValue{AttrPK: &types.AttributeValueMemberS{Value: tokenPK(domain, principalID, string(t))}},
	})
	if err != nil {
		return fmt.Errorf("store/dynamodb: delete token: %w", err)
	}
	return nil
}

// ListByDomain は GSI1 を Query し、AuthType でフィルタした上で principalID 昇順で返す。
// Query は LastEvaluatedKey を辿って全ページ走査する。
func (s *DynamoDBTokenStore) ListByDomain(ctx context.Context, domain string, t store.AuthType) ([]*store.Token, error) {
	var out []*store.Token
	var lastKey map[string]types.AttributeValue
	for {
		q, err := s.client.Query(ctx, &awsdynamodb.QueryInput{
			TableName:              aws.String(s.table),
			IndexName:              aws.String(IndexGSI1),
			KeyConditionExpression: aws.String("gsi1pk = :d"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":d": &types.AttributeValueMemberS{Value: domain},
			},
			ExclusiveStartKey: lastKey,
		})
		if err != nil {
			return nil, fmt.Errorf("store/dynamodb: query gsi1 (domain=%s): %w", domain, err)
		}
		for _, item := range q.Items {
			tok, err := decodeToken(item)
			if err != nil {
				return nil, err
			}
			if tok.AuthType == t {
				out = append(out, tok)
			}
		}
		if len(q.LastEvaluatedKey) == 0 {
			break
		}
		lastKey = q.LastEvaluatedKey
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PrincipalID < out[j].PrincipalID })
	return out, nil
}

// Close は no-op (client は Container が単独所有)。
func (s *DynamoDBTokenStore) Close() error { return nil }

// encodeToken は Token を DynamoDB Item にシリアライズする。
func encodeToken(t store.Token) (map[string]types.AttributeValue, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("store/dynamodb: marshal token: %w", err)
	}
	item := map[string]types.AttributeValue{
		AttrPK:        &types.AttributeValueMemberS{Value: tokenPK(t.Domain, t.PrincipalID, string(t.AuthType))},
		AttrData:      &types.AttributeValueMemberB{Value: data},
		AttrGSI1PK:    &types.AttributeValueMemberS{Value: t.Domain},
		AttrGSI1SK:    &types.AttributeValueMemberS{Value: t.PrincipalID + ":" + string(t.AuthType)},
		AttrUpdatedAt: &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", t.UpdatedAt.UnixNano())},
	}
	return item, nil
}

// decodeToken は DynamoDB Item から Token を復元する。
func decodeToken(item map[string]types.AttributeValue) (*store.Token, error) {
	av, ok := item[AttrData].(*types.AttributeValueMemberB)
	if !ok {
		return nil, fmt.Errorf("store/dynamodb: data attr missing or not bytes")
	}
	var t store.Token
	if err := json.Unmarshal(av.Value, &t); err != nil {
		return nil, fmt.Errorf("store/dynamodb: decode token: %w", err)
	}
	return &t, nil
}
