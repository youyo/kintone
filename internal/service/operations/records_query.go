package operations

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// ErrInvalidApp は records_query / app_describe 等で App=0 などが渡されたエラー。
var ErrInvalidApp = errors.New("operations: app must be > 0")

// RecordsQueryInput は records_query オペレーションの入力。
type RecordsQueryInput struct {
	App        int64    // 必須
	Query      string   // 任意（kintone クエリ言語）
	Fields     []string // 任意（レスポンスを絞り込むフィールドコード）
	TotalCount bool     // 任意（true で TotalCount を含める）
}

// RecordsQueryOutput は records_query オペレーションの出力。
//
// kintone REST は totalCount を文字列で返すため、operations 層で int64 に正規化する。
// LLM が JSON 数値として消費しやすくするための変換。
type RecordsQueryOutput struct {
	Records    []map[string]any `json:"records"`
	TotalCount *int64           `json:"total_count,omitempty"`
}

// RecordsQuery は GET /k/v1/records.json を呼び、レコード一覧を取得する。
//
// 名前解決（M08）はこの関数の冒頭（service/api コール前）で挿入する想定。
// キャッシュ（M07）は service/api 層で吸収するため operations は意識しない。
func RecordsQuery(ctx context.Context, a serviceapi.API, in RecordsQueryInput) (*RecordsQueryOutput, error) {
	if in.App <= 0 {
		return nil, ErrInvalidApp
	}
	resp, err := a.GetRecords(ctx, kintoneapi.GetRecordsRequest{
		App:        in.App,
		Query:      in.Query,
		Fields:     in.Fields,
		TotalCount: in.TotalCount,
	})
	if err != nil {
		return nil, err // APIError 等を透過（cli.MapToOutputError がコード分岐）
	}
	out := &RecordsQueryOutput{Records: resp.Records}
	if resp.TotalCount != nil {
		n, parseErr := strconv.ParseInt(*resp.TotalCount, 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("operations: parse total_count %q: %w", *resp.TotalCount, parseErr)
		}
		out.TotalCount = &n
	}
	return out, nil
}
