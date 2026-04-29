package operations

import (
	"context"

	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// RecordDeleteInput は record_delete オペレーションの入力。
type RecordDeleteInput struct {
	App       int64
	IDs       []int64
	Revisions []int64
}

// RecordDeleteOutput は record_delete の出力。
//
// kintone REST は 200 OK で空 body のため、operations 層で削除件数（len(IDs)）を返す。
type RecordDeleteOutput struct {
	Deleted int `json:"deleted"`
}

// RecordDelete は DELETE /k/v1/records.json を呼ぶ。
//
// バリデーション:
//   - App <= 0 → ErrInvalidApp
//   - len(IDs) == 0 → ErrEmptyIDs
//   - IDs に <= 0 の要素が含まれる → ErrInvalidID
//   - len(Revisions) > 0 かつ len(Revisions) != len(IDs) → ErrRevisionsLengthMismatch
func RecordDelete(ctx context.Context, a serviceapi.API, in RecordDeleteInput) (*RecordDeleteOutput, error) {
	if in.App <= 0 {
		return nil, ErrInvalidApp
	}
	if len(in.IDs) == 0 {
		return nil, ErrEmptyIDs
	}
	for _, id := range in.IDs {
		if id <= 0 {
			return nil, ErrInvalidID
		}
	}
	if len(in.Revisions) > 0 && len(in.Revisions) != len(in.IDs) {
		return nil, ErrRevisionsLengthMismatch
	}

	if err := a.DeleteRecords(ctx, kintoneapi.DeleteRecordsRequest{
		App:       in.App,
		IDs:       in.IDs,
		Revisions: in.Revisions,
	}); err != nil {
		return nil, err
	}
	return &RecordDeleteOutput{Deleted: len(in.IDs)}, nil
}
