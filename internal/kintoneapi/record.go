package kintoneapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// GetRecordRequest は GET /k/v1/record.json の入力。
type GetRecordRequest struct {
	App int64 // 必須
	ID  int64 // 必須
}

// GetRecordResponse は GET /k/v1/record.json のレスポンス。
type GetRecordResponse struct {
	Record map[string]any `json:"record"`
}

// GetRecord は kintone の単一レコードを取得する。
func (c *Client) GetRecord(ctx context.Context, req GetRecordRequest) (*GetRecordResponse, error) {
	if req.App <= 0 {
		return nil, errors.New("kintoneapi: GetRecord: App is required")
	}
	if req.ID <= 0 {
		return nil, errors.New("kintoneapi: GetRecord: ID is required")
	}
	q := url.Values{}
	q.Set("app", strconv.FormatInt(req.App, 10))
	q.Set("id", strconv.FormatInt(req.ID, 10))
	var resp GetRecordResponse
	if err := c.doJSON(ctx, http.MethodGet, "/k/v1/record.json", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
