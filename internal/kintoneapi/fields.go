package kintoneapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// GetFormFieldsRequest は GET /k/v1/app/form/fields.json の入力。
type GetFormFieldsRequest struct {
	App  int64  // 必須
	Lang string // 任意 ("ja"/"en"/"zh"/"user"/"default")
}

// GetFormFieldsResponse は GET /k/v1/app/form/fields.json のレスポンス。
type GetFormFieldsResponse struct {
	Properties map[string]map[string]any `json:"properties"`
	Revision   string                    `json:"revision"`
}

// GetFormFields は kintone アプリのフィールド定義を取得する。
func (c *Client) GetFormFields(ctx context.Context, req GetFormFieldsRequest) (*GetFormFieldsResponse, error) {
	if req.App <= 0 {
		return nil, errors.New("kintoneapi: GetFormFields: App is required")
	}
	q := url.Values{}
	q.Set("app", strconv.FormatInt(req.App, 10))
	if req.Lang != "" {
		q.Set("lang", req.Lang)
	}
	var resp GetFormFieldsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/k/v1/app/form/fields.json", q, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
