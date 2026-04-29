package operations

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/resolver"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// ErrInvalidApp は records_query / app_describe 等で App=0 などが渡されたエラー。
var ErrInvalidApp = errors.New("operations: app must be > 0")

// RecordsQueryInput は records_query オペレーションの入力。
//
// App と AppRef は M08 ハイブリッド解決（排他、どちらか必須）:
//   - App > 0 & AppRef == ""   → 既存の int64 直指定経路（後方互換）
//   - App == 0 & AppRef != ""  → resolver.ResolveApp(AppRef) → ID に変換
//   - 両方指定 → ErrConflictingAppRef
//   - どちらも未指定 → ErrInvalidApp
type RecordsQueryInput struct {
	App        int64    // 既存（M04）: int64 直指定（AppRef と排他）
	AppRef     string   // 新規（M08）: 数値文字列 / code / name / partial（App と排他）
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
// AppRef が指定された場合は r で App ID に解決してから REST を呼ぶ。
// r == nil でも App 直指定経路は動作する（後方互換）。
func RecordsQuery(ctx context.Context, a serviceapi.API, r *resolver.Resolver, in RecordsQueryInput) (*RecordsQueryOutput, error) {
	appID, err := resolveAppID(ctx, r, in.App, in.AppRef)
	if err != nil {
		return nil, err
	}
	resp, err := a.GetRecords(ctx, kintoneapi.GetRecordsRequest{
		App:        appID,
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
