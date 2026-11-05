/**
 * /mcp の基盤の E2E(Worker 全体への HTTP): URL クエリ(read_only / toolsets)、kintone の読み取りリトライ、
 * エラーのエンベロープ、max_response_tokens。kintone の REST と OAuth は fetch のモック。
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type RecordedCall } from "../helpers/mock-fetch";
import {
  firstText,
  MCP_URL,
  mcpRequest,
  readRpc,
  runAuthorizationFlow,
  setupMocks,
  type TokenSet,
} from "../helpers/oauth-flow";

afterEach(() => vi.restoreAllMocks());

const isApps = (c: RecordedCall) => new URL(c.url).pathname === "/k/v1/apps.json";

/** 認可を済ませ、MCP のアクセストークンを返す。`kintoneApps` は認可時の疎通確認(apps.json?limit=1)にも使われる。 */
async function connect(kintoneApps?: (call: RecordedCall) => Response | Promise<Response>) {
  let afterFlow = false;
  const mocks = await setupMocks({
    ...(kintoneApps
      ? {
          kintoneApps: (call) =>
            afterFlow
              ? kintoneApps(call)
              : jsonResponse({ apps: [{ appId: "1", code: "CRM", name: "顧客管理" }] }),
        }
      : {}),
  });
  const flow = await runAuthorizationFlow({ accessKey: mocks.accessKey });
  const { access_token } = (await flow.tokenResponse.json()) as TokenSet;
  afterFlow = true;
  const appsCalls = () => mocks.calls.filter(isApps).length;
  const baseline = appsCalls();
  return { token: access_token, appsCallsSince: () => appsCalls() - baseline, ...mocks };
}

const callSearch = (token: string, args: Record<string, unknown> = {}, url = MCP_URL) =>
  mcpRequest(token, "tools/call", { name: "kintone_apps_search", arguments: args }, url);

describe("E2E: /mcp の URL クエリ(read_only / toolsets)", () => {
  const READ_TOOLS = ["kintone_apps_search", "kintone_app_describe", "kintone_records_query"];
  const WRITE_TOOLS = ["kintone_record_create", "kintone_record_update", "kintone_record_delete"];
  it.each([
    ["?read_only=true", READ_TOOLS],
    ["?toolsets=app", ["kintone_apps_search", "kintone_app_describe"]],
    ["?toolsets=record", ["kintone_records_query", ...WRITE_TOOLS]],
    ["?toolsets=record&read_only=true", ["kintone_records_query"]],
    ["?toolsets=all&read_only=false", [...READ_TOOLS, ...WRITE_TOOLS]],
    ["?x=1", [...READ_TOOLS, ...WRITE_TOOLS]],
  ])("%s で tools/list と tools/call が通る(M04 で公開ツールは 6 つ)", async (query, expected) => {
    const { token } = await connect();
    const list = await mcpRequest(token, "tools/list", {}, `${MCP_URL}${query}`);
    expect(list.status).toBe(200);
    expect(((await readRpc(list)).result?.tools ?? []).map((t) => t.name)).toEqual(expected);
    if (expected.includes("kintone_apps_search")) {
      const call = await callSearch(token, {}, `${MCP_URL}${query}`);
      expect(call.status).toBe(200);
      expect((await readRpc(call)).result?.isError).toBeFalsy();
    }
  });

  it.each([
    ["未知の toolset", "?toolsets=nope"],
    ["read_only の不正値", "?read_only=maybe"],
    ["綴り違いのキー", "?readonly=true"],
  ])(
    "%s は 400 invalid_query で、kintone を呼ばない(tools/list も tools/call も)",
    async (_n, query) => {
      const { token, appsCallsSince } = await connect();
      const list = await mcpRequest(token, "tools/list", {}, `${MCP_URL}${query}`);
      expect(list.status).toBe(400);
      const body = (await list.json()) as { error: string; error_description: string };
      expect(body.error).toBe("invalid_query");
      expect(body.error_description).toBeTruthy();
      const call = await callSearch(token, {}, `${MCP_URL}${query}`);
      expect(call.status).toBe(400);
      expect(appsCallsSince()).toBe(0);
    },
  );

  it("クエリが不正でも、トークンなしは先に 401(認証が先)", async () => {
    await setupMocks();
    const res = await mcpRequest(null, "tools/list", {}, `${MCP_URL}?toolsets=nope`);
    expect(res.status).toBe(401);
  });
});

describe("E2E: kintone の読み取りのリトライとエラーのエンベロープ", () => {
  it("429(Retry-After: 0)を 1 回返されても、リトライして成功する", async () => {
    let n = 0;
    const { token, appsCallsSince } = await connect(() =>
      ++n === 1
        ? new Response("{}", { status: 429, headers: { "Retry-After": "0" } })
        : jsonResponse({ apps: [{ appId: "1" }] }),
    );
    const res = await callSearch(token);
    const rpc = await readRpc(res);
    expect(rpc.result?.isError).toBeFalsy();
    expect(JSON.parse(firstText(rpc))).toEqual({ apps: [{ appId: "1" }] });
    expect(appsCallsSince()).toBe(2);
  });

  it("429 が続くと、3 回(リトライ 2 回)で打ち切って rate_limited(retryable)+ details を返す", async () => {
    const { token, appsCallsSince } = await connect(
      () =>
        new Response(JSON.stringify({ code: "GAIA_TM12", id: "i", message: "too many" }), {
          status: 429,
          headers: { "Retry-After": "0", "Content-Type": "application/json" },
        }),
    );
    const rpc = await readRpc(await callSearch(token));
    expect(rpc.result?.isError).toBe(true);
    expect(JSON.parse(firstText(rpc))).toEqual({
      schema_version: "1",
      error: {
        code: "rate_limited",
        message: expect.stringContaining("HTTP 429"),
        retryable: true,
        details: {
          http_status: 429,
          kintone_code: "GAIA_TM12",
          kintone_id: "i",
          retry_after_sec: 0,
        },
      },
    });
    expect(appsCallsSince()).toBe(3);
  });

  it("kintone の 404 は not_found(details に kintone のコード)。リトライしない", async () => {
    const { token, appsCallsSince } = await connect(() =>
      jsonResponse({ code: "GAIA_AP01", id: "x", message: "app not found" }, 404),
    );
    const body = JSON.parse(firstText(await readRpc(await callSearch(token))));
    expect(body.error).toMatchObject({
      code: "not_found",
      retryable: false,
      details: { http_status: 404, kintone_code: "GAIA_AP01" },
    });
    expect(appsCallsSince()).toBe(1);
  });

  it("200 なのに JSON でない応答は api_error(retryable)で、トークンを含めない", async () => {
    const { token } = await connect(() => new Response("<html>login</html>", { status: 200 }));
    const text = firstText(await readRpc(await callSearch(token)));
    const body = JSON.parse(text);
    expect(body.error).toMatchObject({
      code: "api_error",
      retryable: true,
      details: { http_status: 200 },
    });
    expect(text).not.toContain("kintone-at");
    expect(text).not.toContain("<html>");
  });
});

describe("E2E: max_response_tokens", () => {
  const MANY = {
    apps: Array.from({ length: 200 }, (_, i) => ({
      appId: String(i),
      code: `C${i}`,
      name: "顧客管理",
    })),
  };

  it("上限を超えたら切り詰めずに response_too_large。0 で無効にすると通る。既定では通る", async () => {
    const { token } = await connect(() => jsonResponse(MANY));
    const small = firstText(await readRpc(await callSearch(token, { max_response_tokens: 100 })));
    expect(JSON.parse(small).error).toMatchObject({
      code: "response_too_large",
      retryable: false,
    });
    expect(small).not.toContain("顧客管理");

    const unlimited = await readRpc(await callSearch(token, { max_response_tokens: 0 }));
    expect(unlimited.result?.isError).toBeFalsy();
    expect(JSON.parse(firstText(unlimited))).toEqual(MANY);

    const byDefault = await readRpc(await callSearch(token));
    expect(byDefault.result?.isError).toBeFalsy();
  });

  it("負の値は argument_error で、kintone を呼ばない", async () => {
    const { token, appsCallsSince } = await connect(() => jsonResponse(MANY));
    const body = JSON.parse(
      firstText(await readRpc(await callSearch(token, { max_response_tokens: -5 }))),
    );
    expect(body.error.code).toBe("argument_error");
    expect(appsCallsSince()).toBe(0);
  });
});
