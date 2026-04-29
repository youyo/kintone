package kintoneapi

import (
	"context"
	"errors"
	"net/http"
)

// InsertRecordsRequest は POST /k/v1/records.json の入力。
type InsertRecordsRequest struct {
	// App は kintone アプリ ID（必須・>0）。
	App int64
	// Records は新規登録するレコード配列（必須・1 件以上）。
	// 各 map は kintone のフィールドコード → {"value": ...} 形式。
	Records []map[string]any
}

// InsertRecordsResponse は POST /k/v1/records.json のレスポンス。
//
// ids/revisions は kintone REST 仕様で **数字文字列の配列** で返る。
// LLM 向けの int64 化は service/operations 層で行う。
type InsertRecordsResponse struct {
	IDs       []string `json:"ids"`
	Revisions []string `json:"revisions"`
}

// BuildInsertRecordsBody は InsertRecords の HTTP body を構築する。
//
// 設計判断（advisor 指摘 #5）:
//   - InsertRecords 内部送信ロジックと dry-run 出力の **両方が同じ関数** を使う
//   - 引数バリデーションは行わない（呼び出し元で実施）
func BuildInsertRecordsBody(req InsertRecordsRequest) map[string]any {
	return map[string]any{
		"app":     req.App,
		"records": req.Records,
	}
}

// InsertRecords は kintone のレコードを複数件新規登録する。
//
// バリデーション:
//   - App <= 0 → error
//   - len(Records) == 0 → error
//
// kintone REST の retry 方針: write 系は doJSONWithBody が **MaxAttempts=1** を強制する
// （body 再送による多重作成リスクを回避。advisor 指摘 #3 反映）。
func (c *Client) InsertRecords(ctx context.Context, req InsertRecordsRequest) (*InsertRecordsResponse, error) {
	if req.App <= 0 {
		return nil, errors.New("kintoneapi: InsertRecords: App is required")
	}
	if len(req.Records) == 0 {
		return nil, errors.New("kintoneapi: InsertRecords: at least one record is required")
	}
	body := BuildInsertRecordsBody(req)
	var resp InsertRecordsResponse
	if err := c.doJSONWithBody(ctx, http.MethodPost, "/k/v1/records.json", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteRecordsRequest は DELETE /k/v1/records.json の入力。
type DeleteRecordsRequest struct {
	// App は kintone アプリ ID（必須・>0）。
	App int64
	// IDs は削除対象レコード ID 配列（必須・1 件以上、各要素 >0）。
	IDs []int64
	// Revisions は楽観ロック用 revision 配列（任意。指定時は IDs と同要素数）。
	Revisions []int64
}

// BuildDeleteRecordsBody は DeleteRecords の HTTP body を構築する。
//
// Revisions が空のときはキー自体を含めない（dry-run と実 API 送信の整合のため）。
func BuildDeleteRecordsBody(req DeleteRecordsRequest) map[string]any {
	body := map[string]any{
		"app": req.App,
		"ids": req.IDs,
	}
	if len(req.Revisions) > 0 {
		body["revisions"] = req.Revisions
	}
	return body
}

// DeleteRecords は kintone のレコードを複数件削除する。
//
// kintone REST の DELETE はリクエスト body に JSON を載せる仕様（query bracket 表記より UX 良）。
// 200 OK で空 body （または `{}`）が返るため戻り値は nil error のみ。
func (c *Client) DeleteRecords(ctx context.Context, req DeleteRecordsRequest) error {
	if req.App <= 0 {
		return errors.New("kintoneapi: DeleteRecords: App is required")
	}
	if len(req.IDs) == 0 {
		return errors.New("kintoneapi: DeleteRecords: at least one id is required")
	}
	body := BuildDeleteRecordsBody(req)
	return c.doJSONWithBody(ctx, http.MethodDelete, "/k/v1/records.json", body, nil)
}
