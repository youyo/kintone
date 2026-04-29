package kintoneapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// GetRecordsRequest は GET /k/v1/records.json の入力。
type GetRecordsRequest struct {
	App        int64    // 必須
	Query      string   // 任意（kintone クエリ言語）
	Fields     []string // 任意。指定するとレスポンスを当該フィールドに絞り込む
	TotalCount bool     // 任意。true で totalCount を返す
}

// GetRecordsResponse は GET /k/v1/records.json のレスポンス。
type GetRecordsResponse struct {
	Records    []map[string]any `json:"records"`
	TotalCount *string          `json:"totalCount"`
}

// GetRecords は kintone のレコード一覧を取得する。
func (c *Client) GetRecords(ctx context.Context, req GetRecordsRequest) (*GetRecordsResponse, error) {
	if req.App <= 0 {
		return nil, errors.New("kintoneapi: GetRecords: App is required")
	}
	q := url.Values{}
	q.Set("app", strconv.FormatInt(req.App, 10))
	if req.Query != "" {
		q.Set("query", req.Query)
	}
	for _, f := range req.Fields {
		q.Add("fields", f)
	}
	if req.TotalCount {
		q.Set("totalCount", "true")
	}
	var resp GetRecordsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/k/v1/records.json", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
