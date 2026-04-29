package kintoneapi

import (
	"context"
	"errors"
	"net/http"
)

// UpdateKey は kintone REST の updateKey 指定構造。
//
// 一意制約のあるフィールドコード（Field）と値（Value）で対象レコードを特定する。
type UpdateKey struct {
	Field string
	Value string
}

// UpdateRecordRequest は PUT /k/v1/record.json の入力。
//
// ID 指定（ID > 0）か UpdateKey 指定の **どちらか必須**（両方/どちらでもなしはエラー）。
// Revision は楽観ロック用（ポインタ nil なら送信しない）。
type UpdateRecordRequest struct {
	App       int64
	ID        int64
	UpdateKey *UpdateKey
	Revision  *int64
	Record    map[string]any
}

// UpdateRecordResponse は PUT /k/v1/record.json のレスポンス。
type UpdateRecordResponse struct {
	Revision string `json:"revision"`
}

// validateUpdateRecord はリクエストの必須項目・排他項目を検証する。
func validateUpdateRecord(req UpdateRecordRequest) error {
	if req.App <= 0 {
		return errors.New("kintoneapi: UpdateRecord: App is required")
	}
	hasID := req.ID > 0
	hasKey := req.UpdateKey != nil
	if hasID && hasKey {
		return errors.New("kintoneapi: UpdateRecord: ID and UpdateKey are mutually exclusive")
	}
	if !hasID && !hasKey {
		return errors.New("kintoneapi: UpdateRecord: either ID or UpdateKey is required")
	}
	if hasKey {
		if req.UpdateKey.Field == "" || req.UpdateKey.Value == "" {
			return errors.New("kintoneapi: UpdateRecord: UpdateKey.Field and Value are required")
		}
	}
	if len(req.Record) == 0 {
		return errors.New("kintoneapi: UpdateRecord: Record fields are required")
	}
	return nil
}

// BuildUpdateRecordBody は UpdateRecord の HTTP body を構築する。
//
// 設計判断（advisor 指摘 #5）:
//   - UpdateRecord 内部送信ロジックと dry-run 出力の **両方が同じ関数** を使う
//   - Revision は nil なら省略、ID/UpdateKey は排他で必ず一方のみ含める
//   - 引数バリデーションは行わない（呼び出し元で実施）
func BuildUpdateRecordBody(req UpdateRecordRequest) map[string]any {
	body := map[string]any{
		"app":    req.App,
		"record": req.Record,
	}
	if req.UpdateKey != nil {
		body["updateKey"] = map[string]any{
			"field": req.UpdateKey.Field,
			"value": req.UpdateKey.Value,
		}
	} else {
		body["id"] = req.ID
	}
	if req.Revision != nil {
		body["revision"] = *req.Revision
	}
	return body
}

// UpdateRecord は kintone のレコードを単件更新する。
//
// バリデーション失敗時は HTTP コールせず error を返す。
// 422 等の API エラーは *APIError として透過する。
//
// retry: 書き込み系は doJSONWithBody が MaxAttempts=1 を強制する（多重更新リスク回避）。
func (c *Client) UpdateRecord(ctx context.Context, req UpdateRecordRequest) (*UpdateRecordResponse, error) {
	if err := validateUpdateRecord(req); err != nil {
		return nil, err
	}
	body := BuildUpdateRecordBody(req)
	var resp UpdateRecordResponse
	if err := c.doJSONWithBody(ctx, http.MethodPut, "/k/v1/record.json", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
