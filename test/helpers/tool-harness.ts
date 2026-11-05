/**
 * 読み取りツールの unit 用ハーネス。kintone の GET を偽物で受け(呼び出しを記録)、
 * 実際の `createMcpServer`(ALL_TOOLS)の `tools/call` を通す。KV とキャッシュ用の grant も渡せる。
 */

import type { KintoneClient, Query } from "../../src/kintone/client";
import type { ToolDefinition } from "../../src/mcp/registry";
import { createMcpServer } from "../../src/mcp/server";
import { fakeKintoneClient } from "./fake-kintone-client";
import { rpcTo } from "./mcp";
import { firstText, type RpcResponse } from "./oauth-flow";

/** tools/call の応答 JSON(テストが形ごとに確かめるので、キーは固定しない)。 */
// biome-ignore lint/suspicious/noExplicitAny: テストの応答 JSON は形を固定しない
export type ToolBody = Record<string, any>;

export interface RecordedGet {
  path: string;
  query: Query | undefined;
}

export interface RecordedWrite {
  method: "POST" | "PUT" | "DELETE";
  path: string;
  body: unknown;
}

/** i 番目の書き込みの本文(なければ例外)。 */
export function writeBody(h: { writes: RecordedWrite[] }, i = 0): ToolBody {
  const w = h.writes[i];
  if (!w) throw new Error(`書き込みが ${i + 1} 回目まで呼ばれていない`);
  return w.body as ToolBody;
}

export interface HarnessOptions {
  get?: KintoneClient["get"];
  /** 書き込み(POST / PUT / DELETE)の偽物。省略すると、書き込みは呼ばれたら例外(読み取りのテストの保証)。 */
  write?: (method: RecordedWrite["method"], path: string, body: unknown) => unknown;
  kv?: KVNamespace;
  grant?: { userId: string; grantId: string };
  tools?: readonly ToolDefinition[];
}

export function toolHarness(options: HarnessOptions) {
  const calls: RecordedGet[] = [];
  const writes: RecordedWrite[] = [];
  const clientOptions: ({ maxApiCalls?: number } | undefined)[] = [];
  let unauthorized = 0;
  const factory = () =>
    createMcpServer({
      ...(options.kv ? { kv: options.kv } : {}),
      ...(options.grant ? { grant: options.grant } : {}),
      ...(options.tools ? { tools: options.tools } : {}),
      getClient: (o) => {
        clientOptions.push(o);
        const record =
          (method: RecordedWrite["method"], impl: NonNullable<HarnessOptions["write"]>) =>
          async (path: string, body: unknown) => {
            writes.push({ method, path, body });
            return impl(method, path, body);
          };
        const writeMethods = options.write
          ? {
              post: record("POST", options.write),
              put: record("PUT", options.write),
              delete: record("DELETE", options.write),
            }
          : {};
        return fakeKintoneClient({
          get: async (path, query) => {
            calls.push({ path, query });
            if (!options.get) throw new Error(`toolHarness: 想定外の GET ${path}`);
            return options.get(path, query);
          },
          ...writeMethods,
        });
      },
      onKintoneUnauthorized: () => {
        unauthorized++;
      },
    });
  return {
    calls,
    writes,
    clientOptions,
    get unauthorized() {
      return unauthorized;
    },
    factory,
    /** `meta` は tools/call の `params._meta`(冪等キー `idempotency_key` など)。 */
    async call(name: string, args: Record<string, unknown> = {}, meta?: Record<string, unknown>) {
      const rpc: RpcResponse = await rpcTo(factory, "tools/call", {
        name,
        arguments: args,
        ...(meta ? { _meta: meta } : {}),
      });
      const text = firstText(rpc);
      return {
        rpc,
        isError: rpc.result?.isError === true,
        text,
        body: JSON.parse(text) as ToolBody,
      };
    },
  };
}
