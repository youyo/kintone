package operations

import (
	"context"
	"fmt"
	"strconv"

	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// RecordUpdateInput は record_update オペレーションの入力。
//
// ID 指定（ID > 0）か UpdateKey 指定（Field + Value 両方）の **どちらか必須**（排他）。
// Revision はポインタ nil で省略（楽観ロック）。
type RecordUpdateInput struct {
	App            int64
	ID             int64
	UpdateKeyField string
	UpdateKeyValue string
	Revision       *int64
	Record         map[string]any
}

// RecordUpdateOutput は record_update の出力。
//
// kintone REST は revision を文字列で返すため operations 層で int64 化する。
type RecordUpdateOutput struct {
	Revision int64 `json:"revision"`
}

// RecordUpdate は PUT /k/v1/record.json を呼び、レコードを単件更新する。
//
// バリデーション:
//   - App <= 0 → ErrInvalidApp
//   - ID > 0 かつ UpdateKey* どちらか指定 → ErrConflictingUpdateKey
//   - ID == 0 かつ UpdateKey* どちらも空 → ErrMissingUpdateKey
//   - ID == 0 かつ UpdateKey* 片方のみ空 → ErrMissingUpdateKey
//   - Record == nil または len == 0 → ErrEmptyRecord
func RecordUpdate(ctx context.Context, a serviceapi.API, in RecordUpdateInput) (*RecordUpdateOutput, error) {
	if in.App <= 0 {
		return nil, ErrInvalidApp
	}
	hasID := in.ID > 0
	hasKeyField := in.UpdateKeyField != ""
	hasKeyValue := in.UpdateKeyValue != ""
	hasKey := hasKeyField || hasKeyValue
	if hasID && hasKey {
		return nil, ErrConflictingUpdateKey
	}
	if !hasID {
		// updateKey 経路: Field と Value 両方が必要
		if !hasKeyField || !hasKeyValue {
			return nil, ErrMissingUpdateKey
		}
	}
	if len(in.Record) == 0 {
		return nil, ErrEmptyRecord
	}

	req := kintoneapi.UpdateRecordRequest{
		App:      in.App,
		Revision: in.Revision,
		Record:   in.Record,
	}
	if hasID {
		req.ID = in.ID
	} else {
		req.UpdateKey = &kintoneapi.UpdateKey{
			Field: in.UpdateKeyField,
			Value: in.UpdateKeyValue,
		}
	}

	resp, err := a.UpdateRecord(ctx, req)
	if err != nil {
		return nil, err
	}
	n, perr := strconv.ParseInt(resp.Revision, 10, 64)
	if perr != nil {
		return nil, fmt.Errorf("operations: parse revision %q: %w", resp.Revision, perr)
	}
	return &RecordUpdateOutput{Revision: n}, nil
}
