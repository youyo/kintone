package operations

import (
	"context"
	"fmt"
	"strconv"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/resolver"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// RecordUpdateInput は record_update オペレーションの入力。
//
// App / AppRef は M08 ハイブリッド解決（排他、どちらか必須）。
// UpdateKeyField / UpdateKeyFieldRef も同パターン（排他、UpdateKey 経路で必須）。
//
// ID 指定（ID > 0）か UpdateKey 指定（UpdateKeyField + UpdateKeyValue）のどちらか必須（排他）。
// Revision はポインタ nil で省略（楽観ロック）。
type RecordUpdateInput struct {
	App               int64
	AppRef            string // M08
	ID                int64
	UpdateKeyField    string
	UpdateKeyFieldRef string // M08: label / partial で field code を解決する場合
	UpdateKeyValue    string
	Revision          *int64
	Record            map[string]any
}

// RecordUpdateOutput は record_update の出力。
type RecordUpdateOutput struct {
	Revision int64 `json:"revision"`
}

// RecordUpdate は PUT /k/v1/record.json を呼び、レコードを単件更新する。
//
// 解決順序（M08 計画 #3 advisor 指摘）:
//  1. resolveAppID(App, AppRef) → appID
//  2. UpdateKeyFieldRef != "" のとき r.ResolveField(ctx, appID, UpdateKeyFieldRef) → field code
//  3. ID / UpdateKey 経路の排他チェックを **解決後の updateKeyField** で行う
//
// バリデーション:
//   - App / AppRef → resolveAppID
//   - ID > 0 かつ UpdateKey* どちらか指定 → ErrConflictingUpdateKey
//   - ID == 0 かつ UpdateKey* どちらも空 → ErrMissingUpdateKey
//   - ID == 0 かつ UpdateKey* 片方のみ空 → ErrMissingUpdateKey
//   - Record == nil または len == 0 → ErrEmptyRecord
//   - UpdateKeyField と UpdateKeyFieldRef 両方指定 → ErrConflictingUpdateKeyFieldRef
func RecordUpdate(ctx context.Context, a serviceapi.API, r *resolver.Resolver, in RecordUpdateInput) (*RecordUpdateOutput, error) {
	appID, err := resolveAppID(ctx, r, in.App, in.AppRef)
	if err != nil {
		return nil, err
	}
	updateKeyField, err := resolveUpdateKeyField(ctx, r, appID, in.UpdateKeyField, in.UpdateKeyFieldRef)
	if err != nil {
		return nil, err
	}

	hasID := in.ID > 0
	hasKeyField := updateKeyField != ""
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
		App:      appID,
		Revision: in.Revision,
		Record:   in.Record,
	}
	if hasID {
		req.ID = in.ID
	} else {
		req.UpdateKey = &kintoneapi.UpdateKey{
			Field: updateKeyField,
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
