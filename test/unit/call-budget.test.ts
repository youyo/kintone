/**
 * 各ツールの maxApiCalls(ADR 0008 の最悪ケースの式)を、実クライアント + fetch のモックで数えて固定する。
 *   A = アプリ解決の最悪 = コード 1 + 名前 10 = 11、F = 1
 *   apps_search = 1(× 3 = 3)、app_describe = A + 2 = 13(× 3 = 39)、records_query = A + 1 = 12(× 3 = 36)
 * リトライ(読み取りの 429・503 を 1 呼び出しあたり最大 2 回)を含めた最悪の試行数が、宣言した maxApiCalls ちょうどになる。
 *
 * 書き込みツール(M04): 名前解決の GET は読み取りなのでリトライされる(× 3)が、書き込み本体はリトライしない(× 1)。
 *   record_create = A × 3 + 1 = 34、record_update = (A + F) × 3 + 1 = 37、record_delete = A × 3 + 1 = 34(論理は 12 / 13 / 12)
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { createKintoneClient } from "../../src/kintone/client";
import { createMcpServer } from "../../src/mcp/server";
import { ALL_TOOLS } from "../../src/mcp/tools";
import {
  APP_RESOLVE_CALLS,
  FIELD_RESOLVE_CALLS,
  readBudget,
  writeBudget,
} from "../../src/resolver/budget";
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
    // 書き込みはリトライされないので、429 は返さない(1 回で成功する)。
    [`POST ${ORIGIN}/k/v1/records.json`]: () => jsonResponse({ ids: ["1"], revisions: ["1"] }),
    [`PUT ${ORIGIN}/k/v1/record.json`]: () => jsonResponse({ revision: "2" }),
    [`DELETE ${ORIGIN}/k/v1/records.json`]: () => jsonResponse({}),
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

// ---- M04: 書き込みツール ----

const REC = { amount: { value: "1" } };
/** app_ref で最悪(コード 1 + 名前 10 ページ)まで解決させ、書き込みまで進む引数。 */
const WRITE_CASES = [
  {
    tool: "kintone_record_create",
    args: { app_ref: "共通", record: REC },
    /** A + 書き込み 1 */
    logical: APP_RESOLVE_CALLS + 1,
    budget: 34,
    writeMethod: "POST",
  },
  {
    tool: "kintone_record_update",
    // フィールド解決(F)も最悪まで通す: label 完全一致で fields.json 1 回。
    args: {
      app_ref: "共通",
      update_key_field_ref: "金額",
      update_key_value: "1",
      record: REC,
    },
    /** A + F + 書き込み 1 */
    logical: APP_RESOLVE_CALLS + FIELD_RESOLVE_CALLS + 1,
    budget: 37,
    writeMethod: "PUT",
  },
  {
    tool: "kintone_record_delete",
    args: { app_ref: "共通", ids: [1] },
    logical: APP_RESOLVE_CALLS + 1,
    budget: 34,
    writeMethod: "DELETE",
  },
] as const;

describe("書き込みツールの maxApiCalls(解決の読み取り × 3 + 書き込み 1)", () => {
  it("式の部品: writeBudget(読み取りの論理回数, 書き込み回数 = 1) = readBudget + 書き込み", () => {
    expect(writeBudget(APP_RESOLVE_CALLS)).toBe(34);
    expect(writeBudget(APP_RESOLVE_CALLS + FIELD_RESOLVE_CALLS)).toBe(37);
    expect(writeBudget(0)).toBe(1);
    expect(writeBudget(2, 1)).toBe(7);
  });

  it.each(WRITE_CASES)(
    "$tool の maxApiCalls は $budget(論理最悪 $logical 回)",
    ({ tool, budget, logical }) => {
      const def = ALL_TOOLS.find((t) => t.name === tool);
      expect(def?.maxApiCalls).toBe(budget);
      // 論理 = 読み取り(解決)+ 書き込み 1。読み取りだけ × 3。
      expect((logical - 1) * 3 + 1).toBe(budget);
      // 既定の 50 以内。
      expect(budget).toBeLessThanOrEqual(50);
    },
  );

  it.each(WRITE_CASES)(
    "$tool: 全ページ満杯のモックで数えた論理呼び出し数は $logical 回(リトライなし)",
    async ({ tool, args, logical, writeMethod }) => {
      const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES });
      const body = await callTool(tool, args);
      expect(body.error).toBeUndefined();
      expect(m.calls).toHaveLength(logical);
      // 書き込みは最後に 1 回だけ。
      expect(m.calls.filter((c) => c.method === writeMethod)).toHaveLength(1);
      expect(m.calls.at(-1)?.method).toBe(writeMethod);
    },
  );

  it.each(WRITE_CASES)(
    "$tool: 解決の GET がすべて 429 を 2 回返してから成功しても、試行数は maxApiCalls($budget)ちょうどで成功する(書き込みは 1 回)",
    async ({ tool, args, budget, writeMethod }) => {
      const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES, retryTwice: true });
      const body = await callTool(tool, args);
      expect(body.error).toBeUndefined();
      expect(m.calls).toHaveLength(budget);
      expect(m.calls.filter((c) => c.method === "GET")).toHaveLength(budget - 1);
      expect(m.calls.filter((c) => c.method === writeMethod)).toHaveLength(1);
    },
  );

  it.each(WRITE_CASES)(
    "$tool: 名前検索が 10 ページ満杯なら 11 回の GET で resolver_app_list_too_large になり、書き込みを呼ばない",
    async ({ tool, args }) => {
      const m = mockKintone({ apps: manyApps(1500, "共通") });
      const body = await callTool(tool, args);
      expect(body.error.code).toBe("resolver_app_list_too_large");
      expect(m.calls).toHaveLength(11);
      expect(m.calls.every((c) => c.method === "GET")).toBe(true);
    },
  );

  it.each(WRITE_CASES)(
    "$tool: 古い『なし』のキャッシュ(コード・名前)があっても、取り直しを含めて解決の GET は A = 11 回以内",
    async ({ tool, args, logical }) => {
      const f = fakeKv();
      const cache = createResolverCache(f.kv, GRANT);
      await cache.putApps("code", "共通", []);
      await cache.putApps("name", "共通", []);
      const m = mockKintone({ apps: NINE_AND_A_HALF_PAGES });
      const body = await callTool(tool, args, { kv: f.kv });
      expect(body.error).toBeUndefined();
      expect(m.calls).toHaveLength(logical);
      expect(m.calls.filter((c) => new URL(c.url).pathname === "/k/v1/apps.json")).toHaveLength(11);
    },
  );
});
