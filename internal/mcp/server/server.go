// Package server は kintone MCP サーバーの初期化と stdio 起動を提供する。
//
// 設計判断:
//   - mark3labs/mcp-go の薄いラッパー層に留める
//   - 6 つの kintone tools の登録は internal/mcp/facade.RegisterTools に委譲
//   - 起動方式は M06 では stdio のみ（HTTP/SSE は M10 で remote 対応）
package server

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/youyo/kintone/internal/mcp/facade"
	serviceapi "github.com/youyo/kintone/internal/service/api"
)

// ServerName は MCP server.NewMCPServer に渡す名前。
//
// LLM 側の MCP server リスト表示で識別に使われる。
const ServerName = "kintone"

// Version は MCP server.NewMCPServer に渡すバージョン。
//
// 当面は内部 CLI と同じ "0.1.0" を使う。M11 でビルド時注入に切替予定。
const Version = "0.1.0"

// New は kintone MCP server を構築する。
//
// api を nil で渡すとパニックではなく facade ハンドラ呼び出し時に
// nil panic が起きるが、そもそも cli 経路では NewAPIBuilder が
// 失敗するため到達しない。テストでは mock API を渡す。
func New(api serviceapi.API) *server.MCPServer {
	s := server.NewMCPServer(
		ServerName,
		Version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	facade.RegisterTools(s, facade.ToolDeps{API: api})
	return s
}

// ServeStdio は MCP server を stdio JSON-RPC でブロック起動する。
//
// 標準入出力を MCP transport にバインドし、io.EOF までブロックする。
// CLI コマンドからは `kintone mcp serve` がこの関数を呼ぶ。
func ServeStdio(s *server.MCPServer) error {
	return server.ServeStdio(s)
}
