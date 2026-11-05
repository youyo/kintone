import { describe, expect, it } from "vitest";
import {
  KintoneApiError,
  type KintoneClient,
  KintoneUnauthorizedError,
} from "../../src/kintone/client";
import { createMcpServer } from "../../src/mcp/server";
import { fakeKintoneClient } from "../helpers/fake-kintone-client";
import { rpcTo } from "../helpers/mcp";
import { firstText } from "../helpers/oauth-flow";

interface Recorder {
  calls: { path: string; query: unknown }[];
  unauthorized: number;
}

function setup(get: KintoneClient["get"]) {
  const rec: Recorder = { calls: [], unauthorized: 0 };
  const client = fakeKintoneClient({
    get: async (path, query) => {
      rec.calls.push({ path, query });
      return get(path, query);
    },
  });
  const factory = () =>
    createMcpServer({
      getClient: () => client,
      onKintoneUnauthorized: () => {
        rec.unauthorized++;
      },
    });
  return { rec, factory };
}

const APPS = { apps: [{ appId: "1", code: "CRM", name: "顧客管理", spaceId: null }] };

describe("MCP server (kintone_apps_search)", () => {
  it("tools/list は kintone_apps_search だけを公開する(読み取り専用の annotation と limit 引数つき)", async () => {
    const { factory } = setup(async () => APPS);
    const { result } = await rpcTo(factory, "tools/list");
    const tools = result?.tools ?? [];
    expect(tools.map((t) => t.name)).toEqual(["kintone_apps_search"]);
    expect(tools[0]?.annotations?.readOnlyHint).toBe(true);
    expect(tools[0]?.annotations?.destructiveHint).toBe(false);
    expect(JSON.stringify(tools[0]?.inputSchema)).toContain("limit");
    const schema = tools[0]?.inputSchema as { properties: Record<string, unknown> };
    expect(Object.keys(schema.properties)).toEqual(["limit"]);
  });

  it("tools/call は GET /k/v1/apps.json を limit つきで呼び、kintone の生データを JSON text で返す", async () => {
    const { rec, factory } = setup(async () => APPS);
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: { limit: 1 },
    });
    expect(result?.isError).toBeFalsy();
    expect(rec.calls).toEqual([{ path: "/k/v1/apps.json", query: { limit: 1 } }]);
    expect(JSON.parse(firstText({ result }))).toEqual(APPS);
  });

  it("limit を省略したら limit なしで呼ぶ", async () => {
    const { rec, factory } = setup(async () => APPS);
    await rpcTo(factory, "tools/call", { name: "kintone_apps_search", arguments: {} });
    expect(rec.calls[0]?.query).toEqual({});
  });

  it.each([
    ["0", 0],
    ["101", 101],
    ["小数", 1.5],
    ["負", -1],
    ["文字列", "10"],
  ])("limit が不正(%s)なら argument_error で、kintone を呼ばない", async (_name, limit) => {
    const { rec, factory } = setup(async () => APPS);
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: { limit },
    });
    expect(result?.isError).toBe(true);
    const body = JSON.parse(firstText({ result }));
    expect(body.schema_version).toBe("1");
    expect(body.error.code).toBe("argument_error");
    expect(body.error.retryable).toBe(false);
    expect(rec.calls).toHaveLength(0);
  });

  it("未知の引数は argument_error", async () => {
    const { rec, factory } = setup(async () => APPS);
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: { name: "顧客" },
    });
    expect(result?.isError).toBe(true);
    expect(JSON.parse(firstText({ result })).error.code).toBe("argument_error");
    expect(rec.calls).toHaveLength(0);
  });

  it("kintone の 401 は authentication_error のエンベロープで返し、onKintoneUnauthorized を 1 回呼ぶ", async () => {
    const { rec, factory } = setup(async () => {
      throw new KintoneUnauthorizedError();
    });
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: {},
    });
    expect(rec.unauthorized).toBe(1);
    expect(result?.isError).toBe(true);
    const body = JSON.parse(firstText({ result }));
    expect(body.error.code).toBe("authentication_error");
    expect(body.error.retryable).toBe(false);
  });

  it("kintone のその他のエラーは api_error のエンベロープ(5xx は retryable)で、401 の合図は呼ばない", async () => {
    const { rec, factory } = setup(async () => {
      throw new KintoneApiError(503);
    });
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: {},
    });
    expect(rec.unauthorized).toBe(0);
    const body = JSON.parse(firstText({ result }));
    expect(body.error.code).toBe("api_error");
    expect(body.error.retryable).toBe(true);
  });

  it("想定外の例外は generic_error(固定文。内部情報を出さない)", async () => {
    const { factory } = setup(async () => {
      throw new Error("internal kt-secret-token");
    });
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: {},
    });
    const text = firstText({ result });
    expect(JSON.parse(text).error.code).toBe("generic_error");
    expect(text).not.toContain("kt-secret-token");
  });
});
