/**
 * 読み取りツールの unit 用ハーネス。kintone の GET を偽物で受け(呼び出しを記録)、
 * 実際の `createMcpServer`(ALL_TOOLS)の `tools/call` を通す。KV とキャッシュ用の grant も渡せる。
 */
import { createMcpServer } from "../../src/mcp/server";
import type { ToolDefinition } from "../../src/mcp/registry";
import type { KintoneClient, Query } from "../../src/kintone/client";
import { fakeKintoneClient } from "./fake-kintone-client";
import { rpcTo } from "./mcp";
import { firstText, type RpcResponse } from "./oauth-flow";

export interface RecordedGet {
  path: string;
  query: Query | undefined;
}

export interface HarnessOptions {
  get: KintoneClient["get"];
  kv?: KVNamespace;
  grant?: { userId: string; grantId: string };
  tools?: readonly ToolDefinition[];
}

export function toolHarness(options: HarnessOptions) {
  const calls: RecordedGet[] = [];
  const clientOptions: ({ maxApiCalls?: number } | undefined)[] = [];
  let unauthorized = 0;
  const factory = () =>
    createMcpServer({
      ...(options.kv ? { kv: options.kv } : {}),
      ...(options.grant ? { grant: options.grant } : {}),
      ...(options.tools ? { tools: options.tools } : {}),
      getClient: (o) => {
        clientOptions.push(o);
        return fakeKintoneClient({
          get: async (path, query) => {
            calls.push({ path, query });
            return options.get(path, query);
          },
        });
      },
      onKintoneUnauthorized: () => {
        unauthorized++;
      },
    });
  return {
    calls,
    clientOptions,
    get unauthorized() {
      return unauthorized;
    },
    factory,
    async call(name: string, args: Record<string, unknown> = {}) {
      const rpc: RpcResponse = await rpcTo(factory, "tools/call", { name, arguments: args });
      const text = firstText(rpc);
      return {
        rpc,
        isError: rpc.result?.isError === true,
        text,
        body: JSON.parse(text) as Record<string, any>,
      };
    },
  };
}
