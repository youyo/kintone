package operations

import (
	"context"

	"github.com/youyo/kintone/internal/kintoneapi"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// AppDescribeInput は app_describe オペレーションの入力。
type AppDescribeInput struct {
	App  int64  // 必須
	Lang string // 任意（fields 取得時の表示言語: "ja" / "en" / "zh" / "user" / "default"）
}

// AppSummary は GetAppResponse の主要フィールドを抜粋した snake_case 形式。
//
// LLM 文脈で不要な値の細部はそのまま map で保持しつつ、
// 主要フィールド（app_id/code/name/description）はトップレベルに昇格させる。
type AppSummary struct {
	AppID       string         `json:"app_id"`
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	SpaceID     string         `json:"space_id,omitempty"`
	ThreadID    string         `json:"thread_id,omitempty"`
	CreatedAt   string         `json:"created_at,omitempty"`
	Creator     map[string]any `json:"creator,omitempty"`
	ModifiedAt  string         `json:"modified_at,omitempty"`
	Modifier    map[string]any `json:"modifier,omitempty"`
}

// AppDescribeOutput は app + fields を合成したアプリ記述。
//
// LLM 消費しやすさを重視し、app と fields を同一オブジェクトに合成する。
// fields は kintone REST の生 properties をそのまま転載（型変換は M05 以降で検討）。
type AppDescribeOutput struct {
	App      AppSummary                `json:"app"`
	Fields   map[string]map[string]any `json:"fields"`
	Revision string                    `json:"revision,omitempty"`
}

// AppDescribe は GetApp と GetFormFields を順次呼び、合成結果を返す。
//
// 設計判断: 並列呼び出しはしない（順次）。理由:
//  1. M07 でキャッシュが入れば 2 回目以降は高速化される
//  2. 並列にすると errgroup 等の依存が増え、デバッグ難易度が上がる
//  3. レイテンシ要件が厳しくない（CLI / MCP の単発操作）
func AppDescribe(ctx context.Context, a serviceapi.API, in AppDescribeInput) (*AppDescribeOutput, error) {
	if in.App <= 0 {
		return nil, ErrInvalidApp
	}
	app, err := a.GetApp(ctx, kintoneapi.GetAppRequest{ID: in.App})
	if err != nil {
		return nil, err
	}
	fields, err := a.GetFormFields(ctx, kintoneapi.GetFormFieldsRequest{App: in.App, Lang: in.Lang})
	if err != nil {
		return nil, err
	}
	return &AppDescribeOutput{
		App: AppSummary{
			AppID:       app.AppID,
			Code:        app.Code,
			Name:        app.Name,
			Description: app.Description,
			SpaceID:     app.SpaceID,
			ThreadID:    app.ThreadID,
			CreatedAt:   app.CreatedAt,
			Creator:     app.Creator,
			ModifiedAt:  app.ModifiedAt,
			Modifier:    app.Modifier,
		},
		Fields:   fields.Properties,
		Revision: fields.Revision,
	}, nil
}
