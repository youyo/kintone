package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/store"
)

// CacheProvider は per-request lazy に store.CacheStore を解決する関数。
//
// MCP HTTP の長寿命プロセスでは、起動時に cache 接続を 1 度だけ確立する設計だと
// 一時的な Redis/DynamoDB 障害が永続化する。CacheProvider 経由で各 API コールごとに
// 現在の cache を取得することで、Container 側の lazy init / 自動回復ロジックが効く。
//
// 戻り値が (nil, err) または (nil, nil) のとき、CachingAPI は fail-open で
// upstream を直接呼ぶ（既存の fail-open ポリシーと一貫）。
type CacheProvider func() (store.CacheStore, error)

// CachingAPI は service/api.API を decorator として実装する。
//
// apps / fields / list_apps の read 系メソッドを 1 年 TTL で cache に保存する。
// records 系（read/write）・write 系は upstream に素通し。
//
// キャッシュキーは必ず "v1:" で始まる（バージョンプレフィックス）。
// domain を含めることで複数 profile 切り替え時のクロス汚染を防ぐ。
type CachingAPI struct {
	upstream      API
	cacheProvider CacheProvider
	domain        string
}

// NewCachingAPI は decorator を構築する。
//
// cacheProvider == nil のとき、または provider が常に (nil, nil) を返すときは
// upstream をそのまま返す（CA-12 互換 / fail-open）。
//
// per-request lazy resolution: 各 method 内で cacheProvider() を呼び、
// 解決失敗時は upstream にフォールバックする。
func NewCachingAPI(upstream API, cacheProvider CacheProvider, domain string) API {
	if cacheProvider == nil {
		return upstream
	}
	return &CachingAPI{upstream: upstream, cacheProvider: cacheProvider, domain: domain}
}

// resolveCache は cacheProvider を呼び、現在のリクエストで使う CacheStore を返す。
// fail-open: nil を返したときは caller が upstream を直接呼ぶ。
func (c *CachingAPI) resolveCache() store.CacheStore {
	if c.cacheProvider == nil {
		return nil
	}
	cs, err := c.cacheProvider()
	if err != nil {
		// fail-open: cache 解決失敗はログを残さず upstream にフォールバック
		// （Container 側で既に warn ログが出ている前提）
		return nil
	}
	return cs
}

// GetApp は apps を 1 年 TTL でキャッシュする。
// キャッシュミス or IO エラー時は upstream に フォールバック（fail-open）。
func (c *CachingAPI) GetApp(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
	cs := c.resolveCache()
	if cs == nil {
		return c.upstream.GetApp(ctx, req)
	}
	key := fmt.Sprintf("%s%s:%d", store.KeyPrefixApps, c.domain, req.ID)

	if resp, ok := c.getCached(ctx, cs, key, new(kintoneapi.GetAppResponse)); ok {
		return resp.(*kintoneapi.GetAppResponse), nil
	}

	resp, err := c.upstream.GetApp(ctx, req)
	if err != nil {
		return nil, err
	}
	c.putCached(ctx, cs, key, resp, store.TTLApps)
	return resp, nil
}

// GetFormFields は fields を 1 年 TTL でキャッシュする。
func (c *CachingAPI) GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
	cs := c.resolveCache()
	if cs == nil {
		return c.upstream.GetFormFields(ctx, req)
	}
	lang := req.Lang
	if lang == "" {
		lang = "default"
	}
	key := fmt.Sprintf("%s%s:%d:%s", store.KeyPrefixFields, c.domain, req.App, lang)

	if resp, ok := c.getCached(ctx, cs, key, new(kintoneapi.GetFormFieldsResponse)); ok {
		return resp.(*kintoneapi.GetFormFieldsResponse), nil
	}

	resp, err := c.upstream.GetFormFields(ctx, req)
	if err != nil {
		return nil, err
	}
	c.putCached(ctx, cs, key, resp, store.TTLFields)
	return resp, nil
}

// ListApps は list_apps を 1 年 TTL でキャッシュする。
// リクエストパラメータを正規化してキャッシュキーに SHA256 ハッシュを使う。
func (c *CachingAPI) ListApps(ctx context.Context, req kintoneapi.ListAppsRequest) (*kintoneapi.ListAppsResponse, error) {
	cs := c.resolveCache()
	if cs == nil {
		return c.upstream.ListApps(ctx, req)
	}
	key := c.listAppsCacheKey(req)

	if resp, ok := c.getCached(ctx, cs, key, new(kintoneapi.ListAppsResponse)); ok {
		return resp.(*kintoneapi.ListAppsResponse), nil
	}

	resp, err := c.upstream.ListApps(ctx, req)
	if err != nil {
		return nil, err
	}
	c.putCached(ctx, cs, key, resp, store.TTLListApps)
	return resp, nil
}

// listAppsCacheKey は ListAppsRequest を正規化してキャッシュキーを生成する。
//
// 正規化ルール:
//   - slice は nil と [] を同一視（空スライスに統一）
//   - slice はソートして順序依存をなくす
//   - JSON シリアライズ → SHA256 → 先頭 16 hex 文字
func (c *CachingAPI) listAppsCacheKey(req kintoneapi.ListAppsRequest) string {
	type normalized struct {
		IDs      []int64  `json:"ids"`
		Codes    []string `json:"codes"`
		Name     string   `json:"name"`
		SpaceIDs []int64  `json:"space_ids"`
		Limit    int64    `json:"limit"`
		Offset   int64    `json:"offset"`
	}

	// nil と 空スライスを同一視
	ids := req.IDs
	if ids == nil {
		ids = []int64{}
	}
	codes := req.Codes
	if codes == nil {
		codes = []string{}
	}
	spaceIDs := req.SpaceIDs
	if spaceIDs == nil {
		spaceIDs = []int64{}
	}

	// ソートで順序独立
	sortedIDs := make([]int64, len(ids))
	copy(sortedIDs, ids)
	sort.Slice(sortedIDs, func(i, j int) bool { return sortedIDs[i] < sortedIDs[j] })

	sortedCodes := make([]string, len(codes))
	copy(sortedCodes, codes)
	sort.Strings(sortedCodes)

	sortedSpaceIDs := make([]int64, len(spaceIDs))
	copy(sortedSpaceIDs, spaceIDs)
	sort.Slice(sortedSpaceIDs, func(i, j int) bool { return sortedSpaceIDs[i] < sortedSpaceIDs[j] })

	n := normalized{
		IDs:      sortedIDs,
		Codes:    sortedCodes,
		Name:     req.Name,
		SpaceIDs: sortedSpaceIDs,
		Limit:    req.Limit,
		Offset:   req.Offset,
	}
	b, _ := json.Marshal(n)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%s%s:%x", store.KeyPrefixListApps, c.domain, h[:8])
}

// getCached はキャッシュから値を取得して dst に Unmarshal する。
//
// ヒット時は (dst, true) を返す。ミス or IO エラー時は (nil, false)（fail-open）。
// IO エラーは ErrCacheMiss と同様に upstream にフォールバックする（CA-10）。
func (c *CachingAPI) getCached(ctx context.Context, cs store.CacheStore, key string, dst any) (any, bool) {
	b, err := cs.Get(ctx, key)
	if err != nil {
		// ErrCacheMiss および IO エラーは upstream にフォールバック（fail-open / CA-10）
		return nil, false
	}
	if err := json.Unmarshal(b, dst); err != nil {
		// デシリアライズ失敗も miss 扱い
		return nil, false
	}
	return dst, true
}

// putCached は値を JSON シリアライズして cache に保存する。
//
// エラーは無視する（fail-silent on Put / CA-11）。
func (c *CachingAPI) putCached(ctx context.Context, cs store.CacheStore, key string, v any, ttl time.Duration) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = cs.Put(ctx, key, b, ttl)
}

// GetRecords は upstream に素通し（records は変動するため非キャッシュ）。
func (c *CachingAPI) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	return c.upstream.GetRecords(ctx, req)
}

// GetRecord は upstream に素通し。
func (c *CachingAPI) GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
	return c.upstream.GetRecord(ctx, req)
}

// InsertRecords は upstream に素通し（write 系）。
func (c *CachingAPI) InsertRecords(ctx context.Context, req kintoneapi.InsertRecordsRequest) (*kintoneapi.InsertRecordsResponse, error) {
	return c.upstream.InsertRecords(ctx, req)
}

// UpdateRecord は upstream に素通し（write 系）。
func (c *CachingAPI) UpdateRecord(ctx context.Context, req kintoneapi.UpdateRecordRequest) (*kintoneapi.UpdateRecordResponse, error) {
	return c.upstream.UpdateRecord(ctx, req)
}

// DeleteRecords は upstream に素通し（write 系）。
func (c *CachingAPI) DeleteRecords(ctx context.Context, req kintoneapi.DeleteRecordsRequest) error {
	return c.upstream.DeleteRecords(ctx, req)
}
