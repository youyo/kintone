/**
 * kintone_record_update(仕様書 §5.5)。PUT /k/v1/record.json、id / update_key の排他、
 * update_key_field_ref は write モード(コード・label の完全一致だけ)。
 */
import { describe, expect, it } from "vitest";
import { type AppRow, fieldsResponse, listApps } from "../helpers/fake-kintone-data";
import { toolHarness, writeBody } from "../helpers/tool-harness";

const ROWS: AppRow[] = [
  { appId: "12", code: "CRM", name: "顧客管理" },
  { appId: "15", code: "SALES3", name: "売上報告" },
];
const FIELDS = fieldsResponse([
  { type: "SINGLE_LINE_TEXT", code: "customer_code", label: "顧客コード" },
  { type: "SINGLE_LINE_TEXT", code: "customer_name", label: "顧客名" },
  { type: "NUMBER", code: "amount", label: "金額" },
  { type: "NUMBER", code: "tax_amount", label: "消費税額" },
  { type: "SINGLE_LINE_TEXT", code: "dup1", label: "同名" },
  { type: "SINGLE_LINE_TEXT", code: "dup2", label: "同名" },
]);
const TOOL = "kintone_record_update";
const REC = { amount: { value: "200" } };

function setup(options: { fieldsGets?: string[] } = {}) {
  return toolHarness({
    get: async (path, query) => {
      if (path === "/k/v1/apps.json") return listApps(ROWS, query);
      if (path === "/k/v1/app/form/fields.json") {
        options.fieldsGets?.push(String(query?.app));
        return FIELDS;
      }
      throw new Error(`unexpected GET ${path}`);
    },
    write: () => ({ revision: "7" }),
  });
}

describe("kintone_record_update: 引数と本文", () => {
  it("id 指定: PUT /k/v1/record.json に {app, id, record}。出力 {revision} は数値", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app: 12, id: 5, record: REC });
    expect(r.isError).toBe(false);
    expect(h.calls).toEqual([]);
    expect(h.writes).toEqual([
      { method: "PUT", path: "/k/v1/record.json", body: { app: 12, id: 5, record: REC } },
    ]);
    expect(r.body).toEqual({ revision: 7 });
    expect(typeof r.body.revision).toBe("number");
  });

  it("revision(-1 を含む整数)を本文に入れる。指定しなければキーを出さない", async () => {
    const h = setup();
    await h.call(TOOL, { app: 12, id: 5, record: REC, revision: 3 });
    await h.call(TOOL, { app: 12, id: 5, record: REC, revision: -1 });
    await h.call(TOOL, { app: 12, id: 5, record: REC });
    expect(h.writes.map((w) => (w.body as { revision?: number }).revision)).toEqual([
      3,
      -1,
      undefined,
    ]);
    expect(h.writes[2]?.body).not.toHaveProperty("revision");
  });

  it("update_key_field + update_key_value: body は updateKey:{field, value}(id は入れない)", async () => {
    const h = setup();
    const r = await h.call(TOOL, {
      app: 12,
      update_key_field: "customer_code",
      update_key_value: "C-001",
      record: REC,
    });
    expect(r.isError).toBe(false);
    expect(h.calls).toEqual([]);
    expect(h.writes[0]?.body).toEqual({
      app: 12,
      updateKey: { field: "customer_code", value: "C-001" },
      record: REC,
    });
  });

  it.each([
    ["id も update_key もない", { app: 12, record: REC }],
    [
      "id と update_key_field の両方",
      { app: 12, id: 5, update_key_field: "a", update_key_value: "1", record: REC },
    ],
    [
      "id と update_key_field_ref の両方",
      { app: 12, id: 5, update_key_field_ref: "顧客コード", update_key_value: "1", record: REC },
    ],
    ["id と update_key_value の両方", { app: 12, id: 5, update_key_value: "1", record: REC }],
    [
      "update_key_field と update_key_field_ref の両方",
      {
        app: 12,
        update_key_field: "a",
        update_key_field_ref: "b",
        update_key_value: "1",
        record: REC,
      },
    ],
    ["update_key_value だけ(フィールドなし)", { app: 12, update_key_value: "1", record: REC }],
    ["update_key_field だけ(値なし)", { app: 12, update_key_field: "a", record: REC }],
    [
      "update_key_field_ref だけ(値なし)",
      { app: 12, update_key_field_ref: "顧客コード", record: REC },
    ],
    ["record がない", { app: 12, id: 5 }],
    ["record が空オブジェクト", { app: 12, id: 5, record: {} }],
    ["id が 0", { app: 12, id: 0, record: REC }],
    ["id が小数", { app: 12, id: 1.5, record: REC }],
    ["revision が小数", { app: 12, id: 5, record: REC, revision: 1.5 }],
    ["revision が -2", { app: 12, id: 5, record: REC, revision: -2 }],
    ["app も app_ref もない", { id: 5, record: REC }],
    ["app と app_ref の両方", { app: 12, app_ref: "CRM", id: 5, record: REC }],
    ["未知の引数", { app: 12, id: 5, record: REC, nope: 1 }],
  ])("%s は kintone を一度も呼ばずに argument_error", async (_name, args) => {
    const h = setup();
    const r = await h.call(TOOL, args);
    expect(r.isError).toBe(true);
    expect(r.body.error.code).toBe("argument_error");
    expect(h.calls).toEqual([]);
    expect(h.writes).toEqual([]);
  });

  it("応答の revision が数として読めなければ retryable でない api_error", async () => {
    const h = toolHarness({ write: () => ({ revision: "x" }) });
    const r = await h.call(TOOL, { app: 12, id: 5, record: REC });
    expect(r.body.error.code).toBe("api_error");
    expect(r.body.error.retryable).toBe(false);
  });

  it("maxApiCalls を式から決めて宣言している((A + F) × 3 + 書き込み 1 = 37)", async () => {
    const h = setup();
    await h.call(TOOL, { app: 12, id: 5, record: REC });
    expect(h.clientOptions).toEqual([{ maxApiCalls: 37 }]);
  });
});

describe("kintone_record_update: app_ref は write モード", () => {
  it("コード完全一致で採用し、PUT する", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "CRM", id: 5, record: REC });
    expect(r.isError).toBe(false);
    expect(writeBody(h).app).toBe(12);
  });

  it("部分一致のみなら resolver_app_exact_match_required。PUT を呼ばない(fields.json も呼ばない)", async () => {
    const h = setup();
    const r = await h.call(TOOL, {
      app_ref: "売上",
      update_key_field_ref: "顧客コード",
      update_key_value: "C-001",
      record: REC,
    });
    expect(r.body.error).toMatchObject({
      code: "resolver_app_exact_match_required",
      details: {
        kind: "app",
        ref: "売上",
        candidates: [{ id: "15", code: "SALES3", name: "売上報告" }],
      },
    });
    expect(h.writes).toEqual([]);
    expect(h.calls.every((c) => c.path === "/k/v1/apps.json")).toBe(true);
  });
});

describe("kintone_record_update: update_key_field_ref は write モード(完全一致だけ)", () => {
  const args = (ref: string) => ({
    app: 12,
    update_key_field_ref: ref,
    update_key_value: "C-001",
    record: REC,
  });

  it("フィールドコードの完全一致: fields.json を 1 回呼び、updateKey.field はコード", async () => {
    const gets: string[] = [];
    const h = setup({ fieldsGets: gets });
    const r = await h.call(TOOL, args("customer_code"));
    expect(r.isError).toBe(false);
    expect(gets).toEqual(["12"]);
    expect(h.writes[0]?.body).toEqual({
      app: 12,
      updateKey: { field: "customer_code", value: "C-001" },
      record: REC,
    });
  });

  it("label の完全一致: updateKey.field は label ではなくコード", async () => {
    const h = setup();
    const r = await h.call(TOOL, args("顧客コード"));
    expect(r.isError).toBe(false);
    expect(writeBody(h).updateKey.field).toBe("customer_code");
  });

  it("label の部分一致が 1 件だけでも採用せず resolver_field_exact_match_required。PUT を呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, args("消費税"));
    expect(r.body.error).toMatchObject({
      code: "resolver_field_exact_match_required",
      retryable: false,
      details: {
        kind: "field",
        ref: "消費税",
        candidates: [{ code: "tax_amount", label: "消費税額" }],
      },
    });
    expect(h.writes).toEqual([]);
  });

  it("label の部分一致が複数件でも resolver_field_exact_match_required(コード順)。PUT を呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, args("顧客"));
    expect(r.body.error.code).toBe("resolver_field_exact_match_required");
    expect(r.body.error.details.candidates).toEqual([
      { code: "customer_code", label: "顧客コード" },
      { code: "customer_name", label: "顧客名" },
    ]);
    expect(h.writes).toEqual([]);
  });

  it("label の完全一致が 2 件なら resolver_field_ambiguous。PUT を呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, args("同名"));
    expect(r.body.error.code).toBe("resolver_field_ambiguous");
    expect(h.writes).toEqual([]);
  });

  it("どれにも当たらなければ resolver_field_not_found。PUT を呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, args("存在しない"));
    expect(r.body.error.code).toBe("resolver_field_not_found");
    expect(h.writes).toEqual([]);
  });

  it("app_ref + update_key_field_ref: 解決の GET(apps.json → fields.json)のあとに PUT", async () => {
    const h = setup();
    const r = await h.call(TOOL, {
      app_ref: "顧客管理",
      update_key_field_ref: "顧客コード",
      update_key_value: "C-001",
      record: REC,
    });
    expect(r.isError).toBe(false);
    expect(h.calls.map((c) => c.path)).toEqual([
      "/k/v1/apps.json",
      "/k/v1/apps.json",
      "/k/v1/app/form/fields.json",
    ]);
    expect(h.writes).toHaveLength(1);
  });
});
