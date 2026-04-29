package kintoneapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// GetAppRequest は GET /k/v1/app.json の入力。
type GetAppRequest struct {
	ID int64 // 必須
}

// GetAppResponse は GET /k/v1/app.json のレスポンス。
type GetAppResponse struct {
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

// GetApp は kintone のアプリ情報を取得する。
func (c *Client) GetApp(ctx context.Context, req GetAppRequest) (*GetAppResponse, error) {
	if req.ID <= 0 {
		return nil, errors.New("kintoneapi: GetApp: ID is required")
	}
	q := url.Values{}
	q.Set("id", strconv.FormatInt(req.ID, 10))
	var resp GetAppResponse
	if err := c.doJSON(ctx, http.MethodGet, "/k/v1/app.json", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
