package api

import (
	"context"
	"errors"

	"github.com/youyo/kintone/internal/kintoneapi"
)

// ErrNilClient は NewFromKintone に nil の kintoneapi.Client を渡したときのエラー。
var ErrNilClient = errors.New("service/api: kintoneapi.Client is nil")

// API は kintone REST API への薄い透過層インターフェイス。
//
// このインターフェイスは read/write 全エンドポイントを将来的に持つが、
// M04 では read 系のみ実装する。M05 で write 系（Insert/Update/Delete）を追加する。
//
// operations / facade / cli はこのインターフェイス越しに依存することで、
// M07 のキャッシュ層・テスト時の mock を容易にする。
type API interface {
	GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error)
	GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error)
	GetApp(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error)
	GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error)
}

// Client は API インターフェイスの kintoneapi.Client ベース実装。
// 複数 goroutine から安全に共有可能（kintoneapi.Client が複数 goroutine 安全）。
type Client struct {
	k *kintoneapi.Client
}

// NewFromKintone は kintoneapi.Client から API 実装を構築する。
// k が nil の場合 ErrNilClient を返す。
func NewFromKintone(k *kintoneapi.Client) (*Client, error) {
	if k == nil {
		return nil, ErrNilClient
	}
	return &Client{k: k}, nil
}

// GetRecords は GET /k/v1/records.json を呼ぶ。
func (c *Client) GetRecords(ctx context.Context, req kintoneapi.GetRecordsRequest) (*kintoneapi.GetRecordsResponse, error) {
	return c.k.GetRecords(ctx, req)
}

// GetRecord は GET /k/v1/record.json を呼ぶ。
func (c *Client) GetRecord(ctx context.Context, req kintoneapi.GetRecordRequest) (*kintoneapi.GetRecordResponse, error) {
	return c.k.GetRecord(ctx, req)
}

// GetApp は GET /k/v1/app.json を呼ぶ。
func (c *Client) GetApp(ctx context.Context, req kintoneapi.GetAppRequest) (*kintoneapi.GetAppResponse, error) {
	return c.k.GetApp(ctx, req)
}

// GetFormFields は GET /k/v1/app/form/fields.json を呼ぶ。
func (c *Client) GetFormFields(ctx context.Context, req kintoneapi.GetFormFieldsRequest) (*kintoneapi.GetFormFieldsResponse, error) {
	return c.k.GetFormFields(ctx, req)
}
