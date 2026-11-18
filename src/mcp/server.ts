import { McpServer } from "@modelcontextprotocol/server";
import { SERVER_NAME, SERVER_VERSION } from "../server-info";
import { type RegisterDeps, registerTools, type ToolConfig, type ToolDefinition } from "./registry";
import { ALL_TOOLS } from "./tools";

export interface McpServerDeps extends RegisterDeps {
  /** 公開するツールの絞り込み(/mcp の URL クエリから作る)。省略時は全ツール。 */
  config?: ToolConfig;
  /** 公開候補(テスト用)。省略時は ALL_TOOLS。 */
  tools?: readonly ToolDefinition[];
}

const ALL_ENABLED: ToolConfig = { toolsets: "all", readOnly: false };

/** リクエストごとに生成する(createMcpHandler の要件。同時リクエスト間で状態を共有しない)。 */
export function createMcpServer(deps: McpServerDeps): McpServer {
  const server = new McpServer({ name: SERVER_NAME, version: SERVER_VERSION });
  registerTools(server, deps.tools ?? ALL_TOOLS, deps.config ?? ALL_ENABLED, deps);
  return server;
}
