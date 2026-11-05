import { describe, expect, it } from "vitest";
import { KintoneApiError } from "../../src/kintone/client";
import { appCacheKey, createResolverCache, fieldsCacheKey } from "../../src/resolver/cache";
import { type AppRow, listApps } from "../helpers/fake-kintone-data";
import { fakeKv } from "../helpers/fake-kv";
import { toolHarness } from "../helpers/tool-harness";

const GRANT = { userId: "user-a", grantId: "grant-1" };
const ROWS: AppRow[] = [
  { appId: "12", code: "CRM", name: "顧客管理" },
  { appId: "13", code: "TASK", name: "タスク" },
];
const RECORDS = {
  records: [
    { $id: { type: "__ID__", value: "1" }, customer: { type: "SINGLE_LINE_TEXT", value: "山田" } },
    { $id: { type: "__ID__", value: "2" }, customer: { type: "SINGLE_LINE_TEXT", value: "佐藤" } },
  ],
};

function setup(opts: { kv?: KVNamespace; records?: (q: unknown) => unknown } = {}) {
  return toolHarness({
    ...(opts.kv ? { kv: opts.kv, grant: GRANT } : {}),
    get: async (path, query) => {
      if (path === "/k/v1/apps.json") return listApps(ROWS, query);
      if (path === "/k/v1/records.json") return opts.records ? opts.records(query) : RECORDS;
      throw new Error(`unexpected ${path}`);
    },
  });
}

describe("kintone_records_query(仕様書 §5.3)", () => {
  it("app 指定: GET /k/v1/records.json を 1 回だけ呼ぶ(解決の API は呼ばない)。生のレコードを返す", async () => {
    const h = setup();
    const r = await h.call("kintone_records_query", { app: 12 });
    expect(r.isError).toBe(false);
    expect(h.calls).toEqual([{ path: "/k/v1/records.json", query: { app: 12 } }]);
    expect(r.body).toEqual({ records: RECORDS.records });
  });

  it("query・fields・total_count を kintone のクエリ(query / fields[n] / totalCount)にする", async () => {
    const h = setup({ records: () => ({ ...RECORDS, totalCount: "42" }) });
    const r = await h.call("kintone_records_query", {
      app: 12,
      query: 'customer = "山田" limit 10 offset 20',
      fields: ["$id", "customer"],
      total_count: true,
    });
    expect(h.calls).toEqual([
      {
        path: "/k/v1/records.json",
        query: {
          app: 12,
          query: 'customer = "山田" limit 10 offset 20',
          fields: ["$id", "customer"],
          totalCount: true,
        },
      },
    ]);
    // total_count は数値に変換される。
    expect(r.body).toEqual({ records: RECORDS.records, total_count: 42 });
    expect(typeof r.body.total_count).toBe("number");
  });

  it("total_count を指定しなければ、出力に total_count を出さない", async () => {
    const h = setup();
    const r = await h.call("kintone_records_query", { app: 12, total_count: false });
    expect("total_count" in r.body).toBe(false);
    expect(h.calls[0]?.query).not.toHaveProperty("totalCount");
  });

  it("total_count=true で totalCount が 0 でも出す", async () => {
    const h = setup({ records: () => ({ records: [], totalCount: "0" }) });
    const r = await h.call("kintone_records_query", { app: 12, total_count: true });
    expect(r.body).toEqual({ records: [], total_count: 0 });
  });

  it("totalCount が数値で返ってきても数値のまま", async () => {
    const h = setup({ records: () => ({ records: [], totalCount: 7 }) });
    const r = await h.call("kintone_records_query", { app: 12, total_count: true });
    expect(r.body.total_count).toBe(7);
  });

  it("totalCount が数として読めなければ api_error(黙って落とさない)", async () => {
    const h = setup({ records: () => ({ records: [], totalCount: "abc" }) });
    const r = await h.call("kintone_records_query", { app: 12, total_count: true });
    expect(r.isError).toBe(true);
    expect(r.body.error.code).toBe("api_error");
  });

  it("空の query は送らない", async () => {
    const h = setup();
    await h.call("kintone_records_query", { app: 12, query: "" });
    expect(h.calls[0]?.query).toEqual({ app: 12 });
  });

  it("app_ref(コード)で解決した ID で records.json を呼ぶ(apps.json 1 回 + records.json 1 回)", async () => {
    const h = setup();
    const r = await h.call("kintone_records_query", { app_ref: "CRM" });
    expect(r.isError).toBe(false);
    expect(h.calls.map((c) => c.path)).toEqual(["/k/v1/apps.json", "/k/v1/records.json"]);
    expect(h.calls[1]?.query).toEqual({ app: 12 });
  });

  it("app_ref(名前の部分一致 1 件)は read モードなので採用される", async () => {
    const h = setup();
    const r = await h.call("kintone_records_query", { app_ref: "タス" });
    expect(r.isError).toBe(false);
    expect(h.calls[h.calls.length - 1]?.query).toEqual({ app: 13 });
  });

  it("app_ref が見つからなければ resolver_app_not_found で records.json を呼ばない", async () => {
    const h = setup();
    const r = await h.call("kintone_records_query", { app_ref: "存在しない" });
    expect(r.body.error.code).toBe("resolver_app_not_found");
    expect(r.body.error.details).toEqual({ kind: "app", ref: "存在しない", candidates: [] });
    expect(h.calls.every((c) => c.path === "/k/v1/apps.json")).toBe(true);
  });

  it("fields は 100 件ちょうどが通り、101 件は argument_error", async () => {
    const h = setup();
    const ok = await h.call("kintone_records_query", {
      app: 12,
      fields: Array.from({ length: 100 }, (_, i) => `f${i}`),
    });
    expect(ok.isError).toBe(false);
    const callsBefore = h.calls.length;
    const ng = await h.call("kintone_records_query", {
      app: 12,
      fields: Array.from({ length: 101 }, (_, i) => `f${i}`),
    });
    expect(ng.body.error.code).toBe("argument_error");
    expect(h.calls).toHaveLength(callsBefore);
  });

  it.each([
    ["両方指定", { app: 12, app_ref: "CRM" }],
    ["どちらもなし", { query: "limit 1" }],
    ["app が 0", { app: 0 }],
    ["app_ref が空", { app_ref: "" }],
    ["query が数値", { app: 12, query: 1 }],
    ["fields が文字列", { app: 12, fields: "a" }],
    ["fields に空文字", { app: 12, fields: [""] }],
    ["total_count が文字列", { app: 12, total_count: "true" }],
    ["未知の引数", { app: 12, limit: 10 }],
  ])("%s は argument_error で kintone を呼ばない", async (_n, args) => {
    const h = setup();
    const r = await h.call("kintone_records_query", args);
    expect(r.body.error.code).toBe("argument_error");
    expect(h.calls).toHaveLength(0);
  });

  it("max_response_tokens: 上限を超えたら切り詰めず response_too_large。0 で通る", async () => {
    const big = {
      records: Array.from({ length: 300 }, (_, i) => ({ n: { type: "NUMBER", value: String(i) } })),
    };
    const h = setup({ records: () => big });
    const small = await h.call("kintone_records_query", { app: 12, max_response_tokens: 100 });
    expect(small.body.error.code).toBe("response_too_large");
    expect(small.text).not.toContain('"value":"299"');
    const unlimited = await h.call("kintone_records_query", { app: 12, max_response_tokens: 0 });
    expect(unlimited.isError).toBe(false);
    expect(unlimited.body.records).toHaveLength(300);
  });
});

describe("kintone_records_query とキャッシュ", () => {
  it("app_ref の解決結果をキャッシュし、2 回目は apps.json を呼ばない", async () => {
    const f = fakeKv();
    const h = setup({ kv: f.kv });
    await h.call("kintone_records_query", { app_ref: "顧客管理" });
    expect(h.calls.filter((c) => c.path === "/k/v1/apps.json")).toHaveLength(2); // コード + 名前
    h.calls.length = 0;
    const r = await h.call("kintone_records_query", { app_ref: "顧客管理" });
    expect(r.isError).toBe(false);
    expect(h.calls.map((c) => c.path)).toEqual(["/k/v1/records.json"]);
    expect(h.calls[0]?.query).toEqual({ app: 12 });
  });

  it("キャッシュ由来の解決で records.json が 404 → そのアプリのキャッシュを消してエラーを返す(再解決しない)", async () => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    await cache.putApps("code", "OLD", [{ appId: "77", code: "OLD", name: "削除済み" }]);
    await cache.putFields(77, [{ code: "a", label: "A", type: "NUMBER" }]);
    const h = toolHarness({
      kv: f.kv,
      grant: GRANT,
      get: async (path) => {
        if (path === "/k/v1/records.json")
          throw new KintoneApiError(404, { kintoneCode: "GAIA_AP01" });
        throw new Error(`unexpected ${path}`);
      },
    });
    const r = await h.call("kintone_records_query", { app_ref: "OLD" });
    expect(r.body.error.code).toBe("not_found");
    // 再解決(apps.json)も、records.json の再試行もしない。
    expect(h.calls.map((c) => c.path)).toEqual(["/k/v1/records.json"]);
    expect(f.store.has(await appCacheKey(GRANT, "code", "OLD"))).toBe(false);
    expect(f.store.has(fieldsCacheKey(GRANT, 77))).toBe(false);
  });

  it("API で解決した直後の 404 ではキャッシュを消さない(最新のものを使っているため)", async () => {
    const f = fakeKv();
    const h = toolHarness({
      kv: f.kv,
      grant: GRANT,
      get: async (path, query) => {
        if (path === "/k/v1/apps.json") return listApps(ROWS, query);
        throw new KintoneApiError(404);
      },
    });
    const r = await h.call("kintone_records_query", { app_ref: "CRM" });
    expect(r.body.error.code).toBe("not_found");
    expect(f.deletes).toHaveLength(0);
  });

  it("app を数値で渡した場合の 404 はキャッシュを触らない", async () => {
    const f = fakeKv();
    const h = toolHarness({
      kv: f.kv,
      grant: GRANT,
      get: async () => {
        throw new KintoneApiError(404);
      },
    });
    await h.call("kintone_records_query", { app: 12 });
    expect(f.gets).toHaveLength(0);
    expect(f.deletes).toHaveLength(0);
  });

  it("KV が失敗してもクエリは成功する", async () => {
    const f = fakeKv();
    f.fail.get = true;
    f.fail.put = true;
    const h = setup({ kv: f.kv });
    expect((await h.call("kintone_records_query", { app_ref: "CRM" })).isError).toBe(false);
  });
});

describe("kintone_records_query のツール定義", () => {
  it("ReadOnly・toolset record・maxApiCalls は 36(論理 12 回 × 3)", async () => {
    const { ALL_TOOLS } = await import("../../src/mcp/tools");
    const t = ALL_TOOLS.find((x) => x.name === "kintone_records_query");
    expect(t?.category).toBe("ReadOnly");
    expect(t?.toolset).toBe("record");
    expect(t?.maxApiCalls).toBe(36);
  });
});
