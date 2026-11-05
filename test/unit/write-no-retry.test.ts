/**
 * 書き込みはリトライしない(M02 のクライアントの保証を、ツール経由でも固定する。ADR 0008)。
 * 実クライアント + fetch のモック。POST / PUT / DELETE が 429・503・5xx・通信失敗を返しても、書き込みは 1 回だけ。
 * 名前解決の GET は読み取りなので 429 でリトライされる(書き込みの前段のリトライは許す)。
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { createKintoneClient } from "../../src/kintone/client";
import { createMcpServer } from "../../src/mcp/server";
import { type AppRow, listApps, queryFromSearchParams } from "../helpers/fake-kintone-data";
import { rpcTo } from "../helpers/mcp";
import { jsonResponse, mockOutboundFetch, type RecordedCall } from "../helpers/mock-fetch";
import { firstText } from "../helpers/oauth-flow";
import type { ToolBody } from "../helpers/tool-harness";

afterEach(() => vi.restoreAllMocks());

const ORIGIN = "https://example.cybozu.com";
const ROWS: AppRow[] = [{ appId: "12", code: "CRM", name: "顧客管理" }];
const REC = { customer: { value: "山田" } };

const WRITES = [
  {
    tool: "kintone_record_create",
    key: `POST ${ORIGIN}/k/v1/records.json`,
    args: { app: 12, record: REC },
    ok: { ids: ["1"], revisions: ["1"] },
  },
  {
    tool: "kintone_record_update",
    key: `PUT ${ORIGIN}/k/v1/record.json`,
    args: { app: 12, id: 1, record: REC },
    ok: { revision: "2" },
  },
  {
    tool: "kintone_record_delete",
    key: `DELETE ${ORIGIN}/k/v1/records.json`,
    args: { app: 12, ids: [1] },
    ok: {},
  },
] as const;

async function callTool(name: string, args: Record<string, unknown>) {
  const rpc = await rpcTo(
    () =>
      createMcpServer({
        getClient: ({ maxApiCalls }) =>
          createKintoneClient({
            host: "example.cybozu.com",
            accessToken: "[REDACTED]",
            // リトライの待機を実際には待たない(読み取りのリトライの確認用)。
            sleep: async () => {},
            ...(maxApiCalls === undefined ? {} : { maxApiCalls }),
          }),
        onKintoneUnauthorized: () => {},
      }),
    "tools/call",
    { name, arguments: args },
  );
  return JSON.parse(firstText(rpc)) as ToolBody;
}

describe("書き込みは 1 回だけ(リトライしない)", () => {
  it.each(WRITES)("$tool: 成功なら書き込みは 1 回", async ({ tool, key, args, ok }) => {
    const m = mockOutboundFetch({ [key]: () => jsonResponse(ok) });
    const body = await callTool(tool, args);
    expect(body.error).toBeUndefined();
    expect(m.callsTo(key)).toHaveLength(1);
  });

  const failures: [string, number, string, boolean][] = [
    ["429", 429, "rate_limited", true],
    ["503", 503, "api_error", true],
    ["500", 500, "api_error", true],
  ];
  for (const w of WRITES) {
    it.each(failures)(
      `${w.tool}: %s でもリトライせず 1 回で打ち切る`,
      async (_n, status, code, retryable) => {
        const m = mockOutboundFetch({
          [w.key]: () =>
            new Response(JSON.stringify({ code: "GAIA_X", id: "i", message: "m" }), {
              status,
              headers: { "Retry-After": "0", "Content-Type": "application/json" },
            }),
        });
        const body = await callTool(w.tool, w.args);
        expect(body.error.code).toBe(code);
        expect(body.error.retryable).toBe(retryable);
        expect(m.callsTo(w.key)).toHaveLength(1);
      },
    );

    it(`${w.tool}: 通信失敗・本文が JSON でない 200 でも 1 回だけ`, async () => {
      const m = mockOutboundFetch({
        [w.key]: () => {
          throw new TypeError("network down");
        },
      });
      const failed = await callTool(w.tool, w.args);
      expect(failed.error.code).toBe("api_error");
      expect(m.callsTo(w.key)).toHaveLength(1);
      m.spy.mockRestore();

      const m2 = mockOutboundFetch({ [w.key]: () => new Response("<html>", { status: 200 }) });
      const broken = await callTool(w.tool, w.args);
      expect(broken.error.code).toBe("api_error");
      expect(m2.callsTo(w.key)).toHaveLength(1);
    });
  }

  it("名前解決の GET(読み取り)は 429 でリトライされ、そのあとの書き込みは 1 回だけ", async () => {
    let apps = 0;
    const m = mockOutboundFetch({
      [`GET ${ORIGIN}/k/v1/apps.json`]: (call: RecordedCall) => {
        apps++;
        return apps <= 2
          ? new Response("{}", { status: 429, headers: { "Retry-After": "0" } })
          : jsonResponse(listApps(ROWS, queryFromSearchParams(new URL(call.url).searchParams)));
      },
      [`DELETE ${ORIGIN}/k/v1/records.json`]: () => jsonResponse({}),
    });
    const body = await callTool("kintone_record_delete", { app_ref: "CRM", ids: [1] });
    expect(body).toEqual({ deleted: 1 });
    expect(m.callsTo(`GET ${ORIGIN}/k/v1/apps.json`)).toHaveLength(3);
    expect(m.callsTo(`DELETE ${ORIGIN}/k/v1/records.json`)).toHaveLength(1);
  });

  it("書き込みの本文は JSON(Content-Type: application/json)で、メソッドと宛先が仕様どおり", async () => {
    const m = mockOutboundFetch({
      [`POST ${ORIGIN}/k/v1/records.json`]: () => jsonResponse({ ids: ["1"], revisions: ["1"] }),
      [`PUT ${ORIGIN}/k/v1/record.json`]: () => jsonResponse({ revision: "2" }),
      [`DELETE ${ORIGIN}/k/v1/records.json`]: () => jsonResponse({}),
    });
    for (const w of WRITES) await callTool(w.tool, w.args);
    expect(m.calls.map((c) => [c.method, new URL(c.url).pathname])).toEqual([
      ["POST", "/k/v1/records.json"],
      ["PUT", "/k/v1/record.json"],
      ["DELETE", "/k/v1/records.json"],
    ]);
    for (const c of m.calls) {
      expect(c.headers.get("content-type")).toBe("application/json");
      expect(c.redirect).toBe("manual");
    }
    expect(JSON.parse(m.calls[0]?.body ?? "")).toEqual({ app: 12, records: [REC] });
    expect(JSON.parse(m.calls[1]?.body ?? "")).toEqual({ app: 12, id: 1, record: REC });
    expect(JSON.parse(m.calls[2]?.body ?? "")).toEqual({ app: 12, ids: [1] });
  });
});
