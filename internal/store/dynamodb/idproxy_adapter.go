package dynamodb

import (
	"context"
	"time"

	idproxy "github.com/youyo/idproxy"
	idstore "github.com/youyo/idproxy/store"
)

// kintoneDynamoDBIDProxyAdapter は idproxy の DynamoDB Store を委譲し、
// Close を no-op に上書きする adapter。
//
// idproxy v0.4.2 の DynamoDB Store は同一テーブル + 専用 PK プレフィックス
// (session: / authcode: / accesstoken: / refreshtoken: / client: / familyrevoked:) で
// 動作する。kintone 側は kintone:* 名前空間に閉じ込めているため衝突しない。
//
// idproxy 側 Store は client を所有しないため Close は本来空でよいが、
// Container にライフサイクル責任を集中させるために明示的に no-op で wrap する。
type kintoneDynamoDBIDProxyAdapter struct {
	inner idproxy.Store
}

// newIDProxyStore は idproxy が要求する Store interface を満たす DynamoDB 実装を返す。
//
// idproxy v0.4.2 の `store.NewDynamoDBStoreWithClient(client, table)` を thin wrap し、
// Close は client を閉じない adapter で覆う。
func newIDProxyStore(client dynamoDBAPI, table string) idproxy.Store {
	// idproxy v0.4.2 の DynamoDBClient interface は GetItem/PutItem/DeleteItem の
	// 3 メソッドだけを要求するため、本パッケージの dynamoDBAPI (より広い surface) を
	// そのまま渡せる。
	return &kintoneDynamoDBIDProxyAdapter{
		inner: idstore.NewDynamoDBStoreWithClient(client, table),
	}
}

// --- Session ---

func (a *kintoneDynamoDBIDProxyAdapter) SetSession(ctx context.Context, id string, sess *idproxy.Session, ttl time.Duration) error {
	return a.inner.SetSession(ctx, id, sess, ttl)
}

func (a *kintoneDynamoDBIDProxyAdapter) GetSession(ctx context.Context, id string) (*idproxy.Session, error) {
	return a.inner.GetSession(ctx, id)
}

func (a *kintoneDynamoDBIDProxyAdapter) DeleteSession(ctx context.Context, id string) error {
	return a.inner.DeleteSession(ctx, id)
}

// --- AuthCode ---

func (a *kintoneDynamoDBIDProxyAdapter) SetAuthCode(ctx context.Context, code string, data *idproxy.AuthCodeData, ttl time.Duration) error {
	return a.inner.SetAuthCode(ctx, code, data, ttl)
}

func (a *kintoneDynamoDBIDProxyAdapter) GetAuthCode(ctx context.Context, code string) (*idproxy.AuthCodeData, error) {
	return a.inner.GetAuthCode(ctx, code)
}

func (a *kintoneDynamoDBIDProxyAdapter) DeleteAuthCode(ctx context.Context, code string) error {
	return a.inner.DeleteAuthCode(ctx, code)
}

// --- AccessToken ---

func (a *kintoneDynamoDBIDProxyAdapter) SetAccessToken(ctx context.Context, jti string, data *idproxy.AccessTokenData, ttl time.Duration) error {
	return a.inner.SetAccessToken(ctx, jti, data, ttl)
}

func (a *kintoneDynamoDBIDProxyAdapter) GetAccessToken(ctx context.Context, jti string) (*idproxy.AccessTokenData, error) {
	return a.inner.GetAccessToken(ctx, jti)
}

func (a *kintoneDynamoDBIDProxyAdapter) DeleteAccessToken(ctx context.Context, jti string) error {
	return a.inner.DeleteAccessToken(ctx, jti)
}

// --- Client ---

func (a *kintoneDynamoDBIDProxyAdapter) SetClient(ctx context.Context, clientID string, data *idproxy.ClientData) error {
	return a.inner.SetClient(ctx, clientID, data)
}

func (a *kintoneDynamoDBIDProxyAdapter) GetClient(ctx context.Context, clientID string) (*idproxy.ClientData, error) {
	return a.inner.GetClient(ctx, clientID)
}

func (a *kintoneDynamoDBIDProxyAdapter) DeleteClient(ctx context.Context, clientID string) error {
	return a.inner.DeleteClient(ctx, clientID)
}

// --- RefreshToken ---

func (a *kintoneDynamoDBIDProxyAdapter) SetRefreshToken(ctx context.Context, id string, data *idproxy.RefreshTokenData, ttl time.Duration) error {
	return a.inner.SetRefreshToken(ctx, id, data, ttl)
}

func (a *kintoneDynamoDBIDProxyAdapter) GetRefreshToken(ctx context.Context, id string) (*idproxy.RefreshTokenData, error) {
	return a.inner.GetRefreshToken(ctx, id)
}

func (a *kintoneDynamoDBIDProxyAdapter) ConsumeRefreshToken(ctx context.Context, id string) (*idproxy.RefreshTokenData, error) {
	return a.inner.ConsumeRefreshToken(ctx, id)
}

func (a *kintoneDynamoDBIDProxyAdapter) SetFamilyRevocation(ctx context.Context, familyID string, ttl time.Duration) error {
	return a.inner.SetFamilyRevocation(ctx, familyID, ttl)
}

func (a *kintoneDynamoDBIDProxyAdapter) IsFamilyRevoked(ctx context.Context, familyID string) (bool, error) {
	return a.inner.IsFamilyRevoked(ctx, familyID)
}

// --- Cleanup / Close ---

func (a *kintoneDynamoDBIDProxyAdapter) Cleanup(ctx context.Context) error {
	return a.inner.Cleanup(ctx)
}

// Close は no-op。client は Container が単独所有しているため、
// 閉じる責任を Container に集中させる。
func (a *kintoneDynamoDBIDProxyAdapter) Close() error { return nil }

// ensure kintoneDynamoDBIDProxyAdapter implements idproxy.Store at compile time
var _ idproxy.Store = (*kintoneDynamoDBIDProxyAdapter)(nil)
