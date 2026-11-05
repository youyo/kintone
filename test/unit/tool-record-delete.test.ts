/**
 * kintone_record_delete(仕様書 §5.6)。DELETE /k/v1/records.json(JSON 本文)、ids 1〜100、revisions の長さ、write モード。
 */
import { describe, expect, it } from "vitest";
import { type AppRow, listApps } from "../helpers/fake-kintone-data";
import { toolHarness, writeBody } from "../helpers/tool-harness";

const ROWS: AppRow[] = [
  { appId: "12", code: "CRM", name: "顧客管理" },
  { appId: "15", code: "SALES3", name: "売上報告" },
];
const TOOL = "kintone_record_delete";

function setup() {
  return toolHarness({
    get: async (path, query) => {
      if (path === "/k/v1/apps.json") return listApps(ROWS, query);
      throw new Error(`unexpected GET ${path}`);
    },
    write: () => ({}),
  });
}

describe("kintone_record_delete: 引数と本文", () => {
  it("app + ids: DELETE /k/v1/records.json の本文は {app, ids}(revisions なしならキーを出さない)。出力 {deleted}", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app: 12, ids: [1, 2, 3] });
    expect(r.isError).toBe(false);
    expect(h.calls).toEqual([]);
    expect(h.writes).toEqual([
      { method: "DELETE", path: "/k/v1/records.json", body: { app: 12, ids: [1, 2, 3] } },
    ]);
    expect(h.writes[0]?.body).not.toHaveProperty("revisions");
    expect(r.body).toEqual({ deleted: 3 });
  });

  it("revisions は ids と同じ長さなら本文に入る(-1 = 検証なしを含む)", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app: 12, ids: [1, 2], revisions: [5, -1] });
    expect(r.isError).toBe(false);
    expect(h.writes[0]?.body).toEqual({ app: 12, ids: [1, 2], revisions: [5, -1] });
  });

  it("ids は 100 件ちょうどが通る", async () => {
    const h = setup();
    const ids = Array.from({ length: 100 }, (_, i) => i + 1);
    const r = await h.call(TOOL, { app: 12, ids });
    expect(r.body).toEqual({ deleted: 100 });
  });

  it.each([
    ["ids が 101 件", { app: 12, ids: Array.from({ length: 101 }, (_, i) => i + 1) }],
    ["ids が 0 件", { app: 12, ids: [] }],
    ["ids がない", { app: 12 }],
    ["ids に 0", { app: 12, ids: [1, 0] }],
    ["ids に負", { app: 12, ids: [-1] }],
    ["ids に小数", { app: 12, ids: [1.5] }],
    ["ids に文字列", { app: 12, ids: ["1"] }],
    ["revisions の長さが短い", { app: 12, ids: [1, 2], revisions: [1] }],
    ["revisions の長さが長い", { app: 12, ids: [1], revisions: [1, 2] }],
    ["revisions が空配列(ids と長さが違う)", { app: 12, ids: [1], revisions: [] }],
    ["revisions に小数", { app: 12, ids: [1], revisions: [1.5] }],
    ["app も app_ref もない", { ids: [1] }],
    ["app と app_ref の両方", { app: 12, app_ref: "CRM", ids: [1] }],
    ["未知の引数", { app: 12, ids: [1], nope: 1 }],
  ])("%s は kintone を一度も呼ばずに argument_error", async (_name, args) => {
    const h = setup();
    const r = await h.call(TOOL, args);
    expect(r.isError).toBe(true);
    expect(r.body.error.code).toBe("argument_error");
    expect(h.calls).toEqual([]);
    expect(h.writes).toEqual([]);
  });

  it("maxApiCalls を式から決めて宣言している(解決の読み取り × 3 + 書き込み 1 = 34)", async () => {
    const h = setup();
    await h.call(TOOL, { app: 12, ids: [1] });
    expect(h.clientOptions).toEqual([{ maxApiCalls: 34 }]);
  });
});

describe("kintone_record_delete: app_ref は write モード(完全一致だけ)", () => {
  it("コード完全一致 → 削除する", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "CRM", ids: [9] });
    expect(r.body).toEqual({ deleted: 1 });
    expect(writeBody(h).app).toBe(12);
  });

  it("名前の部分一致のみ(1 件でも)なら resolver_app_exact_match_required。DELETE を呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "顧客", ids: [9] });
    expect(r.body.error).toMatchObject({
      code: "resolver_app_exact_match_required",
      details: {
        kind: "app",
        ref: "顧客",
        candidates: [{ id: "12", code: "CRM", name: "顧客管理" }],
      },
    });
    expect(h.writes).toEqual([]);
  });

  it("見つからなければ resolver_app_not_found。DELETE を呼ばない", async () => {
    const h = setup();
    const r = await h.call(TOOL, { app_ref: "存在しない", ids: [9] });
    expect(r.body.error.code).toBe("resolver_app_not_found");
    expect(h.writes).toEqual([]);
  });
});
