import { describe, expect, it } from "vitest";
import {
  KintoneApiError,
  type KintoneClient,
  KintoneUnauthorizedError,
} from "../../src/kintone/client";
import { createMcpServer } from "../../src/mcp/server";
import { appsSearch } from "../../src/mcp/tools/apps-search";
import { fakeKintoneClient } from "../helpers/fake-kintone-client";
import { fakeKv } from "../helpers/fake-kv";
import { rpcTo } from "../helpers/mcp";
import { firstText } from "../helpers/oauth-flow";

interface Recorder {
  calls: { path: string; query: unknown }[];
  unauthorized: number;
}

function setup(
  get: KintoneClient["get"],
  extra: { kv?: KVNamespace; grant?: { userId: string; grantId: string } } = {},
) {
  const rec: Recorder = { calls: [], unauthorized: 0 };
  const client = fakeKintoneClient({
    get: async (path, query) => {
      rec.calls.push({ path, query });
      return get(path, query);
    },
  });
  const factory = () =>
    createMcpServer({
      // このファイルは kintone_apps_search だけを対象にする(ほかのツールは各自のテストで確かめる)。
      tools: [appsSearch],
      ...extra,
      getClient: () => client,
      onKintoneUnauthorized: () => {
        rec.unauthorized++;
      },
    });
  return { rec, factory };
}

const APPS = { apps: [{ appId: "1", code: "CRM", name: "顧客管理", spaceId: null }] };

describe("MCP server (kintone_apps_search)", () => {
  it("tools/list は kintone_apps_search だけを公開する(読み取り専用の annotation と limit・max_response_tokens 引数つき)", async () => {
    const { factory } = setup(async () => APPS);
    const { result } = await rpcTo(factory, "tools/list");
    const tools = result?.tools ?? [];
    expect(tools.map((t) => t.name)).toEqual(["kintone_apps_search"]);
    expect(tools[0]?.annotations?.readOnlyHint).toBe(true);
    expect(tools[0]?.annotations?.destructiveHint).toBe(false);
    expect(JSON.stringify(tools[0]?.inputSchema)).toContain("limit");
    const schema = tools[0]?.inputSchema as { properties: Record<string, unknown> };
    // 読み取りツールには max_response_tokens が自動で付く(M02)。
    // M03 で仕様書 §5.1 の引数に拡張(ids / codes / name / space_ids / offset を追加)。
    expect(Object.keys(schema.properties)).toEqual([
      "ids",
      "codes",
      "name",
      "space_ids",
      "limit",
      "offset",
      "max_response_tokens",
    ]);
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
      // `name` は M03 で有効な引数になったので、実在しない引数名を使う。
      arguments: { nope: "顧客" },
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

describe("kintone_apps_search の max_response_tokens(読み取りツールの共通引数)", () => {
  const MANY = {
    apps: Array.from({ length: 200 }, (_, i) => ({
      appId: String(i),
      code: `C${i}`,
      name: "顧客管理",
    })),
  };

  it("既定(50000)で通る小さな応答は成功する", async () => {
    const { factory } = setup(async () => APPS);
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: {},
    });
    expect(result?.isError).toBeFalsy();
  });

  it("上限を超えたら切り詰めずに response_too_large(絞り込みの案内つき)", async () => {
    const { factory } = setup(async () => MANY);
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: { max_response_tokens: 100 },
    });
    expect(result?.isError).toBe(true);
    const text = firstText({ result });
    const body = JSON.parse(text);
    expect(body.error).toMatchObject({ code: "response_too_large", retryable: false });
    expect(body.error.message).toContain("max_response_tokens");
    expect(text).not.toContain("顧客管理");
  });

  it("0 で無効にすると同じ応答が通る。limit と併用できる", async () => {
    const { rec, factory } = setup(async () => MANY);
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: { max_response_tokens: 0, limit: 100 },
    });
    expect(result?.isError).toBeFalsy();
    expect(JSON.parse(firstText({ result }))).toEqual(MANY);
    // max_response_tokens は kintone へは送らない。
    expect(rec.calls[0]?.query).toEqual({ limit: 100 });
  });

  it("負の値は argument_error で、kintone を呼ばない", async () => {
    const { rec, factory } = setup(async () => APPS);
    const { result } = await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: { max_response_tokens: -1 },
    });
    expect(JSON.parse(firstText({ result })).error.code).toBe("argument_error");
    expect(rec.calls).toHaveLength(0);
  });
});

describe("kintone_apps_search の引数(仕様書 §5.1。M03 で拡張)", () => {
  const callWith = async (args: Record<string, unknown>) => {
    const s = setup(async () => APPS);
    const { result } = await rpcTo(s.factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: args,
    });
    return { ...s, result };
  };

  it("ids / codes / name / space_ids / limit / offset を kintone のクエリ(ids / codes / name / spaceIds / limit / offset)にして 1 回だけ呼ぶ", async () => {
    const { rec, result } = await callWith({
      ids: [1, 2],
      codes: ["CRM", "TASK"],
      name: "顧客",
      space_ids: [5],
      limit: 20,
      offset: 40,
    });
    expect(result?.isError).toBeFalsy();
    expect(rec.calls).toEqual([
      {
        path: "/k/v1/apps.json",
        query: {
          ids: [1, 2],
          codes: ["CRM", "TASK"],
          name: "顧客",
          spaceIds: [5],
          limit: 20,
          offset: 40,
        },
      },
    ]);
    expect(JSON.parse(firstText({ result }))).toEqual(APPS);
  });

  it("offset: 0 も有効(0 以上)", async () => {
    const { rec, result } = await callWith({ offset: 0 });
    expect(result?.isError).toBeFalsy();
    expect(rec.calls[0]?.query).toEqual({ offset: 0 });
  });

  it("指定しなかった引数は kintone へ送らない", async () => {
    const { rec } = await callWith({ name: "顧客" });
    expect(rec.calls[0]?.query).toEqual({ name: "顧客" });
  });

  it("配列の上限(100 件)ちょうどは通る", async () => {
    const ids = Array.from({ length: 100 }, (_, i) => i + 1);
    const codes = ids.map((i) => `C${i}`);
    const { rec, result } = await callWith({ ids, codes, space_ids: ids });
    expect(result?.isError).toBeFalsy();
    expect(rec.calls).toHaveLength(1);
  });

  it.each([
    ["ids が 101 件", { ids: Array.from({ length: 101 }, (_, i) => i + 1) }],
    ["codes が 101 件", { codes: Array.from({ length: 101 }, (_, i) => `C${i}`) }],
    ["space_ids が 101 件", { space_ids: Array.from({ length: 101 }, (_, i) => i + 1) }],
    ["ids に 0", { ids: [0] }],
    ["ids に負", { ids: [-1] }],
    ["ids に小数", { ids: [1.5] }],
    ["ids が文字列", { ids: "1" }],
    ["codes に空文字", { codes: [""] }],
    ["codes に 65 文字", { codes: ["a".repeat(65)] }],
    ["name が 65 文字", { name: "a".repeat(65) }],
    ["name が数値", { name: 1 }],
    ["space_ids に 0", { space_ids: [0] }],
    ["offset が負", { offset: -1 }],
    ["offset が小数", { offset: 1.5 }],
    ["offset が文字列", { offset: "0" }],
  ])("%s は argument_error で kintone を呼ばない", async (_n, args) => {
    const { rec, result } = await callWith(args);
    expect(result?.isError).toBe(true);
    expect(JSON.parse(firstText({ result })).error.code).toBe("argument_error");
    expect(rec.calls).toHaveLength(0);
  });

  it("codes は 64 文字ちょうど、name は 64 文字ちょうどが通る", async () => {
    const { result } = await callWith({ codes: ["a".repeat(64)], name: "あ".repeat(64) });
    expect(result?.isError).toBeFalsy();
  });

  it("キャッシュ(KV)を読まず、書かない(名前解決を使わない)", async () => {
    const f = fakeKv();
    const { factory } = setup(async () => APPS, { kv: f.kv, grant: { userId: "u", grantId: "g" } });
    await rpcTo(factory, "tools/call", {
      name: "kintone_apps_search",
      arguments: { name: "顧客" },
    });
    expect(f.gets).toHaveLength(0);
    expect(f.puts).toHaveLength(0);
    expect(f.deletes).toHaveLength(0);
  });
});
