import { describe, expect, it } from "vitest";
import { KintoneApiError } from "../../src/kintone/client";
import { createResolverCache, fieldsCacheKey } from "../../src/resolver/cache";
import { type AppRow, fieldsResponse, listApps } from "../helpers/fake-kintone-data";
import { fakeKv } from "../helpers/fake-kv";
import { toolHarness } from "../helpers/tool-harness";

const GRANT = { userId: "user-a", grantId: "grant-1" };
const ROWS: AppRow[] = [
  { appId: "12", code: "CRM", name: "顧客管理" },
  { appId: "13", code: "TASK", name: "タスク" },
];

const APP_JSON = {
  appId: "12",
  code: "CRM",
  name: "顧客管理",
  description: "顧客の台帳",
  spaceId: null,
  threadId: null,
  createdAt: "2024-01-01T00:00:00.000Z",
  creator: { code: "alice", name: "Alice" },
  modifiedAt: "2024-02-01T00:00:00.000Z",
  modifier: { code: "bob", name: "Bob" },
};

const FIELDS = fieldsResponse(
  [
    { type: "SINGLE_LINE_TEXT", code: "customer", label: "顧客名", required: true },
    { type: "NUMBER", code: "amount", label: "金額" },
  ],
  "7",
);

function setup(opts: { kv?: KVNamespace; app?: unknown } = {}) {
  return toolHarness({
    ...(opts.kv ? { kv: opts.kv, grant: GRANT } : {}),
    get: async (path, query) => {
      if (path === "/k/v1/apps.json") return listApps(ROWS, query);
      if (path === "/k/v1/app.json") return opts.app ?? APP_JSON;
      if (path === "/k/v1/app/form/fields.json") return FIELDS;
      throw new Error(`unexpected ${path}`);
    },
  });
}

describe("kintone_app_describe(仕様書 §5.2)", () => {
  it("app 指定: app.json → form/fields.json の順に 1 回ずつ呼び、解決のための apps.json は呼ばない", async () => {
    const h = setup();
    const r = await h.call("kintone_app_describe", { app: 12 });
    expect(r.isError).toBe(false);
    expect(h.calls).toEqual([
      { path: "/k/v1/app.json", query: { id: 12 } },
      { path: "/k/v1/app/form/fields.json", query: { app: 12 } },
    ]);
  });

  it("出力は {app(snake_case), fields(kintone の生定義), revision}", async () => {
    const h = setup();
    const { body } = await h.call("kintone_app_describe", { app: 12 });
    expect(body).toEqual({
      app: {
        app_id: "12",
        code: "CRM",
        name: "顧客管理",
        description: "顧客の台帳",
        created_at: "2024-01-01T00:00:00.000Z",
        creator: { code: "alice", name: "Alice" },
        modified_at: "2024-02-01T00:00:00.000Z",
        modifier: { code: "bob", name: "Bob" },
      },
      fields: FIELDS.properties,
      revision: "7",
    });
  });

  it("空・null の項目は省略する(space_id / thread_id)が、app_id・code・name・description は常に出す", async () => {
    const h = setup({
      app: { appId: "12", code: "", name: "無題", description: "", spaceId: null, threadId: null },
    });
    const { body } = await h.call("kintone_app_describe", { app: 12 });
    expect(body.app).toEqual({ app_id: "12", code: "", name: "無題", description: "" });
  });

  it("スペース内のアプリは space_id / thread_id を出す", async () => {
    const h = setup({ app: { ...APP_JSON, spaceId: "5", threadId: "9" } });
    const { body } = await h.call("kintone_app_describe", { app: 12 });
    expect(body.app).toMatchObject({ space_id: "5", thread_id: "9" });
  });

  it("lang を form/fields.json へ渡す", async () => {
    const h = setup();
    await h.call("kintone_app_describe", { app: 12, lang: "en" });
    expect(h.calls[1]).toEqual({
      path: "/k/v1/app/form/fields.json",
      query: { app: 12, lang: "en" },
    });
  });

  it.each(["ja", "en", "zh", "user", "default"])("lang=%s は有効", async (lang) => {
    const h = setup();
    expect((await h.call("kintone_app_describe", { app: 12, lang })).isError).toBe(false);
  });

  it("app_ref(コード): 解決してから app.json → fields.json(合計 3 回)", async () => {
    const h = setup();
    const r = await h.call("kintone_app_describe", { app_ref: "CRM" });
    expect(r.isError).toBe(false);
    expect(h.calls.map((c) => c.path)).toEqual([
      "/k/v1/apps.json",
      "/k/v1/app.json",
      "/k/v1/app/form/fields.json",
    ]);
    expect(h.calls[1]?.query).toEqual({ id: 12 });
  });

  it("app_ref(名前の部分一致 1 件)は read モードなので採用される", async () => {
    const h = setup();
    expect((await h.call("kintone_app_describe", { app_ref: "顧客" })).isError).toBe(false);
    expect(h.calls[h.calls.length - 2]?.query).toEqual({ id: 12 });
  });

  it("解決できないとき(ambiguous)は candidates つきのエンベロープで、app.json は呼ばない", async () => {
    const h = toolHarness({
      get: async (_path, query) =>
        listApps(
          [
            { appId: "1", code: "A", name: "顧客A" },
            { appId: "2", code: "B", name: "顧客B" },
          ],
          query,
        ),
    });
    const r = await h.call("kintone_app_describe", { app_ref: "顧客" });
    expect(r.isError).toBe(true);
    expect(r.body.error.code).toBe("resolver_app_ambiguous");
    expect(r.body.error.details.candidates).toEqual([
      { id: "1", code: "A", name: "顧客A" },
      { id: "2", code: "B", name: "顧客B" },
    ]);
    expect(h.calls.every((c) => c.path === "/k/v1/apps.json")).toBe(true);
  });

  it.each([
    ["両方指定", { app: 12, app_ref: "CRM" }],
    ["どちらもなし", {}],
    ["app が 0", { app: 0 }],
    ["app が負", { app: -1 }],
    ["app が小数", { app: 1.5 }],
    ["app が文字列", { app: "12" }],
    ["app_ref が空文字", { app_ref: "" }],
    ["app_ref が数値", { app_ref: 12 }],
    ["lang が不正", { app: 12, lang: "fr" }],
    ["未知の引数", { app: 12, nope: 1 }],
  ])("%s は argument_error で kintone を呼ばない", async (_n, args) => {
    const h = setup();
    const r = await h.call("kintone_app_describe", args);
    expect(r.isError).toBe(true);
    expect(r.body.error.code).toBe("argument_error");
    expect(h.calls).toHaveLength(0);
  });

  it("app / app_ref の排他は message に書かれる", async () => {
    const h = setup();
    const both = await h.call("kintone_app_describe", { app: 12, app_ref: "CRM" });
    expect(both.body.error.message).toMatch(/app.*app_ref|exactly one|either/i);
    const none = await h.call("kintone_app_describe", {});
    expect(none.body.error.message).toMatch(/app.*app_ref|exactly one|either/i);
  });

  it("kintone の 404 は not_found のまま伝わる", async () => {
    const h = toolHarness({
      get: async () => {
        throw new KintoneApiError(404, { kintoneCode: "GAIA_AP01" });
      },
    });
    const r = await h.call("kintone_app_describe", { app: 999 });
    expect(r.body.error).toMatchObject({ code: "not_found", details: { http_status: 404 } });
  });

  it("max_response_tokens が効く(超えたら response_too_large)", async () => {
    const h = setup();
    const r = await h.call("kintone_app_describe", { app: 12, max_response_tokens: 5 });
    expect(r.body.error.code).toBe("response_too_large");
  });
});

describe("kintone_app_describe とキャッシュ(ADR 0007)", () => {
  it("取得したフィールド定義でフィールドのキャッシュを更新する(code / label / type だけ)", async () => {
    const f = fakeKv();
    const h = setup({ kv: f.kv });
    await h.call("kintone_app_describe", { app: 12 });
    const put = f.puts.find((p) => p.key === fieldsCacheKey(GRANT, 12));
    expect(put?.options).toEqual({ expirationTtl: 600 });
    expect(JSON.parse(put?.value ?? "null")).toEqual([
      { code: "customer", label: "顧客名", type: "SINGLE_LINE_TEXT" },
      { code: "amount", label: "金額", type: "NUMBER" },
    ]);
  });

  it("キャッシュを読まない: 古いキャッシュがあっても常に kintone から取る(app_ref の解決を含む)", async () => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    // 古い(別のアプリを指す)キャッシュ。
    await cache.putApps("code", "CRM", [{ appId: "999", code: "CRM", name: "古い" }]);
    await cache.putFields(12, [{ code: "old", label: "旧", type: "NUMBER" }]);
    const h = setup({ kv: f.kv });
    f.gets.length = 0;
    const r = await h.call("kintone_app_describe", { app_ref: "CRM" });
    expect(f.gets).toHaveLength(0);
    expect(h.calls[1]?.query).toEqual({ id: 12 }); // 古いキャッシュの 999 ではない
    expect(Object.keys(r.body.fields)).toEqual(["customer", "amount"]);
    // 取得結果でキャッシュが更新された。
    expect(await cache.getFields(12)).toEqual([
      { code: "customer", label: "顧客名", type: "SINGLE_LINE_TEXT" },
      { code: "amount", label: "金額", type: "NUMBER" },
    ]);
    expect(await cache.getApps("code", "CRM")).toEqual([
      { appId: "12", code: "CRM", name: "顧客管理" },
    ]);
  });

  it("KV が失敗しても describe は成功する", async () => {
    const f = fakeKv();
    f.fail.put = true;
    f.fail.get = true;
    const h = setup({ kv: f.kv });
    const r = await h.call("kintone_app_describe", { app_ref: "CRM" });
    expect(r.isError).toBe(false);
  });

  it("grant が分からない(キャッシュなし)でも動く", async () => {
    const h = setup();
    expect((await h.call("kintone_app_describe", { app_ref: "CRM" })).isError).toBe(false);
  });
});

describe("kintone_app_describe のツール定義", () => {
  it("ReadOnly・toolset app・maxApiCalls は 39(論理 13 回 × 3)", async () => {
    const { ALL_TOOLS } = await import("../../src/mcp/tools");
    const t = ALL_TOOLS.find((x) => x.name === "kintone_app_describe");
    expect(t).toBeDefined();
    expect(t?.category).toBe("ReadOnly");
    expect(t?.toolset).toBe("app");
    expect(t?.maxApiCalls).toBe(39);
  });

  it("tools/call のクライアントには maxApiCalls=39 が渡る", async () => {
    const h = setup();
    await h.call("kintone_app_describe", { app: 12 });
    expect(h.clientOptions).toEqual([{ maxApiCalls: 39 }]);
  });
});
