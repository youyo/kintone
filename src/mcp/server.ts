import { McpServer } from "@modelcontextprotocol/server";
import { type AppsSearchDeps, registerAppsSearch } from "./tools/apps-search";

export type McpServerDeps = AppsSearchDeps;

/** リクエストごとに生成する(createMcpHandler の要件。同時リクエスト間で状態を共有しない)。 */
export function createMcpServer(deps: McpServerDeps): McpServer {
  const server = new McpServer({ name: "kintone-mcp", version: "0.1.0" });
  registerAppsSearch(server, deps);
  return server;
}
