/**
 * 各ツールの maxApiCalls(ADR 0008 の最悪ケースの式)を、実クライアント + fetch のモックで数えて固定する。
 *   A = アプリ解決の最悪 = コード 1 + 名前 10 = 11、F = 1
 *   apps_search = 1(× 3 = 3)、app_describe = A + 2 = 13(× 3 = 39)、records_query = A + 1 = 12(× 3 = 36)
 * リトライ(読み取りの 429・503 を 1 呼び出しあたり最大 2 回)を含めた最悪の試行数が、宣言した maxApiCalls ちょうどになる。
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { createKintoneClient } from "../../src/kintone/client";
import { createMcpServer } from "../../src/mcp/server";
import { ALL_TOOLS } from "../../src/mcp/tools";
import { APP_RESOLVE_CALLS, FIELD_RESOLVE_CALLS, readBudget } from "../../src/resolver/budget";
import { appCacheKey, createResolverCache } from "../../src/resolver/cache";
import {
  type AppRow,
  fieldsResponse,
  listApps,
  manyApps,
  queryFromSearchParams,
} from "../helpers/fake-kintone-data";
import { fakeKv } from "../helpers/fake-kv";
import { rpcTo } from "../helpers/mcp";
import { jsonResponse, mockOutboundFetch, type RecordedCall } from "../helpers/mock-fetch";
import { firstText } from "../helpers/oauth-flow";
import type { ToolBody } from "../helpers/tool-harness";

afterEach(() => vi.restoreAllMocks());

const ORIGIN = "https://example.cybozu.com";
const GRANT = { userId: "user-a", grantId: "grant-1" };
const APP_JSON = { appId: "1", code: "T", name: "共通", description: "" };

/** 名前が `共通` に部分一致する 949 件 + 完全一致 1 件 = 950 件(10 ページ目が 50 件で終わる)。 */
const NINE_AND_A_HALF_PAGES: AppRow[] = [
  ...manyApps(949, "共通"),
  { appId: "1", code: "T", name: "共通" },
];

interface World {
  apps: AppRow[];
  /** true なら、同じリクエスト(URL)の最初の 2 回は 429 を返す(最悪のリトライ)。 */
  retryTwice?: boolean;
}

function mockKintone(world: World) {
  const attempts = new Map<string, number>();
  const maybe429 = (call: RecordedCall, ok: () => Response) => {
    if (world.retryTwice) {
      const n = (attempts.get(call.url) ?? 0) + 1;
      attempts.set(call.url, n);
      if (n <= 2) return new Response("{}", { status: 429, headers: { "Retry-After": "0" } });
    }
    return ok();
  };
  return mockOutboundFetch({
    [`GET ${ORIGIN}/k/v1/apps.json`]: (call) =>
      maybe429(call, () =>
        jsonResponse(listApps(world.apps, queryFromSearchParams(new URL(call.url).searchParams))),
      ),
    [`GET ${ORIGIN}/k/v1/app.json`]: (call) => maybe429(call, () => jsonResponse(APP_JSON)),
    [`GET ${ORIGIN}/k/v1/app/form/fields.json`]: (call) =>
      maybe429(call, () =>
        jsonResponse(fieldsResponse([{ type: "NUMBER", code: "amount", label: "金額" }])),
      ),
    [`GET ${ORIGIN}/k/v1/records.json`]: (call) =>
      maybe429(call, () => jsonResponse({ records: [], totalCount: "0" })),
  });
}

async function callTool(
  name: string,
  args: Record<string, unknown>,
  deps: { kv?: KVNamespace } = {},
) {
  const rpc = await rpcTo(
    () =>
      createMcpServer({
        ...(deps.kv ? { kv: deps.kv, grant: GRANT } : {}),
        getClient: ({ maxApiCalls }) =>
          createKintoneClient({
            host: "example.cybozu.com",
            accessToken: "kt-test-token",
            ...(maxApiCalls === undefined ? {} : { maxApiCalls }),
          }),
        onKintoneUnauthorized: () => {},
      }),
    "tools/call",
    { name, arguments: args },
  );
  return JSON.parse(firstText(rpc)) as ToolBody;
}

describe("maxApiCalls の宣言値(最悪ケースの式 × 3)", () => {
  it("式の部品: A = 11(コード 1 + 名前 10)、F = 1、リトライ込みは × 3", () => {
    expect(APP_RESOLVE_CALLS).toBe(11);
    expect(FIELD_RESOLVE_CALLS).toBe(1);
    expect(readBudget(1)).toBe(3);
    expect(readBudget(13)).toBe(39);
  });

  it.each([
    ["kintone_apps_search", 3, 1],
    ["kintone_app_describe", 39, 13],
    ["kintone_records_query", 36, 12],
  ])("%s の maxApiCalls は %i(論理最悪 %i 回 × 3)", (name, budget, logical) => {
    const tool = ALL_TOOLS.find((t) => t.name === name);
    expect(tool?.maxApiCalls).toBe(budget);
    expect(logical * 3).toBe(budget);
    // 既定の 50 以内。
    expect(budget).toBeLessThanOrEqual(50);
  });
});

describe("全ページ満杯のモックで数えた論理呼び出し数(リトライなし)", () => {
  it("kintone_apps_search = 1 回", async () => {
    const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES });
    const body = await callTool("kintone_apps_search", { name: "共通" });
    expect(body.apps).toBeDefined();
    expect(m.calls).toHaveLength(1);
  });

  it("kintone_app_describe = A + 2 = 13 回(コード 1 + 名前 10 + app.json + fields.json)", async () => {
    const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES });
    const body = await callTool("kintone_app_describe", { app_ref: "共通" });
    expect(body.error).toBeUndefined();
    expect(body.app.app_id).toBe("1");
    expect(m.calls).toHaveLength(APP_RESOLVE_CALLS + 2);
    expect(m.calls).toHaveLength(13);
  });

  it("kintone_records_query = A + 1 = 12 回", async () => {
    const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES });
    const body = await callTool("kintone_records_query", { app_ref: "共通" });
    expect(body.error).toBeUndefined();
    expect(m.calls).toHaveLength(APP_RESOLVE_CALLS + 1);
    expect(m.calls).toHaveLength(12);
  });

  it("名前検索が 10 ページ満杯なら 11 回で resolver_app_list_too_large(それ以上は呼ばない)", async () => {
    const m = mockKintone({ apps: manyApps(1500, "共通") });
    for (const tool of ["kintone_app_describe", "kintone_records_query"]) {
      m.calls.length = 0;
      const body = await callTool(tool, { app_ref: "共通" });
      expect(body.error.code, tool).toBe("resolver_app_list_too_large");
      expect(m.calls, tool).toHaveLength(11);
    }
  });
});

describe("最悪のリトライ(すべての呼び出しが 429 を 2 回返してから成功)でも maxApiCalls ちょうどで収まる", () => {
  it("kintone_apps_search: 3 試行 = maxApiCalls 3", async () => {
    const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES, retryTwice: true });
    const body = await callTool("kintone_apps_search", {});
    expect(body.error).toBeUndefined();
    expect(m.calls).toHaveLength(3);
  });

  it("kintone_app_describe: 13 × 3 = 39 試行 = maxApiCalls 39", async () => {
    const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES, retryTwice: true });
    const body = await callTool("kintone_app_describe", { app_ref: "共通" });
    expect(body.error).toBeUndefined();
    expect(m.calls).toHaveLength(39);
  });

  it("kintone_records_query: 12 × 3 = 36 試行 = maxApiCalls 36", async () => {
    const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES, retryTwice: true });
    const body = await callTool("kintone_records_query", { app_ref: "共通" });
    expect(body.error).toBeUndefined();
    expect(m.calls).toHaveLength(36);
  });
});

describe("キャッシュの取り直しを含めても、解決の呼び出しは A = 11 以内", () => {
  it("古い『なし』のキャッシュ(コード・名前)があり、実際は 10 ページ目まであるアプリでも 11 回で解決する", async () => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    await cache.putApps("code", "共通", []);
    await cache.putApps("name", "共通", []);
    const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES });
    const body = await callTool("kintone_records_query", { app_ref: "共通" }, { kv: f.kv });
    expect(body.error).toBeUndefined();
    // 解決 11(コード 1 + 名前 10)+ records.json 1 = 12。取り直しで 11 を超えない。
    expect(m.calls).toHaveLength(12);
    expect(m.calls.filter((c) => new URL(c.url).pathname === "/k/v1/apps.json")).toHaveLength(11);
  });

  it("コードだけがキャッシュ済みなら名前の 10 ページだけで解決でき(not_found ではないので)コードを取り直さない", async () => {
    const f = fakeKv();
    await createResolverCache(f.kv, GRANT).putApps("code", "共通XX", []);
    const m = mockKintone({
      apps: [...manyApps(949, "共通XX"), { appId: "1", code: "T", name: "共通XX" }],
    });
    const body = await callTool("kintone_records_query", { app_ref: "共通XX" }, { kv: f.kv });
    expect(body.error).toBeUndefined();
    expect(m.calls.filter((c) => new URL(c.url).pathname === "/k/v1/apps.json")).toHaveLength(10);
    expect(f.store.has(await appCacheKey(GRANT, "code", "共通XX"))).toBe(true);
  });
});
