package facade

import (
	"github.com/mark3labs/mcp-go/server"

	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// ToolDeps は facade ハンドラ群が依存するサービス。
//
// MCP server 構築時に注入し、各 tool ハンドラから service/api.API
// を呼び出してビジネスロジックを実行する。
type ToolDeps struct {
	API serviceapi.API
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
