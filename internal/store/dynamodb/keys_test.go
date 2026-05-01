package dynamodb

import (
	"strings"
	"testing"
)

// TestKeysNoCollision は kintone 側 PK プレフィックスが idproxy 側
// プレフィックス (session: / authcode: / accesstoken: / refreshtoken: /
// client: / familyrevoked:) と prefix 衝突しないことを検証する。
//
// 同一テーブルに idproxy v0.4.2 を共存させる前提のため、片方の prefix が
// もう片方の prefix の prefix でないことが必須。
func TestKeysNoCollision(t *testing.T) {
	kintonePrefixes := []string{
		PKPrefixKintoneTokens,
		PKPrefixKintoneCache,
		PKKintoneSigningKeyCurrent,
	}
	idproxyPrefixes := []string{
		"session:",
		"authcode:",
		"accesstoken:",
		"refreshtoken:",
		"client:",
		"familyrevoked:",
	}
	for _, k := range kintonePrefixes {
		for _, ip := range idproxyPrefixes {
			if k == ip {
				t.Fatalf("kintone prefix %q exactly equals idproxy prefix %q", k, ip)
			}
			if strings.HasPrefix(k, ip) {
				t.Fatalf("kintone prefix %q has idproxy prefix %q as prefix (collision)", k, ip)
			}
			if strings.HasPrefix(ip, k) {
				t.Fatalf("idproxy prefix %q has kintone prefix %q as prefix (collision)", ip, k)
			}
		}
	}
}
