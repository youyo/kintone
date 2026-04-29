package facade

import (
	"context"

	"github.com/mark3labs/mcp-go/server"

	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// APIResolver は ctx を見て per-request の service/api.API を返すインターフェース。
//
// 本番では service/api.PrincipalAPIFactory がこれを満たす（ForContext メソッドあり）。
// テストでは任意の関数でスタブ可能。
type APIResolver interface {
	ForContext(ctx context.Context) (serviceapi.API, error)
}

// ToolDeps は facade ハンドラ群が依存するサービス。
//
// MCP server 構築時に注入し、各 tool ハンドラから service/api.API
// を呼び出してビジネスロジックを実行する。
//
// Factory はオプショナル: 設定されている場合、各リクエスト ctx の Principal を見て
// per-user API を解決する（remote MCP / OIDC + OAuth 経路）。nil の場合は API を直接使う
// （stdio / api-token 経路の後方互換）。Factory が ErrAuthRequired を返した場合、
// 呼び出し元は MapError 経由で AUTH_REQUIRED envelope を返す。
type ToolDeps struct {
	API     serviceapi.API
	Factory APIResolver
}

// resolveAPI は ctx の Principal を見て使用する API を決定する。
//
//   - deps.Factory != nil: Factory.ForContext(ctx) を呼ぶ。
//     AuthZ=oauth + Principal 不在では ErrAuthRequired が返る。
//   - deps.Factory == nil: deps.API をそのまま返す（stdio / api-token 経路）。
//
// resolveAPI は nil error 時に必ず非 nil の API を返す。
func resolveAPI(ctx context.Context, deps ToolDeps) (serviceapi.API, error) {
	if deps.Factory != nil {
		return deps.Factory.ForContext(ctx)
	}
	return deps.API, nil
}

// RegisterTools は MCP サーバーに 6 つの tools を登録する。
//
// 登録順は LLM の選択順序に直接影響しないが、可読性のため
// 「アプリ → レコード read → レコード write」の順序にする。
func RegisterTools(s *server.MCPServer, deps ToolDeps) {
	s.AddTool(appsSearchTool(), appsSearchHandler(deps))
	s.AddTool(appDescribeTool(), appDescribeHandler(deps))
	s.AddTool(recordsQueryTool(), recordsQueryHandler(deps))
	s.AddTool(recordCreateTool(), recordCreateHandler(deps))
	s.AddTool(recordUpdateTool(), recordUpdateHandler(deps))
	s.AddTool(recordDeleteTool(), recordDeleteHandler(deps))
}
