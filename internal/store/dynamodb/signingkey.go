package dynamodb

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoDBSigningKeyStore は SigningKeyStore の DynamoDB 実装。
//
// 単一固定 PK (kintone:signingkey:current) に PEM (PKCS8) を保存する。
// 並行する LoadOrCreate に対しては ConditionExpression
// "attribute_not_exists(pk)" で competitive write を 1 回のみ通し、
// ConditionalCheckFailedException が出たら GetItem で勝者の鍵を返す。
type DynamoDBSigningKeyStore struct {
	client dynamoDBAPI
	table  string
}

func newSigningKeyStore(client dynamoDBAPI, table string) *DynamoDBSigningKeyStore {
	return &DynamoDBSigningKeyStore{client: client, table: table}
}

// LoadOrCreate は永続鍵をロードする。未保存なら新規生成して保存する。
func (s *DynamoDBSigningKeyStore) LoadOrCreate(ctx context.Context) (*ecdsa.PrivateKey, error) {
	if k, ok, err := s.load(ctx); err != nil {
		return nil, err
	} else if ok {
		return k, nil
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("store/dynamodb: generate ec key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("store/dynamodb: marshal pkcs8: %w", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	_, err = s.client.PutItem(ctx, &awsdynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item: map[string]types.AttributeValue{
			AttrPK:  &types.AttributeValueMemberS{Value: PKKintoneSigningKeyCurrent},
			AttrPEM: &types.AttributeValueMemberS{Value: pemStr},
		},
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			// 競合 — 既存を再 GET して勝者を返す。
			if k, ok, err := s.load(ctx); err == nil && ok {
				return k, nil
			}
		}
		return nil, fmt.Errorf("store/dynamodb: put signing key: %w", err)
	}
	return priv, nil
}

// load は永続鍵をロードする。(key, found, err) を返す。
func (s *DynamoDBSigningKeyStore) load(ctx context.Context) (*ecdsa.PrivateKey, bool, error) {
	out, err := s.client.GetItem(ctx, &awsdynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key:       map[string]types.AttributeValue{AttrPK: &types.AttributeValueMemberS{Value: PKKintoneSigningKeyCurrent}},
	})
	if err != nil {
		return nil, false, fmt.Errorf("store/dynamodb: get signing key: %w", err)
	}
	if len(out.Item) == 0 {
		return nil, false, nil
	}
	av, ok := out.Item[AttrPEM].(*types.AttributeValueMemberS)
	if !ok {
		return nil, false, fmt.Errorf("store/dynamodb: pem attr missing")
	}
	block, _ := pem.Decode([]byte(av.Value))
	if block == nil {
		return nil, false, fmt.Errorf("store/dynamodb: pem decode failed")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if ec, ok := k.(*ecdsa.PrivateKey); ok {
			return ec, true, nil
		}
		return nil, false, fmt.Errorf("store/dynamodb: signing key not ECDSA")
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, true, nil
	}
	return nil, false, fmt.Errorf("store/dynamodb: parse signing key failed")
}

// Close は no-op (client は Container が単独所有)。
func (s *DynamoDBSigningKeyStore) Close() error { return nil }
