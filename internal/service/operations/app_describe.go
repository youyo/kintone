package operations

import (
	"context"

	"github.com/youyo/kintone/internal/kintoneapi"
	"github.com/youyo/kintone/internal/resolver"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// AppDescribeInput は app_describe オペレーションの入力。
//
// App / AppRef は M08 ハイブリッド解決（排他、どちらか必須）。
type AppDescribeInput struct {
	App    int64  // 既存（M04）: int64 直指定（AppRef と排他）
	AppRef string // 新規（M08）: code / name / partial（App と排他）
	Lang   string // 任意（fields 取得時の表示言語: "ja" / "en" / "zh" / "user" / "default"）
}

// AppSummary は GetAppResponse の主要フィールドを抜粋した snake_case 形式。
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
type AppDescribeOutput struct {
	App      AppSummary                `json:"app"`
	Fields   map[string]map[string]any `json:"fields"`
	Revision string                    `json:"revision,omitempty"`
}

// AppDescribe は GetApp と GetFormFields を順次呼び、合成結果を返す。
//
// AppRef が指定された場合は r で App ID に解決してから REST を呼ぶ。
//
// 設計判断: 並列呼び出しはしない（順次）。理由:
//  1. M07 でキャッシュが入ったため 2 回目以降は高速化される
//  2. 並列にすると errgroup 等の依存が増え、デバッグ難易度が上がる
//  3. レイテンシ要件が厳しくない（CLI / MCP の単発操作）
func AppDescribe(ctx context.Context, a serviceapi.API, r *resolver.Resolver, in AppDescribeInput) (*AppDescribeOutput, error) {
	appID, err := resolveAppID(ctx, r, in.App, in.AppRef)
	if err != nil {
		return nil, err
	}
	app, err := a.GetApp(ctx, kintoneapi.GetAppRequest{ID: appID})
	if err != nil {
		return nil, err
	}
	fields, err := a.GetFormFields(ctx, kintoneapi.GetFormFieldsRequest{App: appID, Lang: in.Lang})
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
