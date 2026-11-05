/**
 * kintone_record_create(仕様書 §5.4)。POST /k/v1/records.json、record / records の排他、1〜100 件、
 * 結果の数値化、write モード(完全一致だけ。部分一致のみなら書き込みを呼ばない)。
 */
import { describe, expect, it } from "vitest";
import { KintoneApiError } from "../../src/kintone/client";
import { recordCreate } from "../../src/mcp/tools/record-create";
import { type AppRow, listApps } from "../helpers/fake-kintone-data";
import { toolHarness, writeBody } from "../helpers/tool-harness";

const ROWS: AppRow[] = [
  { appId: "12", code: "CRM", name: "顧客管理" },
  { appId: "13", code: "TASK", name: "タスク" },
  { appId: "14", code: "SALES2", name: "売上" },
  { appId: "15", code: "SALES3", name: "売上報告" },
  { appId: "16", code: "DUP-A", name: "重複" },
  { appId: "17", code: "DUP-B", name: "重複" },
];
const TOOL = "kintone_record_create";
const REC = { customer: { value: "山田" }, amount: { value: "100" } };

function setup(write?: (body: unknown) => unknown) {
  return toolHarness({
    get: async (path, query) => {
      if (path === "/k/v1/apps.json") return listApps(ROWS, query);
      throw new Error(`unexpected GET ${path}`);
    },
    write: (_method, _path, body) =>
      write ? write(body) : { ids: ["101", "102"], revisions: ["1", "1"] },
  });
}

describe("kintone_record_create: 引数と本文", () => {
  it("app + record: POST /k/v1/records.json に {app, records:[record]}。解決の API は呼ばない", async () => {
    const h = setup(() => ({ ids: ["101"], revisions: ["1"] }));
    const r = await h.call(TOOL, { app: 12, record: REC });
    expect(r.isError).toBe(false);
    expect(h.calls).toEqual([]);
    expect(h.writes).toEqual([
      { method: "POST", path: "/k/v1/records.json", body: { app: 12, records: [REC] } },
    ]);
    expect(r.body).toEqual({ ids: [101], revisions: [1] });
  });

  it("records(複数件)はそのまま送り、ids / revisions を数値にする", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app: 12, records: [REC, { customer: { value: "佐藤" } }] });
    expect(h.writes[0]?.body).toEqual({
      app: 12,
      records: [REC, { customer: { value: "佐藤" } }],
    });
    expect(r.body).toEqual({ ids: [101, 102], revisions: [1, 1] });
    expect(r.body.ids.every((n: unknown) => typeof n === "number")).toBe(true);
    expect(r.body.revisions.every((n: unknown) => typeof n === "number")).toBe(true);
  });

  it("records は 100 件ちょうどが通る", async () => {
    const h = setup();
    const records = Array.from({ length: 100 }, (_, i) => ({ n: { value: String(i) } }));
    const r = await h.call(TOOL, { app: 12, records });
    expect(r.isError).toBe(false);
    expect(writeBody(h).records).toHaveLength(100);
  });

  it.each([
    ["records が 101 件", { app: 12, records: Array.from({ length: 101 }, () => ({})) }],
    ["records が 0 件", { app: 12, records: [] }],
    ["record と records の両方", { app: 12, record: REC, records: [REC] }],
    ["record も records もない", { app: 12 }],
    ["app も app_ref もない", { record: REC }],
    ["app と app_ref の両方", { app: 12, app_ref: "CRM", record: REC }],
    ["app が 0", { app: 0, record: REC }],
    ["app_ref が空文字", { app_ref: "", record: REC }],
    ["record が配列", { app: 12, record: [REC] }],
    ["records の要素が object でない", { app: 12, records: ["x"] }],
    ["未知の引数", { app: 12, record: REC, nope: 1 }],
    [
      "max_response_tokens(書き込みには付けない)",
      { app: 12, record: REC, max_response_tokens: 10 },
    ],
  ])("%s は kintone を一度も呼ばずに argument_error", async (_name, args) => {
    const h = setup();
    const r = await h.call(TOOL, args);
    expect(r.isError).toBe(true);
    expect(r.body.error.code).toBe("argument_error");
    expect(r.body.error.retryable).toBe(false);
    expect(h.calls).toEqual([]);
    expect(h.writes).toEqual([]);
  });
});

describe("kintone_record_create: app_ref は write モード(完全一致だけ)", () => {
  it("コード完全一致: apps.json 1 回 → POST", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "CRM", record: REC });
    expect(r.isError).toBe(false);
    expect(h.calls.map((c) => c.path)).toEqual(["/k/v1/apps.json"]);
    expect(writeBody(h).app).toBe(12);
  });

  it("名前の完全一致で採用される(部分一致の別アプリがあっても)", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "売上", record: REC });
    expect(r.isError).toBe(false);
    expect(writeBody(h).app).toBe(14);
  });

  it("数字の app_ref は ID としてそのまま使う(解決の API を呼ばない)", async () => {
    const h = setup();
    await h.call(TOOL, { app_ref: "12", record: REC });
    expect(h.calls).toEqual([]);
    expect(writeBody(h).app).toBe(12);
  });

  it("名前の部分一致が 1 件だけでも採用せず resolver_app_exact_match_required。書き込みを呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "顧客", record: REC });
    expect(r.isError).toBe(true);
    expect(r.body.error).toMatchObject({
      code: "resolver_app_exact_match_required",
      retryable: false,
      details: {
        kind: "app",
        ref: "顧客",
        candidates: [{ id: "12", code: "CRM", name: "顧客管理" }],
      },
    });
    expect(h.writes).toEqual([]);
  });

  it("部分一致が複数件でも resolver_app_exact_match_required(candidates は全部)。書き込みを呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "売", record: REC });
    expect(r.body.error.code).toBe("resolver_app_exact_match_required");
    expect(r.body.error.details.candidates).toEqual([
      { id: "14", code: "SALES2", name: "売上" },
      { id: "15", code: "SALES3", name: "売上報告" },
    ]);
    expect(h.writes).toEqual([]);
  });

  it("名前の完全一致が 2 件なら resolver_app_ambiguous。書き込みを呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "重複", record: REC });
    expect(r.body.error.code).toBe("resolver_app_ambiguous");
    expect(h.writes).toEqual([]);
  });

  it("見つからなければ resolver_app_not_found。書き込みを呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "存在しない", record: REC });
    expect(r.body.error.code).toBe("resolver_app_not_found");
    expect(h.writes).toEqual([]);
  });
});

describe("kintone_record_create: 結果とエラー", () => {
  it("ids が数として読めなければ、書き込みは済んでいるので retryable でない api_error", async () => {
    const h = setup(() => ({ ids: ["abc"], revisions: ["1"] }));
    const r = await h.call(TOOL, { app: 12, record: REC });
    expect(r.isError).toBe(true);
    expect(r.body.error.code).toBe("api_error");
    expect(r.body.error.retryable).toBe(false);
    expect(r.body.error.message).toMatch(/may have been|already/i);
  });

  it("応答に ids がなくても retryable でない api_error", async () => {
    const h = setup(() => ({}));
    const r = await h.call(TOOL, { app: 12, record: REC });
    expect(r.body.error.code).toBe("api_error");
    expect(r.body.error.retryable).toBe(false);
  });

  it("kintone が 400 + CB_VA01 なら argument_error(details に kintone のコード)", async () => {
    const h = setup(() => {
      throw new KintoneApiError(400, { kintoneCode: "CB_VA01", kintoneMessage: "bad field" });
    });
    const r = await h.call(TOOL, { app: 12, record: REC });
    expect(r.body.error).toMatchObject({
      code: "argument_error",
      retryable: false,
      details: { http_status: 400, kintone_code: "CB_VA01" },
    });
  });

  it("kintone が 403 なら permission_error", async () => {
    const h = setup(() => {
      throw new KintoneApiError(403, { kintoneCode: "CB_NO02" });
    });
    const r = await h.call(TOOL, { app: 12, record: REC });
    expect(r.body.error.code).toBe("permission_error");
  });

  it("maxApiCalls を式から決めて宣言している(解決の読み取り × 3 + 書き込み 1 = 34)", async () => {
    const h = setup();
    await h.call(TOOL, { app: 12, record: REC });
    expect(h.clientOptions).toEqual([{ maxApiCalls: 34 }]);
  });
});

describe("kintone_record_create: description(暗黙の再送判定を AI に伝える)", () => {
  const description = recordCreate.description;

  it("明示の冪等キーがないと、5 分以内の同じ引数の呼び出しは再送とみなして前回の結果を返す(2 件目は作られない)", () => {
    expect(description).toMatch(/without an explicit idempotency key/i);
    expect(description).toMatch(/same arguments within 5 minutes/i);
    expect(description).toMatch(/re-?send/i);
    expect(description).toMatch(/returns the previous result/i);
    expect(description).toMatch(/second record is NOT created/i);
  });

  it("意図して同じ内容を 2 件作る方法(別の idempotency_key。_meta.idempotency_key でもよい)を書く", () => {
    expect(description).toMatch(/identical records on purpose/i);
    expect(description).toContain("idempotency_key");
    expect(description).toContain("_meta.idempotency_key");
    expect(description).toMatch(/different idempotency_key/i);
  });

  it("書き込みがリトライされないことと、通信失敗・タイムアウトのあとは結果が分からないことを書く", () => {
    expect(description).toMatch(/not retried/i);
    expect(description).toMatch(/timeout|network failure/i);
  });
});
