package operations

import (
	"context"
	"fmt"
	"strconv"

	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// RecordCreateInput は record_create オペレーションの入力。
//
// Record（単件）と Records（複数件）どちらか一方を指定する。
// 両方/どちらも未指定はエラー。
type RecordCreateInput struct {
	App     int64
	Record  map[string]any
	Records []map[string]any
}

// RecordCreateOutput は record_create の出力。
//
// kintone REST は ids/revisions を文字列配列で返すため、operations 層で int64 に正規化する。
// LLM が JSON 数値として消費しやすくするための変換。
type RecordCreateOutput struct {
	IDs       []int64 `json:"ids"`
	Revisions []int64 `json:"revisions"`
}

// RecordCreate は POST /k/v1/records.json を呼び、レコードを新規登録する。
//
// バリデーション:
//   - App <= 0 → ErrInvalidApp
//   - len(Records)==0 かつ Record == nil → ErrEmptyRecords
//   - Record と Records 両方指定 → ErrConflictingRecords
func RecordCreate(ctx context.Context, a serviceapi.API, in RecordCreateInput) (*RecordCreateOutput, error) {
	if in.App <= 0 {
		return nil, ErrInvalidApp
	}
	hasSingle := in.Record != nil
	hasMulti := len(in.Records) > 0
	if hasSingle && hasMulti {
		return nil, ErrConflictingRecords
	}
	if !hasSingle && !hasMulti {
		return nil, ErrEmptyRecords
	}

	records := in.Records
	if hasSingle {
		records = []map[string]any{in.Record}
	}

	resp, err := a.InsertRecords(ctx, kintoneapi.InsertRecordsRequest{
		App:     in.App,
		Records: records,
	})
	if err != nil {
		return nil, err
	}
	out := &RecordCreateOutput{
		IDs:       make([]int64, 0, len(resp.IDs)),
		Revisions: make([]int64, 0, len(resp.Revisions)),
	}
	for _, s := range resp.IDs {
		n, perr := strconv.ParseInt(s, 10, 64)
		if perr != nil {
			return nil, fmt.Errorf("operations: parse id %q: %w", s, perr)
		}
		out.IDs = append(out.IDs, n)
	}
	for _, s := range resp.Revisions {
		n, perr := strconv.ParseInt(s, 10, 64)
		if perr != nil {
			return nil, fmt.Errorf("operations: parse revision %q: %w", s, perr)
		}
		out.Revisions = append(out.Revisions, n)
	}
	return out, nil
}
