/**
 * feedback ツール(ADR 0013)。OAuth 済みトークンで /mcp(Worker 全体への HTTP)から呼び、
 * 公開(既定で見える・read_only=true でも見える・toolsets=feedback で見える・toolsets=app では消える)と、
 * 応答に環境の値が出ないことを固定する。
 *
 * 応答はそのまま公開 issue に貼られうる。env(PUBLIC_BASE_URL・KINTONE_HOST・クライアント ID / シークレット)と
 * grant(利用者・kintone のトークン)が異なる 2 つの構成で、応答が kind ごとに完全に一致することを確かめる(C5)。
 * kintone・GitHub への通信は 0 回。
 */
import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Env } from "../../src/env";
import worker from "../../src/index";
import { jsonResponse, mockOutboundFetch } from "../helpers/mock-fetch";
import {
  DEFAULT_TARGET,
  type FlowTarget,
  firstText,
  MCP_URL,
  mcpRequest,
  readRpc,
  runAuthorizationFlow,
  setupMocks,
  type TokenSet,
} from "../helpers/oauth-flow";

afterEach(() => vi.restoreAllMocks());

const NAME = "kintone_feedback";
const ctx = { waitUntil() {}, passThroughOnException() {} } as unknown as ExecutionContext;

/** 構成 B: Worker の公開 URL・kintone のホスト・OAuth クライアント・利用者・トークンを、構成 A(テストの既定)と変えた番兵の値。 */
const B = {
  origin: "https://sentinel-worker-q7x.example.net",
  host: "sentinel-space-k3v.cybozu.com",
  clientId: "sentinel-client-id-w4n",
  clientSecret: "sentinel-client-secret-r6c",
  email: "sentinel-mail-j2@sentinel-corp.example",
  kintoneToken: "kintone-at-sentinel-tok-55w",
  kintoneRefresh: "kintone-rt-sentinel-ref-81p",
};

const envB = {
  ...(env as unknown as Env),
  PUBLIC_BASE_URL: B.origin,
  KINTONE_HOST: B.host,
  KINTONE_CLIENT_ID: B.clientId,
  KINTONE_CLIENT_SECRET: B.clientSecret,
} as Env;

const targetB: FlowTarget = {
  origin: B.origin,
  fetch: (url, init = {}) =>
    worker.fetch(new Request(url, { redirect: "manual", ...init }), envB, ctx),
};

/** OAuth の通しを流し、MCP のアクセストークンを返す。kintone・Access のモックは通しのあと外す。 */
async function login(
  target: FlowTarget,
  mocks: Parameters<typeof setupMocks>[0] = {},
  flow: { email?: string } = {},
): Promise<string> {
  const { accessKey } = await setupMocks(mocks);
  const result = await runAuthorizationFlow({ accessKey, target, ...flow });
  expect(result.tokenResponse.status).toBe(200);
  const { access_token } = (await result.tokenResponse.json()) as TokenSet;
  vi.restoreAllMocks();
  return access_token;
}

const loginA = () => login(DEFAULT_TARGET);
const loginB = () =>
  login(
    targetB,
    {
      kintoneOrigin: `https://${B.host}`,
      kintoneToken: () =>
        jsonResponse({
          access_token: B.kintoneToken,
          refresh_token: B.kintoneRefresh,
          token_type: "Bearer",
          expires_in: 3600,
        }),
    },
    { email: B.email },
  );

async function toolNames(token: string, query = ""): Promise<string[]> {
  const res = await mcpRequest(token, "tools/list", {}, `${MCP_URL}${query}`);
  expect(res.status, `tools/list ${query}`).toBe(200);
  const tools = (await readRpc(res)).result?.tools;
  expect(tools, `tools/list ${query} に result.tools がない`).toBeDefined();
  return (tools ?? []).map((t) => t.name);
}

async function callFeedback(
  token: string,
  target: FlowTarget,
  args: Record<string, unknown>,
  query = "",
) {
  const res = await mcpRequest(
    token,
    "tools/call",
    { name: NAME, arguments: args },
    `${target.origin}/mcp${query}`,
    target.fetch,
  );
  expect(res.status).toBe(200);
  return readRpc(res);
}

describe("E2E: feedback ツールの公開", () => {
  it("既定(toolsets=all)で tools/list に出る(7 ツール。最後)", async () => {
    const token = await loginA();
    const names = await toolNames(token);
    expect(names).toHaveLength(7);
    expect(names.at(-1)).toBe(NAME);
  });

  it("read_only=true でも見える(読み取り専用)", async () => {
    const token = await loginA();
    expect(await toolNames(token, "?read_only=true")).toContain(NAME);
  });

  it("toolsets=feedback ではこれだけ(read_only=true でも 0 個にならない)", async () => {
    const token = await loginA();
    expect(await toolNames(token, "?toolsets=feedback")).toEqual([NAME]);
    expect(await toolNames(token, "?toolsets=feedback&read_only=true")).toEqual([NAME]);
    expect(await toolNames(token, "?toolsets=app,feedback")).toEqual([
      "kintone_apps_search",
      "kintone_app_describe",
      NAME,
    ]);
  });

  it("toolsets=app / record では消える(常時公開の例外を作らない)", async () => {
    const token = await loginA();
    expect(await toolNames(token, "?toolsets=app")).not.toContain(NAME);
    expect(await toolNames(token, "?toolsets=record")).not.toContain(NAME);
    expect(await toolNames(token, "?toolsets=app,record")).not.toContain(NAME);
  });

  it("annotations は readOnlyHint=true・idempotentHint=true・openWorldHint=false", async () => {
    const token = await loginA();
    const res = await mcpRequest(token, "tools/list", {}, `${MCP_URL}?toolsets=feedback`);
    const tool = (await readRpc(res)).result?.tools?.[0] as unknown as {
      annotations: Record<string, unknown>;
    };
    expect(tool.annotations).toMatchObject({
      readOnlyHint: true,
      destructiveHint: false,
      idempotentHint: true,
      openWorldHint: false,
    });
  });
});

describe("E2E: feedback ツールの tools/call", () => {
  it("案内の JSON を text content で返す。kintone にも GitHub にも通信しない", async () => {
    const token = await loginA();
    const { calls } = mockOutboundFetch({});
    const rpc = await callFeedback(token, DEFAULT_TARGET, {});
    expect(rpc.result?.isError).toBeUndefined();
    const out = JSON.parse(firstText(rpc));
    expect(out.schema_version).toBe("1");
    expect(out.kind).toBe("bug");
    expect(out.repository).toEqual({
      owner: "youyo",
      repo: "kintone",
      url: "https://github.com/youyo/kintone",
    });
    expect(out.title_template).toBe("[feedback] <公開ツール名>: <一行の要約>");
    expect(out.search_url).toBe("https://github.com/youyo/kintone/issues");
    expect(out.create.web.new_issue_url).toBe("https://github.com/youyo/kintone/issues/new");
    expect(out.create.gh.command).toBe(
      "gh issue create -R youyo/kintone --title '<title>' --body-file <承認された本文のファイル>",
    );
    expect(Object.keys(out.create)).toEqual(["web", "gh", "github_mcp"]);
    expect(out.anonymization.placeholders).toEqual([
      "<USER>",
      "<EMAIL>",
      "<ORG>",
      "<HOST>",
      "<URL>",
      "<ID>",
      "<SPACE>",
      "<APP>",
      "<FIELD>",
    ]);
    expect(calls).toEqual([]);
  });

  it("tools/list の入力スキーマは kind だけ", async () => {
    const token = await loginA();
    const res = await mcpRequest(token, "tools/list", {}, `${MCP_URL}?toolsets=feedback`);
    const tool = (await readRpc(res)).result?.tools?.[0] as unknown as {
      inputSchema: { properties: Record<string, unknown>; required?: string[] };
    };
    expect(Object.keys(tool.inputSchema.properties)).toEqual(["kind"]);
    expect(tool.inputSchema.required ?? []).toEqual([]);
  });

  it("env と grant が異なる 2 つの構成で、kind ごと(4 種類)に応答が完全に一致し、どちらの環境の値も出ない(C5)", async () => {
    const tokenA = await loginA();
    const tokenB = await loginB();
    expect(tokenB).not.toBe(tokenA);

    const sentinels = [
      // 構成 A(テストの既定。vitest.config.ts の束縛と setupMocks の既定値)
      "kintone-mcp.test",
      "example.cybozu.com",
      "test-kintone-client-id",
      "test-kintone-client-secret",
      "alice",
      "kintone-at-1",
      "kintone-rt-1",
      tokenA,
      // 構成 B
      "sentinel",
      B.origin,
      B.host,
      B.clientId,
      B.clientSecret,
      B.email,
      B.kintoneToken,
      B.kintoneRefresh,
      tokenB,
    ];

    const { calls } = mockOutboundFetch({});
    for (const kind of ["bug", "feature", "question", "other"]) {
      const textA = firstText(await callFeedback(tokenA, DEFAULT_TARGET, { kind }));
      const textB = firstText(await callFeedback(tokenB, targetB, { kind }));
      expect(textA, kind).not.toBe("");
      expect(JSON.parse(textA).kind, kind).toBe(kind);
      expect(textB, kind).toBe(textA);
      for (const s of sentinels) {
        // 作成先の公開リポジトリ名(youyo/kintone)や、サーバー名(kintone-mcp)は応答に入る固定値
        expect(textA, `${kind} に ${s}`).not.toContain(s);
      }
    }
    // kind 省略(既定 bug)と read_only で絞った接続でも同じ応答
    const omitted = firstText(await callFeedback(tokenB, targetB, {}, "?read_only=true"));
    expect(omitted).toBe(firstText(await callFeedback(tokenA, DEFAULT_TARGET, { kind: "bug" })));
    expect(calls).toEqual([]);
  });

  it("不正な kind は argument_error のエンベロープで、値をそのまま返さない", async () => {
    const token = await loginA();
    const { calls } = mockOutboundFetch({});
    const rpc = await callFeedback(token, DEFAULT_TARGET, {
      kind: "sentinel-kind-https://example.cybozu.com",
    });
    expect(rpc.result?.isError).toBe(true);
    const text = firstText(rpc);
    expect(JSON.parse(text)).toMatchObject({
      schema_version: "1",
      error: { code: "argument_error", retryable: false },
    });
    expect(text).not.toContain("sentinel");
    expect(text).not.toContain("example.cybozu.com");
    expect(calls).toEqual([]);
  });

  it("宣言していない引数(tool_name・下書きの title / body・max_response_tokens)は argument_error で、値を返さない", async () => {
    const token = await loginA();
    const { calls } = mockOutboundFetch({});
    for (const extra of [
      { tool_name: "sentinel_tool_q1" },
      { title: "sentinel-title-q2" },
      { body: "sentinel-body-q3" },
      { max_response_tokens: 1 },
    ]) {
      const rpc = await callFeedback(token, DEFAULT_TARGET, { kind: "bug", ...extra });
      expect(rpc.result?.isError, JSON.stringify(extra)).toBe(true);
      const text = firstText(rpc);
      expect(JSON.parse(text).error.code).toBe("argument_error");
      expect(text).not.toContain("sentinel");
    }
    expect(calls).toEqual([]);
  });
});
