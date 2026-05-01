package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	idproxy "github.com/youyo/idproxy"
	idpredis "github.com/youyo/idproxy/store/redis"
)

// idproxyKeyPrefix は idproxy 専用のキー prefix。kintone:* と衝突させない。
const idproxyKeyPrefix = "idproxy:"

// kintoneRedisIDProxyAdapter は idproxy の Redis Store を委譲し、Close を no-op に
// 上書きする adapter。
//
// idproxy v0.4.2 の `store/redis.Store.Close()` は内部で client.Close() を呼ぶ仕様
// だが、kintone backend では client を Container が単独所有して両 prefix を共有する
// ため、idproxy 側で client を閉じられると kintone 側のアクセスが破綻する。
// 本 adapter は Close() を no-op にすることでライフサイクル責任を Container に集中させる。
type kintoneRedisIDProxyAdapter struct {
	inner idproxy.Store
}

// newIDProxyStore は idproxy が要求する Store interface を満たす Redis 実装を返す。
//
// idproxy v0.4.2 の `store/redis.NewWithClient(client, "idproxy:")` を thin wrap し、
// Close は client を閉じない adapter で覆う。
func newIDProxyStore(client goredis.UniversalClient) idproxy.Store {
	return &kintoneRedisIDProxyAdapter{
		inner: idpredis.NewWithClient(client, idproxyKeyPrefix),
	}
}

// --- Session ---

func (a *kintoneRedisIDProxyAdapter) SetSession(ctx context.Context, id string, sess *idproxy.Session, ttl time.Duration) error {
	return a.inner.SetSession(ctx, id, sess, ttl)
}

func (a *kintoneRedisIDProxyAdapter) GetSession(ctx context.Context, id string) (*idproxy.Session, error) {
	return a.inner.GetSession(ctx, id)
}

func (a *kintoneRedisIDProxyAdapter) DeleteSession(ctx context.Context, id string) error {
	return a.inner.DeleteSession(ctx, id)
}

// --- AuthCode ---

func (a *kintoneRedisIDProxyAdapter) SetAuthCode(ctx context.Context, code string, data *idproxy.AuthCodeData, ttl time.Duration) error {
	return a.inner.SetAuthCode(ctx, code, data, ttl)
}

func (a *kintoneRedisIDProxyAdapter) GetAuthCode(ctx context.Context, code string) (*idproxy.AuthCodeData, error) {
	return a.inner.GetAuthCode(ctx, code)
}

func (a *kintoneRedisIDProxyAdapter) DeleteAuthCode(ctx context.Context, code string) error {
	return a.inner.DeleteAuthCode(ctx, code)
}

// --- AccessToken ---

func (a *kintoneRedisIDProxyAdapter) SetAccessToken(ctx context.Context, jti string, data *idproxy.AccessTokenData, ttl time.Duration) error {
	return a.inner.SetAccessToken(ctx, jti, data, ttl)
}

func (a *kintoneRedisIDProxyAdapter) GetAccessToken(ctx context.Context, jti string) (*idproxy.AccessTokenData, error) {
	return a.inner.GetAccessToken(ctx, jti)
}

func (a *kintoneRedisIDProxyAdapter) DeleteAccessToken(ctx context.Context, jti string) error {
	return a.inner.DeleteAccessToken(ctx, jti)
}

// --- Client ---

func (a *kintoneRedisIDProxyAdapter) SetClient(ctx context.Context, clientID string, data *idproxy.ClientData) error {
	return a.inner.SetClient(ctx, clientID, data)
}

func (a *kintoneRedisIDProxyAdapter) GetClient(ctx context.Context, clientID string) (*idproxy.ClientData, error) {
	return a.inner.GetClient(ctx, clientID)
}

func (a *kintoneRedisIDProxyAdapter) DeleteClient(ctx context.Context, clientID string) error {
	return a.inner.DeleteClient(ctx, clientID)
}

// --- RefreshToken ---

func (a *kintoneRedisIDProxyAdapter) SetRefreshToken(ctx context.Context, id string, data *idproxy.RefreshTokenData, ttl time.Duration) error {
	return a.inner.SetRefreshToken(ctx, id, data, ttl)
}

func (a *kintoneRedisIDProxyAdapter) GetRefreshToken(ctx context.Context, id string) (*idproxy.RefreshTokenData, error) {
	return a.inner.GetRefreshToken(ctx, id)
}

func (a *kintoneRedisIDProxyAdapter) ConsumeRefreshToken(ctx context.Context, id string) (*idproxy.RefreshTokenData, error) {
	return a.inner.ConsumeRefreshToken(ctx, id)
}

func (a *kintoneRedisIDProxyAdapter) SetFamilyRevocation(ctx context.Context, familyID string, ttl time.Duration) error {
	return a.inner.SetFamilyRevocation(ctx, familyID, ttl)
}

func (a *kintoneRedisIDProxyAdapter) IsFamilyRevoked(ctx context.Context, familyID string) (bool, error) {
	return a.inner.IsFamilyRevoked(ctx, familyID)
}

// --- Cleanup / Close ---

func (a *kintoneRedisIDProxyAdapter) Cleanup(ctx context.Context) error {
	return a.inner.Cleanup(ctx)
}

// Close は no-op。idproxy の Store.Close は内部 client を閉じるが、kintone backend では
// client を Container が単独所有しているため、閉じる責任を Container に集中させる。
func (a *kintoneRedisIDProxyAdapter) Close() error { return nil }

// ensure kintoneRedisIDProxyAdapter implements idproxy.Store at compile time
var _ idproxy.Store = (*kintoneRedisIDProxyAdapter)(nil)
