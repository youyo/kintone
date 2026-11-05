/**
 * M03 の読み取りツール 3 つ(kintone_apps_search / kintone_app_describe / kintone_records_query)を、
 * Worker 全体への HTTP(/register → /authorize → /callback → /token → /mcp)で通す E2E。
 * kintone の REST と OAuth、Access の JWKS は fetch のモック(実通信なし)。
 */
import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import { type AppRow, fieldsResponse, listApps, manyApps, queryFromSearchParams } from "../helpers/fake-kintone-data";
import { jsonResponse, type RecordedCall } from "../helpers/mock-fetch";
import {
  firstText,
  KINTONE_ORIGIN,
  MCP_URL,
  mcpRequest,
  readRpc,
  runAuthorizationFlow,
  setupMocks,
  type TokenSet,
} from "../helpers/oauth-flow";

afterEach(() => vi.restoreAllMocks());

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
const FIELDS = fieldsResponse([
  { type: "SINGLE_LINE_TEXT", code: "customer", label: "顧客名" },
  { type: "NUMBER", code: "amount", label: "金額" },
]);
const RECORDS = [{ $id: { type: "__ID__", value: "1" }, customer: { type: "SINGLE_LINE_TEXT", value: "山田" } }];

const path = (c: RecordedCall) => new URL(c.url).pathname;
const params = (c: RecordedCall) => new URL(c.url).searchParams;

interface Options {
  apps?: AppRow[];
}

/** 認可を済ませた状態の kintone のモック。認可時の疎通確認(apps.json?limit=1)には通常の応答を返す。 */
async function setup(options: Options = {}) {
  const state = { apps: options.apps ?? ROWS, afterFlow: false, recordsNotFound: false };
  const apiCalls: RecordedCall[] = [];
  const track = (c: RecordedCall) => {
    if (state.afterFlow) apiCalls.push(c);
  };
  const mocks = await setupMocks({
    kintoneApps: (call) => {
      track(call);
      return jsonResponse(listApps(state.apps, queryFromSearchParams(params(call))));
    },
    extra: {
      [`GET ${KINTONE_ORIGIN}/k/v1/app.json`]: (call) => {
        track(call);
        return jsonResponse(APP_JSON);
      },
      [`GET ${KINTONE_ORIGIN}/k/v1/app/form/fields.json`]: (call) => {
        track(call);
        return jsonResponse(FIELDS);
      },
      [`GET ${KINTONE_ORIGIN}/k/v1/records.json`]: (call) => {
        track(call);
        if (state.recordsNotFound) {
          return jsonResponse({ code: "GAIA_AP01", id: "x", message: "app not found" }, 404);
        }
        const total = params(call).get("totalCount") === "true";
        return jsonResponse({ records: RECORDS, ...(total ? { totalCount: "42" } : {}) });
      },
    },
  });
  const connect = async (email?: string) => {
    const flow = await runAuthorizationFlow({ accessKey: mocks.accessKey, ...(email ? { email } : {}) });
    const { access_token } = (await flow.tokenResponse.json()) as TokenSet;
    state.afterFlow = true;
    return access_token;
  };
  const callTool = async (
    token: string,
    name: string,
    args: Record<string, unknown> = {},
    url = MCP_URL,
  ) => {
    const res = await mcpRequest(token, "tools/call", { name, arguments: args }, url);
    expect(res.status).toBe(200);
    const rpc = await readRpc(res);
    const text = firstText(rpc);
    return { rpc, isError: rpc.result?.isError === true, text, body: JSON.parse(text) as Record<string, any> };
  };
  return { ...mocks, state, apiCalls, connect, callTool };
}

describe("E2E: tools/list", () => {
  it("読み取りツール 3 つが見える。?toolsets=app は 2 つ、?toolsets=record は 1 つ", async () => {
    const { connect } = await setup();
    const token = await connect();
    const names = async (query = "") =>
      ((await readRpc(await mcpRequest(token, "tools/list", {}, `${MCP_URL}${query}`))).result?.tools ?? []).map(
        (t) => t.name,
      );
    expect(await names()).toEqual(["kintone_apps_search", "kintone_app_describe", "kintone_records_query"]);
    expect(await names("?toolsets=app")).toEqual(["kintone_apps_search", "kintone_app_describe"]);
    expect(await names("?toolsets=record")).toEqual(["kintone_records_query"]);
    expect(await names("?read_only=true")).toHaveLength(3);
  });

  it("max_response_tokens の公開スキーマに minimum: 0 が出る", async () => {
    const { connect } = await setup();
    const token = await connect();
    const tools = (await readRpc(await mcpRequest(token, "tools/list"))).result?.tools ?? [];
    for (const t of tools) {
      const props = (t.inputSchema as { properties: Record<string, { minimum?: number }> }).properties;
      expect(props.max_response_tokens?.minimum, t.name).toBe(0);
    }
  });
});

describe("E2E: kintone_apps_search", () => {
  it("ids / codes / name / space_ids / limit / offset を kintone のクエリにして、生の応答を返す", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_apps_search", {
      ids: [12, 13],
      codes: ["CRM"],
      name: "顧客",
      space_ids: [5],
      limit: 10,
      offset: 20,
    });
    expect(r.isError).toBe(false);
    expect(apiCalls).toHaveLength(1);
    const p = params(apiCalls[0] as RecordedCall);
    expect(Object.fromEntries(p)).toEqual({
      "ids[0]": "12",
      "ids[1]": "13",
      "codes[0]": "CRM",
      name: "顧客",
      "spaceIds[0]": "5",
      limit: "10",
      offset: "20",
    });
    expect((apiCalls[0] as RecordedCall).headers.get("authorization")).toMatch(/^Bearer .+/);
    expect(r.body).toEqual({ apps: [] }); // spaceId=5 のアプリはモックのデータにない
  });

  it("絞り込みなし: アプリ一覧(生データ)をそのまま返し、キャッシュ(KV)を作らない", async () => {
    const { connect, callTool } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_apps_search");
    expect(r.body.apps.map((a: { code: string }) => a.code)).toEqual(["CRM", "TASK"]);
    const [userId, grantId] = token.split(":");
    expect((await env.OAUTH_KV.list({ prefix: `kcache:v1:${userId}:${grantId}:` })).keys).toHaveLength(0);
  });

  it("範囲外の引数は argument_error で kintone を呼ばない", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_apps_search", { offset: -1 });
    expect(r.body.error.code).toBe("argument_error");
    expect(apiCalls).toHaveLength(0);
  });
});

describe("E2E: kintone_app_describe", () => {
  it("app 指定: app.json → form/fields.json。出力は {app, fields, revision}", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_app_describe", { app: 12, lang: "ja" });
    expect(r.isError).toBe(false);
    expect(apiCalls.map(path)).toEqual(["/k/v1/app.json", "/k/v1/app/form/fields.json"]);
    expect(params(apiCalls[0] as RecordedCall).get("id")).toBe("12");
    expect(params(apiCalls[1] as RecordedCall).get("app")).toBe("12");
    expect(params(apiCalls[1] as RecordedCall).get("lang")).toBe("ja");
    expect(r.body).toEqual({
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
      revision: "5",
    });
  });

  it("app_ref(コード・名前)で解決でき、app と app_ref の排他違反は argument_error", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const byCode = await callTool(token, "kintone_app_describe", { app_ref: "CRM" });
    expect(byCode.body.app.app_id).toBe("12");
    const byName = await callTool(token, "kintone_app_describe", { app_ref: "顧客管理" });
    expect(byName.body.app.app_id).toBe("12");
    const before = apiCalls.length;
    for (const args of [{ app: 12, app_ref: "CRM" }, {}]) {
      const r = await callTool(token, "kintone_app_describe", args);
      expect(r.body.error.code).toBe("argument_error");
    }
    expect(apiCalls).toHaveLength(before);
  });

  it("フィールドのキャッシュが grant 単位のキー・TTL 600 秒で KV にできる。常に kintone から取る", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const [userId, grantId] = token.split(":");
    await callTool(token, "kintone_app_describe", { app: 12 });
    const listed = await env.OAUTH_KV.list({ prefix: `kcache:v1:${userId}:${grantId}:` });
    expect(listed.keys.map((k) => k.name)).toEqual([`kcache:v1:${userId}:${grantId}:fields:12`]);
    const exp = listed.keys[0]?.expiration ?? 0;
    const now = Math.floor(Date.now() / 1000);
    expect(exp).toBeGreaterThanOrEqual(now + 540);
    expect(exp).toBeLessThanOrEqual(now + 660);
    expect(JSON.parse((await env.OAUTH_KV.get(listed.keys[0]?.name ?? "")) ?? "null")).toEqual([
      { code: "customer", label: "顧客名", type: "SINGLE_LINE_TEXT" },
      { code: "amount", label: "金額", type: "NUMBER" },
    ]);
    // 2 回目も kintone を呼ぶ(キャッシュを読まない)。
    apiCalls.length = 0;
    await callTool(token, "kintone_app_describe", { app: 12 });
    expect(apiCalls.map(path)).toEqual(["/k/v1/app.json", "/k/v1/app/form/fields.json"]);
  });
});

describe("E2E: kintone_records_query", () => {
  it("app 指定: records.json を 1 回。query / fields / total_count が kintone のクエリになり、total_count は数値", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_records_query", {
      app: 12,
      query: 'customer = "山田" limit 5',
      fields: ["$id", "customer"],
      total_count: true,
    });
    expect(r.isError).toBe(false);
    expect(apiCalls).toHaveLength(1);
    expect(Object.fromEntries(params(apiCalls[0] as RecordedCall))).toEqual({
      app: "12",
      query: 'customer = "山田" limit 5',
      "fields[0]": "$id",
      "fields[1]": "customer",
      totalCount: "true",
    });
    expect(r.body).toEqual({ records: RECORDS, total_count: 42 });
  });

  it("total_count を指定しなければ出力に total_count はない", async () => {
    const { connect, callTool } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_records_query", { app: 12 });
    expect(r.body).toEqual({ records: RECORDS });
  });

  it("app_ref で解決 → records.json。2 回目はキャッシュで apps.json を呼ばない", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const first = await callTool(token, "kintone_records_query", { app_ref: "顧客管理" });
    expect(first.isError).toBe(false);
    expect(apiCalls.map(path)).toEqual(["/k/v1/apps.json", "/k/v1/apps.json", "/k/v1/records.json"]);
    apiCalls.length = 0;
    const second = await callTool(token, "kintone_records_query", { app_ref: "顧客管理" });
    expect(second.isError).toBe(false);
    expect(apiCalls.map(path)).toEqual(["/k/v1/records.json"]);
    expect(params(apiCalls[0] as RecordedCall).get("app")).toBe("12");
  });

  it("キャッシュのキーは <userId>:<grantId> を含み、TTL は 600 秒。別の利用者(別 grant)とは共有しない", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const alice = await connect("alice@example.com");
    const bob = await connect("bob@example.com");
    await callTool(alice, "kintone_records_query", { app_ref: "CRM" });
    const [aliceUser, aliceGrant] = alice.split(":");
    const [bobUser, bobGrant] = bob.split(":");
    expect(aliceUser).not.toBe(bobUser);
    const aliceKeys = await env.OAUTH_KV.list({ prefix: `kcache:v1:${aliceUser}:${aliceGrant}:` });
    expect(aliceKeys.keys.length).toBeGreaterThan(0);
    for (const k of aliceKeys.keys) {
      const exp = k.expiration ?? 0;
      const now = Math.floor(Date.now() / 1000);
      expect(exp).toBeGreaterThanOrEqual(now + 540);
      expect(exp).toBeLessThanOrEqual(now + 660);
    }
    // bob は alice のキャッシュを使えず、自分で apps.json を呼ぶ。
    apiCalls.length = 0;
    await callTool(bob, "kintone_records_query", { app_ref: "CRM" });
    expect(apiCalls.map(path)).toEqual(["/k/v1/apps.json", "/k/v1/records.json"]);
    const bobKeys = await env.OAUTH_KV.list({ prefix: `kcache:v1:${bobUser}:${bobGrant}:` });
    expect(bobKeys.keys.length).toBeGreaterThan(0);
    expect(JSON.stringify(aliceKeys.keys)).not.toContain(String(bobUser));
  });

  it("キャッシュ由来のアプリが削除されて records.json が 404 → キャッシュを消し、not_found を返す", async () => {
    const { connect, callTool, state, apiCalls } = await setup();
    const token = await connect();
    const [userId, grantId] = token.split(":");
    const prefix = `kcache:v1:${userId}:${grantId}:`;
    await callTool(token, "kintone_records_query", { app_ref: "CRM" });
    expect((await env.OAUTH_KV.list({ prefix })).keys.length).toBeGreaterThan(0);

    // アプリが削除された。以降の records.json は 404。
    state.recordsNotFound = true;
    apiCalls.length = 0;
    const r = await callTool(token, "kintone_records_query", { app_ref: "CRM" });
    expect(r.body.error).toMatchObject({ code: "not_found", details: { http_status: 404 } });
    // キャッシュで解決したので apps.json は呼ばず、再解決もしない。
    expect(apiCalls.map(path)).toEqual(["/k/v1/records.json"]);
    expect((await env.OAUTH_KV.list({ prefix })).keys).toHaveLength(0);
  });

  it("max_response_tokens: 小さい上限で response_too_large", async () => {
    const { connect, callTool } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_records_query", { app: 12, max_response_tokens: 5 });
    expect(r.body.error.code).toBe("response_too_large");
  });
});

describe("E2E: リゾルバーのエラー(details.candidates)", () => {
  it("見つからない: resolver_app_not_found(candidates は空配列)", async () => {
    const { connect, callTool } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_records_query", { app_ref: "存在しない" });
    expect(r.isError).toBe(true);
    expect(r.body.error).toMatchObject({
      code: "resolver_app_not_found",
      retryable: false,
      details: { kind: "app", ref: "存在しない", candidates: [] },
    });
  });

  it("複数に一致: resolver_app_ambiguous(candidates に {id, code, name})", async () => {
    const { connect, callTool, apiCalls } = await setup({
      apps: [
        { appId: "1", code: "A", name: "顧客A" },
        { appId: "2", code: "B", name: "顧客B" },
      ],
    });
    const token = await connect();
    const r = await callTool(token, "kintone_app_describe", { app_ref: "顧客" });
    expect(r.body.error).toMatchObject({
      code: "resolver_app_ambiguous",
      details: {
        kind: "app",
        ref: "顧客",
        candidates: [
          { id: "1", code: "A", name: "顧客A" },
          { id: "2", code: "B", name: "顧客B" },
        ],
      },
    });
    expect(apiCalls.every((c) => path(c) === "/k/v1/apps.json")).toBe(true);
  });

  it("10 ページ満杯: resolver_app_list_too_large(apps.json は 11 回で止まる)", async () => {
    const { connect, callTool, apiCalls } = await setup({ apps: manyApps(1500, "共通") });
    const token = await connect();
    const r = await callTool(token, "kintone_records_query", { app_ref: "共通" });
    expect(r.body.error).toMatchObject({
      code: "resolver_app_list_too_large",
      details: { kind: "app", ref: "共通" },
    });
    expect(apiCalls).toHaveLength(11);
  });
});
