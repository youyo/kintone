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

// PKPrefixKintoneOAuthState は OAuth state の PK プレフィックス。
// 形式: kintone:oauthstate:<state>
const PKPrefixKintoneOAuthState = "kintone:oauthstate:"

// 属性名（state ストア専用）。
const (
	AttrPrincipalID = "principal_id"
	AttrVerifier    = "verifier"
	AttrMethod      = "method"
	AttrCreatedAt   = "created_at"
)

// DynamoDBStateStore は store.StateStore の DynamoDB 実装。
//
// 単一テーブル設計に相乗りし、PK=`kintone:oauthstate:<state>` で entry を保存する。
// TTL は DynamoDB Auto-TTL（最大 48h ラグ）+ Go 側 expires_at チェックの二重で実現。
//
// **one-shot Take は DeleteItem with ReturnValues=ALL_OLD** で atomic に取り出し削除する。
// 並行 Take は DynamoDB が DeleteItem を atomic に保証するため、勝者は最大 1 つ
// （残りは Attributes 空 → ErrStateNotFound）。
type DynamoDBStateStore struct {
	client dynamoDBAPI
	table  string
	ttl    time.Duration
}

func newStateStore(client dynamoDBAPI, table string) *DynamoDBStateStore {
	return &DynamoDBStateStore{client: client, table: table, ttl: store.DefaultStateTTL}
}

// statePK は state を PK にエンコードする。
func statePK(state string) string { return PKPrefixKintoneOAuthState + state }

// Put は entry を保存する。CreatedAt が zero のときは now を埋める。
func (s *DynamoDBStateStore) Put(ctx context.Context, entry store.StateEntry) error {
	if entry.State == "" {
		return store.ErrStateNotFound
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	method := entry.Method
	if method == "" {
		method = "S256"
	}
	pk := statePK(entry.State)
	expiresAtNs := entry.CreatedAt.Add(s.ttl).UnixNano()
	item := map[string]types.AttributeValue{
		AttrPK:          &types.AttributeValueMemberS{Value: pk},
		AttrPrincipalID: &types.AttributeValueMemberS{Value: entry.PrincipalID},
		AttrVerifier:    &types.AttributeValueMemberS{Value: entry.Verifier},
		AttrMethod:      &types.AttributeValueMemberS{Value: method},
		AttrCreatedAt:   &types.AttributeValueMemberN{Value: strconv.FormatInt(entry.CreatedAt.UnixNano(), 10)},
		AttrExpiresAt:   &types.AttributeValueMemberN{Value: strconv.FormatInt(expiresAtNs, 10)},
		// DynamoDB Auto-TTL は秒単位 epoch。
		AttrTTL: &types.AttributeValueMemberN{Value: strconv.FormatInt(expiresAtNs/int64(time.Second), 10)},
	}
	_, err := s.client.PutItem(ctx, &awsdynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("store/dynamodb: state put: %w", err)
	}
	return nil
}

// Take は DeleteItem with ReturnValues=ALL_OLD で atomic に取り出し削除する。
//
// 並行 Take は DynamoDB が DeleteItem を atomic に保証するため、winner は最大 1 つ。
// Attributes が空 = 不在として ErrStateNotFound を返す。
// Auto-TTL は eventual delete のため、ロジカル expires_at < now なら ErrStateNotFound。
func (s *DynamoDBStateStore) Take(ctx context.Context, state string) (*store.StateEntry, error) {
	if state == "" {
		return nil, store.ErrStateNotFound
	}
	pk := statePK(state)
	out, err := s.client.DeleteItem(ctx, &awsdynamodb.DeleteItemInput{
		TableName:    aws.String(s.table),
		Key:          map[string]types.AttributeValue{AttrPK: &types.AttributeValueMemberS{Value: pk}},
		ReturnValues: types.ReturnValueAllOld,
	})
	if err != nil {
		return nil, fmt.Errorf("store/dynamodb: state take: %w", err)
	}
	if len(out.Attributes) == 0 {
		return nil, store.ErrStateNotFound
	}
	// 期限切れ判定（Auto-TTL のラグ補正）
	if expN, ok := out.Attributes[AttrExpiresAt].(*types.AttributeValueMemberN); ok {
		if exp, err := strconv.ParseInt(expN.Value, 10, 64); err == nil {
			if exp > 0 && exp < time.Now().UnixNano() {
				return nil, store.ErrStateNotFound
			}
		}
	}
	entry := &store.StateEntry{State: state}
	if v, ok := out.Attributes[AttrPrincipalID].(*types.AttributeValueMemberS); ok {
		entry.PrincipalID = v.Value
	}
	if v, ok := out.Attributes[AttrVerifier].(*types.AttributeValueMemberS); ok {
		entry.Verifier = v.Value
	}
	if v, ok := out.Attributes[AttrMethod].(*types.AttributeValueMemberS); ok {
		entry.Method = v.Value
	}
	if v, ok := out.Attributes[AttrCreatedAt].(*types.AttributeValueMemberN); ok {
		if ns, err := strconv.ParseInt(v.Value, 10, 64); err == nil {
			entry.CreatedAt = time.Unix(0, ns)
		}
	}
	return entry, nil
}

// Close は no-op (client は Container が単独所有)。
func (s *DynamoDBStateStore) Close() error { return nil }

// ensure DynamoDBStateStore implements store.StateStore at compile time
var _ store.StateStore = (*DynamoDBStateStore)(nil)
