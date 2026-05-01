// Package dynamodb は kintone CLI/MCP の DynamoDB backend を提供する。
//
// 単一テーブル設計で kintone 側 (tokens / cache / signing key) と
// idproxy 側 (sessions / authcodes / accesstokens / clients / refresh tokens / family revocation)
// を共存させる。属性名は upstream (idproxy v0.4.2) と整合する lowercase
// (pk / data / ttl / used)。kintone 側はさらに gsi1pk / gsi1sk / gsi2pk / gsi2sk を使い、
// GSI1 (token list-by-domain) と GSI2 (cache list-by-prefix) を追加する。
// idproxy は GSI を見ないため安全に共存可能。
package dynamodb

// kintone 側 PK プレフィックス。idproxy 側プレフィックス (session: / authcode: /
// accesstoken: / refreshtoken: / client: / familyrevoked:) と衝突しないよう
// すべて "kintone:" 名前空間に閉じ込める。
const (
	// PKPrefixKintoneTokens は Token 永続化用 PK プレフィックス。
	// 形式: kintone:tokens:<domain>:<principalID>:<authType>
	PKPrefixKintoneTokens = "kintone:tokens:"
	// PKPrefixKintoneCache は CacheStore 用 PK プレフィックス。
	// 形式: kintone:cache:<v1:scope:id>
	PKPrefixKintoneCache = "kintone:cache:"
	// PKKintoneSigningKeyCurrent は現行署名鍵の単一固定 PK。
	PKKintoneSigningKeyCurrent = "kintone:signingkey:current"
)

// GSI 名。テーブル定義側で同名の Global Secondary Index が前提。
const (
	IndexGSI1 = "gsi1"
	IndexGSI2 = "gsi2"
)

// 属性名（DynamoDB 上では lowercase）。
const (
	AttrPK     = "pk"
	AttrData   = "data"
	AttrTTL    = "ttl"
	AttrPEM    = "pem"
	AttrGSI1PK = "gsi1pk"
	AttrGSI1SK = "gsi1sk"
	AttrGSI2PK = "gsi2pk"
	AttrGSI2SK = "gsi2sk"

	// AttrExpiresAt はナノ秒精度の有効期限 (int64 文字列、N 属性)。
	// kintone 側のロジカル TTL チェックに使う。0 は無期限。
	AttrExpiresAt = "expires_at"
	// AttrUpdatedAt はナノ秒精度の最終更新時刻 (int64 文字列、N 属性)。
	AttrUpdatedAt = "updated_at"
)

// GSI2PKCache は CacheStore 全エントリ走査用の GSI2 共通 PK。
// GSI2SK には key (PKPrefixKintoneCache を除いた部分) を入れることで
// prefix 走査と件数集計を可能にする。
const GSI2PKCache = "kintone:cache"

// tokenPK は TokenStore のキーをエンコードする。
func tokenPK(domain, principalID string, authType string) string {
	return PKPrefixKintoneTokens + domain + ":" + principalID + ":" + authType
}

// cachePK は CacheStore のキーをエンコードする。
func cachePK(key string) string { return PKPrefixKintoneCache + key }
