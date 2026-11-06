/**
 * M04 の書き込みツール 3 つ(kintone_record_create / kintone_record_update / kintone_record_delete)を、
 * Worker 全体への HTTP(/register → /authorize → /callback → /token → /mcp)で通す E2E。
 * kintone の REST と OAuth、Access の JWKS は fetch のモック(実通信なし)。
 */
import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import { userIdFromEmail } from "../../src/auth/user-id";
import {
  type AppRow,
  fieldsResponse,
  listApps,
  queryFromSearchParams,
} from "../helpers/fake-kintone-data";
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
import type { ToolBody } from "../helpers/tool-harness";

afterEach(() => vi.restoreAllMocks());

const ROWS: AppRow[] = [
  { appId: "12", code: "CRM", name: "顧客管理" },
  { appId: "13", code: "TASK", name: "タスク" },
  { appId: "14", code: "SALES2", name: "売上" },
  { appId: "15", code: "SALES3", name: "売上報告" },
];
const FIELDS = fieldsResponse([
  { type: "SINGLE_LINE_TEXT", code: "customer_code", label: "顧客コード" },
  { type: "SINGLE_LINE_TEXT", code: "customer_name", label: "顧客名" },
  { type: "NUMBER", code: "tax_amount", label: "消費税額" },
]);
const REC = { customer_name: { value: "山田" } };

const path = (c: RecordedCall) => new URL(c.url).pathname;
const params = (c: RecordedCall) => new URL(c.url).searchParams;
const bodyOf = (c: RecordedCall) => JSON.parse(c.body) as Record<string, unknown>;

interface State {
  /** 書き込み(POST / PUT / DELETE)に返すステータス。200 以外のときはエラー本文を返す。 */
  writeStatus: number;
}

/** 認可を済ませた状態の kintone のモック。認可時の疎通確認(apps.json?limit=1)には通常の応答を返す。 */
async function setup() {
  const state: State & { afterFlow: boolean } = { writeStatus: 200, afterFlow: false };
  const apiCalls: RecordedCall[] = [];
  const track = (c: RecordedCall) => {
    if (state.afterFlow) apiCalls.push(c);
  };
  const failure = () =>
    jsonResponse({ code: "GAIA_X", id: "i", message: "write failed" }, state.writeStatus);
  const mocks = await setupMocks({
    kintoneApps: (call) => {
      track(call);
      return jsonResponse(listApps(ROWS, queryFromSearchParams(params(call))));
    },
    extra: {
      [`GET ${KINTONE_ORIGIN}/k/v1/app/form/fields.json`]: (call) => {
        track(call);
        return jsonResponse(FIELDS);
      },
      [`POST ${KINTONE_ORIGIN}/k/v1/records.json`]: (call) => {
        track(call);
        if (state.writeStatus !== 200) return failure();
        const records = (bodyOf(call).records as unknown[]) ?? [];
        return jsonResponse({
          ids: records.map((_, i) => String(100 + i)),
          revisions: records.map(() => "1"),
        });
      },
      [`PUT ${KINTONE_ORIGIN}/k/v1/record.json`]: (call) => {
        track(call);
        return state.writeStatus !== 200 ? failure() : jsonResponse({ revision: "8" });
      },
      [`DELETE ${KINTONE_ORIGIN}/k/v1/records.json`]: (call) => {
        track(call);
        return state.writeStatus !== 200 ? failure() : jsonResponse({});
      },
    },
  });
  const connect = async (email?: string) => {
    const flow = await runAuthorizationFlow({
      accessKey: mocks.accessKey,
      ...(email ? { email } : {}),
    });
    const { access_token } = (await flow.tokenResponse.json()) as TokenSet;
    state.afterFlow = true;
    return access_token;
  };
  const callRaw = (
    token: string,
    name: string,
    args: Record<string, unknown> = {},
    url = MCP_URL,
    meta?: Record<string, unknown>,
  ) =>
    mcpRequest(
      token,
      "tools/call",
      { name, arguments: args, ...(meta ? { _meta: meta } : {}) },
      url,
    );
  const callTool = async (
    token: string,
    name: string,
    args: Record<string, unknown> = {},
    url = MCP_URL,
    meta?: Record<string, unknown>,
  ) => {
    const res = await callRaw(token, name, args, url, meta);
    expect(res.status).toBe(200);
    const rpc = await readRpc(res);
    const text = firstText(rpc);
    return { rpc, isError: rpc.result?.isError === true, text, body: JSON.parse(text) as ToolBody };
  };
  const writes = () => apiCalls.filter((c) => c.method !== "GET");
  return { ...mocks, state, apiCalls, writes, connect, callTool, callRaw };
}

const WRITE_TOOLS = ["kintone_record_create", "kintone_record_update", "kintone_record_delete"];
const READ_TOOLS = ["kintone_apps_search", "kintone_app_describe", "kintone_records_query"];

describe("E2E: tools/list(書き込みツールと URL クエリ)", () => {
  it("6 ツールが見える。?toolsets=record は 4 つ、?toolsets=app は 2 つ", async () => {
    const { connect } = await setup();
    const token = await connect();
    const list = async (query = "") =>
      (
        (await readRpc(await mcpRequest(token, "tools/list", {}, `${MCP_URL}${query}`))).result
          ?.tools ?? []
      ).map((t) => t.name);
    expect(await list()).toEqual([...READ_TOOLS, ...WRITE_TOOLS]);
    expect(await list("?toolsets=record")).toEqual(["kintone_records_query", ...WRITE_TOOLS]);
    expect(await list("?toolsets=app")).toEqual(["kintone_apps_search", "kintone_app_describe"]);
  });

  it("annotation: create = WriteNonIdempotent、update = WriteIdempotent、delete = Destructive(destructiveHint=true)", async () => {
    const { connect } = await setup();
    const token = await connect();
    const tools = (await readRpc(await mcpRequest(token, "tools/list"))).result?.tools ?? [];
    const a = (name: string) => tools.find((t) => t.name === name)?.annotations;
    expect(a("kintone_record_create")).toMatchObject({
      readOnlyHint: false,
      destructiveHint: false,
      idempotentHint: false,
    });
    expect(a("kintone_record_update")).toMatchObject({
      readOnlyHint: false,
      destructiveHint: false,
      idempotentHint: true,
    });
    expect(a("kintone_record_delete")).toMatchObject({
      readOnlyHint: false,
      destructiveHint: true,
    });
  });

  it("read_only=true では書き込み 3 ツールが tools/list に出ず、tools/call でも呼べない(kintone を呼ばない)", async () => {
    const { connect, callRaw, apiCalls } = await setup();
    const token = await connect();
    const url = `${MCP_URL}?read_only=true`;
    const names = (
      (await readRpc(await mcpRequest(token, "tools/list", {}, url))).result?.tools ?? []
    ).map((t) => t.name);
    expect(names).toEqual(READ_TOOLS);
    expect(
      (await mcpRequest(token, "tools/list", {}, `${MCP_URL}?toolsets=record&read_only=true`))
        .status,
    ).toBe(200);

    for (const [name, args] of [
      ["kintone_record_create", { app: 12, record: REC }],
      ["kintone_record_update", { app: 12, id: 1, record: REC }],
      ["kintone_record_delete", { app: 12, ids: [1] }],
    ] as const) {
      const res = await callRaw(token, name, args, url);
      const text = await res.text();
      expect(text, name).not.toContain('"ids"');
      expect(text, name).not.toContain('"revision"');
      expect(text, name).not.toContain('"deleted"');
      expect(text, name).toMatch(/not found|unknown|error/i);
    }
    expect(apiCalls).toEqual([]);
  });
});

describe("E2E: kintone_record_create", () => {
  it("app + record: POST /k/v1/records.json に {app, records:[record]}(JSON)。ids / revisions は数値", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_record_create", { app: 12, record: REC });
    expect(r.isError).toBe(false);
    expect(apiCalls).toHaveLength(1);
    const call = apiCalls[0] as RecordedCall;
    expect([call.method, path(call)]).toEqual(["POST", "/k/v1/records.json"]);
    expect(call.headers.get("content-type")).toBe("application/json");
    expect(call.headers.get("authorization")).toMatch(/^Bearer .+/);
    expect(bodyOf(call)).toEqual({ app: 12, records: [REC] });
    expect(r.body).toEqual({ ids: [100], revisions: [1] });
  });

  it("app_ref(コード・名前の完全一致)で解決して records(複数件)を作る", async () => {
    const { connect, callTool, writes } = await setup();
    const token = await connect();
    const byCode = await callTool(token, "kintone_record_create", {
      app_ref: "CRM",
      records: [REC, REC],
    });
    expect(byCode.body).toEqual({ ids: [100, 101], revisions: [1, 1] });
    const byName = await callTool(token, "kintone_record_create", {
      app_ref: "顧客管理",
      record: { customer_name: { value: "佐藤" } },
    });
    expect(byName.isError).toBe(false);
    expect(writes().map((c) => bodyOf(c).app)).toEqual([12, 12]);
  });

  it("101 件の records は argument_error で kintone を一度も呼ばない", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const records = Array.from({ length: 101 }, () => REC);
    const r = await callTool(token, "kintone_record_create", { app_ref: "CRM", records });
    expect(r.body.error.code).toBe("argument_error");
    expect(apiCalls).toEqual([]);
  });

  it("app_ref が部分一致のみ(1 件でも複数件でも)なら、POST を呼ばず resolver_app_exact_match_required + candidates", async () => {
    const { connect, callTool, writes } = await setup();
    const token = await connect();
    const one = await callTool(token, "kintone_record_create", { app_ref: "顧客", record: REC });
    expect(one.isError).toBe(true);
    expect(one.body.error).toMatchObject({
      code: "resolver_app_exact_match_required",
      retryable: false,
      details: {
        kind: "app",
        ref: "顧客",
        candidates: [{ id: "12", code: "CRM", name: "顧客管理" }],
      },
    });
    const many = await callTool(token, "kintone_record_create", { app_ref: "売", record: REC });
    expect(many.body.error.code).toBe("resolver_app_exact_match_required");
    expect(many.body.error.details.candidates).toEqual([
      { id: "14", code: "SALES2", name: "売上" },
      { id: "15", code: "SALES3", name: "売上報告" },
    ]);
    expect(writes()).toEqual([]);
    // 候補の ID で再実行できる。
    const retry = await callTool(token, "kintone_record_create", { app: 12, record: REC });
    expect(retry.isError).toBe(false);
    expect(writes()).toHaveLength(1);
  });

  it("書き込みの 429 はリトライされない(POST は 1 回)。rate_limited(retryable)を返す", async () => {
    const { connect, callTool, state, writes } = await setup();
    const token = await connect();
    state.writeStatus = 429;
    const r = await callTool(token, "kintone_record_create", { app: 12, record: REC });
    expect(r.body.error).toMatchObject({ code: "rate_limited", retryable: true });
    expect(writes()).toHaveLength(1);
  });

  it("書き込みの 5xx もリトライされず、エラーは冪等キャッシュに保存されない(再送すると POST し直す)", async () => {
    const { connect, callTool, state, writes } = await setup();
    const token = await connect();
    const [userId, grantId] = token.split(":");
    state.writeStatus = 503;
    const failed = await callTool(token, "kintone_record_create", { app: 12, record: REC });
    expect(failed.body.error.code).toBe("api_error");
    expect(writes()).toHaveLength(1);
    expect((await env.OAUTH_KV.list({ prefix: `idem:${userId}:${grantId}:` })).keys).toHaveLength(
      0,
    );
    state.writeStatus = 200;
    const ok = await callTool(token, "kintone_record_create", { app: 12, record: REC });
    expect(ok.body).toEqual({ ids: [100], revisions: [1] });
    expect(writes()).toHaveLength(2);
  });

  it("kintone が 401 を返したら MCP の応答も HTTP 401(再認可・更新の合図)", async () => {
    const { connect, callRaw, state } = await setup();
    const token = await connect();
    vi.spyOn(console, "warn").mockImplementation(() => {});
    state.writeStatus = 401;
    const res = await callRaw(token, "kintone_record_create", { app: 12, record: REC });
    expect(res.status).toBe(401);
    expect(res.headers.get("WWW-Authenticate")).toContain('error="invalid_token"');
  });
});

describe("E2E: record_create の冪等キャッシュ(idem:)", () => {
  it("同じ引数の再送は kintone を呼ばずに前回の結果を返す。キーは idem:<userId>:<grantId>:... で、email・トークンを含まず、TTL は 300 秒", async () => {
    const { connect, callTool, apiCalls, writes } = await setup();
    const email = "alice@example.com";
    const token = await connect(email);
    const [userId, grantId, secret] = token.split(":");
    expect(userId).toBe(await userIdFromEmail(email));

    const first = await callTool(token, "kintone_record_create", { app_ref: "CRM", record: REC });
    expect(first.body).toEqual({ ids: [100], revisions: [1] });
    const callsAfterFirst = apiCalls.length;
    expect(writes()).toHaveLength(1);

    const second = await callTool(token, "kintone_record_create", { app_ref: "CRM", record: REC });
    expect(second.text).toBe(first.text);
    // 名前解決(apps.json)も書き込みも呼ばない。
    expect(apiCalls).toHaveLength(callsAfterFirst);

    const listed = await env.OAUTH_KV.list({ prefix: "idem:" });
    const mine = listed.keys.filter((k) => k.name.startsWith(`idem:${userId}:${grantId}:`));
    expect(mine).toHaveLength(1);
    const key = mine[0]?.name ?? "";
    expect(key).toMatch(/^idem:[^:]+:[^:]+:kintone_record_create:hash:[0-9a-f]{64}$/);
    for (const forbidden of [email, "alice", secret ?? "?"]) {
      expect(key).not.toContain(forbidden);
    }
    const value = (await env.OAUTH_KV.get(key)) ?? "";
    expect(value).not.toContain(email);
    expect(value).not.toContain(secret ?? "?");
    const now = Math.floor(Date.now() / 1000);
    expect(mine[0]?.expiration ?? 0).toBeGreaterThanOrEqual(now + 240);
    expect(mine[0]?.expiration ?? 0).toBeLessThanOrEqual(now + 360);
  });

  it("_meta.idempotency_key: 同じキーは再送として扱い、別のキーなら同じ引数でも作成する。同じキーで別の引数は argument_error", async () => {
    const { connect, callTool, writes } = await setup();
    const token = await connect();
    const call = (args: Record<string, unknown>, key: string) =>
      callTool(token, "kintone_record_create", args, MCP_URL, { idempotency_key: key });
    const a = await call({ app: 12, record: REC }, "key-a");
    const again = await call({ app: 12, record: REC }, "key-a");
    expect(again.text).toBe(a.text);
    expect(writes()).toHaveLength(1);
    await call({ app: 12, record: REC }, "key-b");
    expect(writes()).toHaveLength(2);
    const misuse = await call({ app: 12, record: { customer_name: { value: "別人" } } }, "key-a");
    expect(misuse.body.error.code).toBe("argument_error");
    expect(writes()).toHaveLength(2);
    // KV のキーに生の明示キーは入らない。
    const keys = (await env.OAUTH_KV.list({ prefix: "idem:" })).keys.map((k) => k.name);
    expect(keys.some((k) => k.includes(":explicit:"))).toBe(true);
    expect(keys.some((k) => k.includes("key-a"))).toBe(false);
  });

  it("別の利用者・別の grant とは共有しない(同じ引数でも作成する)。record_update / record_delete は対象外", async () => {
    const { connect, callTool, writes } = await setup();
    const alice = await connect("alice@example.com");
    const bob = await connect("bob@example.com");
    await callTool(alice, "kintone_record_create", { app: 12, record: REC });
    await callTool(bob, "kintone_record_create", { app: 12, record: REC });
    expect(writes()).toHaveLength(2);
    await callTool(alice, "kintone_record_create", { app: 12, record: REC });
    expect(writes()).toHaveLength(2);

    const before = (await env.OAUTH_KV.list({ prefix: "idem:" })).keys.length;
    await callTool(alice, "kintone_record_update", { app: 12, id: 1, record: REC });
    await callTool(alice, "kintone_record_update", { app: 12, id: 1, record: REC });
    await callTool(alice, "kintone_record_delete", { app: 12, ids: [1] });
    await callTool(alice, "kintone_record_delete", { app: 12, ids: [1] });
    expect(
      writes()
        .map((c) => c.method)
        .slice(2),
    ).toEqual(["PUT", "PUT", "DELETE", "DELETE"]);
    expect((await env.OAUTH_KV.list({ prefix: "idem:" })).keys).toHaveLength(before);
  });
});

describe("E2E: kintone_record_update", () => {
  it("id 指定: PUT /k/v1/record.json に {app, id, revision, record}。出力 {revision} は数値", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_record_update", {
      app: 12,
      id: 5,
      revision: 3,
      record: { customer_name: { value: "山田" } },
    });
    expect(r.isError).toBe(false);
    expect(apiCalls).toHaveLength(1);
    const call = apiCalls[0] as RecordedCall;
    expect([call.method, path(call)]).toEqual(["PUT", "/k/v1/record.json"]);
    expect(call.headers.get("content-type")).toBe("application/json");
    expect(bodyOf(call)).toEqual({
      app: 12,
      id: 5,
      revision: 3,
      record: { customer_name: { value: "山田" } },
    });
    expect(r.body).toEqual({ revision: 8 });
  });

  it("update_key_field_ref(label の完全一致): fields.json で解決し、updateKey.field はフィールドコード", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_record_update", {
      app_ref: "CRM",
      update_key_field_ref: "顧客コード",
      update_key_value: "C-001",
      record: REC,
    });
    expect(r.isError).toBe(false);
    expect(apiCalls.map((c) => `${c.method} ${path(c)}`)).toEqual([
      "GET /k/v1/apps.json",
      "GET /k/v1/app/form/fields.json",
      "PUT /k/v1/record.json",
    ]);
    expect(bodyOf(apiCalls[2] as RecordedCall)).toEqual({
      app: 12,
      updateKey: { field: "customer_code", value: "C-001" },
      record: REC,
    });
  });

  it("update_key_field_ref が部分一致のみなら、PUT を呼ばず resolver_field_exact_match_required + candidates", async () => {
    const { connect, callTool, writes } = await setup();
    const token = await connect();
    const one = await callTool(token, "kintone_record_update", {
      app: 12,
      update_key_field_ref: "消費税",
      update_key_value: "1",
      record: REC,
    });
    expect(one.body.error).toMatchObject({
      code: "resolver_field_exact_match_required",
      retryable: false,
      details: {
        kind: "field",
        ref: "消費税",
        candidates: [{ code: "tax_amount", label: "消費税額" }],
      },
    });
    const many = await callTool(token, "kintone_record_update", {
      app: 12,
      update_key_field_ref: "顧客",
      update_key_value: "1",
      record: REC,
    });
    expect(many.body.error.code).toBe("resolver_field_exact_match_required");
    expect(many.body.error.details.candidates).toEqual([
      { code: "customer_code", label: "顧客コード" },
      { code: "customer_name", label: "顧客名" },
    ]);
    expect(writes()).toEqual([]);
  });

  it("app_ref が部分一致のみなら、PUT を呼ばず resolver_app_exact_match_required + candidates", async () => {
    const { connect, callTool, writes } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_record_update", {
      app_ref: "顧客",
      id: 1,
      record: REC,
    });
    expect(r.body.error).toMatchObject({
      code: "resolver_app_exact_match_required",
      details: {
        kind: "app",
        ref: "顧客",
        candidates: [{ id: "12", code: "CRM", name: "顧客管理" }],
      },
    });
    expect(writes()).toEqual([]);
  });

  it("id と update_key の両方・record が空は argument_error で kintone を呼ばない。書き込みの 429 はリトライされない", async () => {
    const { connect, callTool, apiCalls, state, writes } = await setup();
    const token = await connect();
    for (const args of [
      { app: 12, id: 1, update_key_field: "a", update_key_value: "1", record: REC },
      { app: 12, id: 1, record: {} },
    ]) {
      const r = await callTool(token, "kintone_record_update", args);
      expect(r.body.error.code).toBe("argument_error");
    }
    expect(apiCalls).toEqual([]);
    state.writeStatus = 429;
    const limited = await callTool(token, "kintone_record_update", { app: 12, id: 1, record: REC });
    expect(limited.body.error.code).toBe("rate_limited");
    expect(writes()).toHaveLength(1);
  });
});

describe("E2E: kintone_record_delete", () => {
  it("DELETE /k/v1/records.json の JSON 本文は {app, ids, revisions}。出力 {deleted}", async () => {
    const { connect, callTool, apiCalls } = await setup();
    const token = await connect();
    const r = await callTool(token, "kintone_record_delete", {
      app: 12,
      ids: [1, 2, 3],
      revisions: [4, 5, -1],
    });
    expect(r.body).toEqual({ deleted: 3 });
    expect(apiCalls).toHaveLength(1);
    const call = apiCalls[0] as RecordedCall;
    expect([call.method, path(call)]).toEqual(["DELETE", "/k/v1/records.json"]);
    expect(call.headers.get("content-type")).toBe("application/json");
    expect(bodyOf(call)).toEqual({ app: 12, ids: [1, 2, 3], revisions: [4, 5, -1] });
    // クエリ文字列ではなく本文で送る。
    expect(new URL(call.url).search).toBe("");
  });

  it("app_ref が部分一致のみなら、DELETE を呼ばず resolver_app_exact_match_required + candidates。完全一致の名前なら削除できる", async () => {
    const { connect, callTool, writes } = await setup();
    const token = await connect();
    const partial = await callTool(token, "kintone_record_delete", { app_ref: "顧客", ids: [1] });
    expect(partial.body.error).toMatchObject({
      code: "resolver_app_exact_match_required",
      details: {
        kind: "app",
        ref: "顧客",
        candidates: [{ id: "12", code: "CRM", name: "顧客管理" }],
      },
    });
    expect(writes()).toEqual([]);
    const exact = await callTool(token, "kintone_record_delete", { app_ref: "顧客管理", ids: [1] });
    expect(exact.body).toEqual({ deleted: 1 });
    expect(writes().map((c) => bodyOf(c).app)).toEqual([12]);
  });

  it("ids が 101 件・revisions の長さ違いは argument_error で kintone を呼ばない。書き込みの 503 はリトライされない", async () => {
    const { connect, callTool, apiCalls, state, writes } = await setup();
    const token = await connect();
    const tooMany = await callTool(token, "kintone_record_delete", {
      app_ref: "CRM",
      ids: Array.from({ length: 101 }, (_, i) => i + 1),
    });
    expect(tooMany.body.error.code).toBe("argument_error");
    const mismatch = await callTool(token, "kintone_record_delete", {
      app: 12,
      ids: [1, 2],
      revisions: [1],
    });
    expect(mismatch.body.error.code).toBe("argument_error");
    expect(apiCalls).toEqual([]);
    state.writeStatus = 503;
    const failed = await callTool(token, "kintone_record_delete", { app: 12, ids: [1] });
    expect(failed.body.error.code).toBe("api_error");
    expect(writes()).toHaveLength(1);
  });

  it("書き込みはキャッシュを読まず毎回 API で解決する。DELETE が 404 なら not_found を返す(解決は API の最新なのでキャッシュは消さない)", async () => {
    const { connect, callTool, state, apiCalls } = await setup();
    const token = await connect();
    const [userId, grantId] = token.split(":");
    const prefix = `kcache:v1:${userId}:${grantId}:`;
    await callTool(token, "kintone_record_delete", { app_ref: "CRM", ids: [1] });
    const cached = (await env.OAUTH_KV.list({ prefix })).keys.length;
    expect(cached).toBeGreaterThan(0); // 解決結果はキャッシュに書く

    state.writeStatus = 404;
    apiCalls.length = 0;
    const r = await callTool(token, "kintone_record_delete", { app_ref: "CRM", ids: [2] });
    expect(r.body.error).toMatchObject({ code: "not_found", details: { http_status: 404 } });
    // キャッシュがあっても、もう一度 apps.json で解決してから DELETE する
    expect(apiCalls.map((c) => `${c.method} ${path(c)}`)).toEqual([
      "GET /k/v1/apps.json",
      "DELETE /k/v1/records.json",
    ]);
    expect((await env.OAUTH_KV.list({ prefix })).keys).toHaveLength(cached);
  });
});
