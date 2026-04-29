package kintoneapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// ListAppsRequest は GET /k/v1/apps.json の入力。
//
// kintone REST はクエリパラメータで配列を `ids[0]=1&ids[1]=2` 形式（インデックス付き）で受け取る。
// 全フィールドは任意。Limit/Offset は 0 のときクエリに含めない（kintone のデフォルト挙動に委ねる）。
type ListAppsRequest struct {
	IDs      []int64
	Codes    []string
	Name     string  // 部分一致
	SpaceIDs []int64
	Limit    int64 // 1-100、0 で省略
	Offset   int64 // 0 で省略
}

// AppListEntry は ListAppsResponse.Apps の要素。
//
// `GetAppResponse` と同形だが、kintone REST 上は同じ shape のため独立した型としている。
type AppListEntry struct {
	AppID       string         `json:"appId"`
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	SpaceID     string         `json:"spaceId"`
	ThreadID    string         `json:"threadId"`
	CreatedAt   string         `json:"createdAt"`
	Creator     map[string]any `json:"creator"`
	ModifiedAt  string         `json:"modifiedAt"`
	Modifier    map[string]any `json:"modifier"`
}

// ListAppsResponse は GET /k/v1/apps.json のレスポンス。
type ListAppsResponse struct {
	Apps []AppListEntry `json:"apps"`
}

// ListApps は kintone のアプリ一覧を取得する。
//
// 入力フィールドが空配列/空文字/0 の場合は当該クエリパラメータを送らない。
// limit/offset の妥当性チェックは kintone REST 側に委ねる（API バリデーションエラーは APIError として透過）。
func (c *Client) ListApps(ctx context.Context, req ListAppsRequest) (*ListAppsResponse, error) {
	q := url.Values{}
	for i, id := range req.IDs {
		q.Set("ids["+strconv.Itoa(i)+"]", strconv.FormatInt(id, 10))
	}
	for i, code := range req.Codes {
		q.Set("codes["+strconv.Itoa(i)+"]", code)
	}
	if req.Name != "" {
		q.Set("name", req.Name)
	}
	for i, sid := range req.SpaceIDs {
		q.Set("spaceIds["+strconv.Itoa(i)+"]", strconv.FormatInt(sid, 10))
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.FormatInt(req.Limit, 10))
	}
	if req.Offset > 0 {
		q.Set("offset", strconv.FormatInt(req.Offset, 10))
	}
	var resp ListAppsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/k/v1/apps.json", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
