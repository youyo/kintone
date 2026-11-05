import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import type { GrantProps } from "../../src/auth/props";
import { createMcpApiHandler } from "../../src/mcp/api-handler";
import { defineTool } from "../../src/mcp/registry";
import { jsonResponse, mockOutboundFetch } from "../helpers/mock-fetch";
import { readRpc } from "../helpers/oauth-flow";

const APPS = "GET https://example.cybozu.com/k/v1/apps.json";
const props: GrantProps = {
  kintoneAccessToken: "kintone-at",
  kintoneRefreshToken: "kintone-rt",
  kintoneExpiresAt: Date.now() + 3_600_000,
};
const MCP_TOKEN = "userid43chars:grant-unit-1:mcp-secret-value";

function makeCtx(overrides: { token?: string; props?: GrantProps } = {}) {
  return {
    waitUntil() {},
    passThroughOnException() {},
    props: overrides.props ?? props,
    auth: {
      token: overrides.token ?? MCP_TOKEN,
      audience: "https://kintone-mcp.test/mcp",
      scope: [],
      userId: "userid43chars",
    },
  } as unknown as ExecutionContext;
}

function callTool(testEnv: object = env, ctx: ExecutionContext = makeCtx()) {
  return createMcpApiHandler().fetch(
    new Request("https://kintone-mcp.test/mcp", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "application/json, text/event-stream",
      },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "kintone_apps_search", arguments: { limit: 1 } },
      }),
    }),
    testEnv as typeof env,
    ctx,
  );
}

afterEach(() => vi.restoreAllMocks());

describe("MCP API handler (/mcp の下流)", () => {
  it("grant の props にある kintone アクセストークンで kintone を呼ぶ", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [{ code: "CRM" }] }) });
    const res = await callTool();
    expect(res.status).toBe(200);
    expect(await res.text()).toContain("CRM");
    expect(m.calls[0]?.headers.get("authorization")).toBe("Bearer kintone-at");
    expect(new URL(m.calls[0]?.url ?? "").searchParams.get("limit")).toBe("1");
  });

  it("kintone が 401 なら MCP も 401 + invalid_token の WWW-Authenticate にし、トークンは応答に含めない", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    mockOutboundFetch({ [APPS]: () => jsonResponse({}, 401) });
    const res = await callTool();
    expect(res.status).toBe(401);
    const challenge = res.headers.get("WWW-Authenticate") ?? "";
    expect(challenge).toMatch(/^Bearer /);
    expect(challenge).toContain('error="invalid_token"');
    expect(challenge).toContain(
      'resource_metadata="https://kintone-mcp.test/.well-known/oauth-protected-resource/mcp"',
    );
    const body = await res.text();
    expect(body).not.toContain("kintone-at");
    expect(challenge).not.toContain("kintone-at");
  });

  describe("kintone の 401 で grant に更新の印を置く", () => {
    it("kintone-stale:<userId>:<grantId> を KV に置く(値にトークンは入らない)", async () => {
      vi.spyOn(console, "warn").mockImplementation(() => {});
      mockOutboundFetch({ [APPS]: () => jsonResponse({}, 401) });
      const res = await callTool();
      expect(res.status).toBe(401);
      const value = await env.OAUTH_KV.get("kintone-stale:userid43chars:grant-unit-1");
      expect(value).toBe("1");
      for (const secret of ["kintone-at", "kintone-rt", "mcp-secret-value"]) {
        expect(value).not.toContain(secret);
      }
    });

    it("kintone が 200 なら印を置かない", async () => {
      mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [] }) });
      const res = await callTool(env, makeCtx({ token: "u:grant-unit-ok:secret" }));
      expect(res.status).toBe(200);
      expect(await env.OAUTH_KV.get("kintone-stale:u:grant-unit-ok")).toBeNull();
    });

    it.each([403, 404, 429, 500])(
      "kintone が %i なら印を置かない(401 のときだけ置く)",
      async (status) => {
        vi.spyOn(console, "warn").mockImplementation(() => {});
        mockOutboundFetch({ [APPS]: () => jsonResponse({}, status) });
        const res = await callTool(env, makeCtx({ token: `u:grant-unit-${status}:secret` }));
        expect(res.status).not.toBe(401);
        expect(await env.OAUTH_KV.get(`kintone-stale:u:grant-unit-${status}`)).toBeNull();
      },
      30_000,
    );

    it("KV への書き込みが失敗しても 401 は返す", async () => {
      vi.spyOn(console, "warn").mockImplementation(() => {});
      mockOutboundFetch({ [APPS]: () => jsonResponse({}, 401) });
      const brokenKv = {
        ...env.OAUTH_KV,
        get: env.OAUTH_KV.get.bind(env.OAUTH_KV),
        put: async () => {
          throw new Error("kv down");
        },
      };
      const res = await callTool({ ...env, OAUTH_KV: brokenKv });
      expect(res.status).toBe(401);
    });

    it("MCP トークンの形式が読めなくても(印は置けないが)401 は返す", async () => {
      vi.spyOn(console, "warn").mockImplementation(() => {});
      mockOutboundFetch({ [APPS]: () => jsonResponse({}, 401) });
      const res = await callTool(env, makeCtx({ token: "unparseable" }));
      expect(res.status).toBe(401);
    });

    it("401 の診断ログに props 上の残り秒数と印の結果を出し、トークンは出さない", async () => {
      const lines: string[] = [];
      for (const level of ["log", "info", "warn", "error"] as const) {
        vi.spyOn(console, level).mockImplementation((...args: unknown[]) => {
          lines.push(args.map(String).join(" "));
        });
      }
      mockOutboundFetch({ [APPS]: () => jsonResponse({}, 401) });
      await callTool(
        env,
        makeCtx({ props: { ...props, kintoneExpiresAt: Date.now() + 2_000_000 } }),
      );
      const event = lines
        .flatMap((l) => {
          try {
            return [JSON.parse(l) as Record<string, unknown>];
          } catch {
            return [];
          }
        })
        .find((e) => e.event === "kintone_unauthorized");
      expect(event).toMatchObject({ event: "kintone_unauthorized", stale_marked: true });
      expect(event?.kintone_remaining_seconds as number).toBeGreaterThan(1900);
      expect(event?.kintone_remaining_seconds as number).toBeLessThanOrEqual(2000);
      const all = lines.join("\n");
      for (const secret of ["kintone-at", "kintone-rt", "mcp-secret-value", MCP_TOKEN]) {
        expect(all).not.toContain(secret);
      }
    });
  });
});

describe("/mcp の URL クエリ(read_only / toolsets。仕様書 §8)", () => {
  function rpc(
    query: string,
    method: string,
    params: Record<string, unknown> = {},
    httpMethod = "POST",
  ) {
    return createMcpApiHandler().fetch(
      new Request(`https://kintone-mcp.test/mcp${query}`, {
        method: httpMethod,
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json, text/event-stream",
        },
        body:
          httpMethod === "POST"
            ? JSON.stringify({ jsonrpc: "2.0", id: 1, method, params })
            : undefined,
      }),
      env,
      makeCtx(),
    );
  }

  const listNames = async (query: string) => {
    const res = await rpc(query, "tools/list");
    expect(res.status).toBe(200);
    return ((await readRpc(res)).result?.tools ?? []).map((t) => t.name);
  };

  const ALL_READ = ["kintone_apps_search", "kintone_app_describe", "kintone_records_query"];

  it.each([
    "",
    "?read_only=true",
    "?read_only=false",
    "?toolsets=all&read_only=true",
    "?x=1",
  ])("有効なクエリ(%s)では tools/list が通り、M03 の読み取りツール 3 つが見える", async (query) => {
    expect(await listNames(query)).toEqual(ALL_READ);
  });

  it("?toolsets=app は apps_search と app_describe、?toolsets=record は records_query だけ", async () => {
    expect(await listNames("?toolsets=app")).toEqual([
      "kintone_apps_search",
      "kintone_app_describe",
    ]);
    expect(await listNames("?toolsets=record")).toEqual(["kintone_records_query"]);
    expect(await listNames("?toolsets=record&read_only=true")).toEqual(["kintone_records_query"]);
  });

  it.each([
    ["未知の toolset", "?toolsets=nope"],
    ["read_only が true / false 以外", "?read_only=maybe"],
    ["read_only の重複", "?read_only=true&read_only=false"],
    ["綴り違いのキー(readonly)", "?readonly=true"],
    ["綴り違いのキー(toolset)", "?toolset=app"],
  ])("%s は HTTP 400 invalid_query。kintone は呼ばない", async (_name, query) => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [] }) });
    for (const [method, name] of [
      ["tools/list", undefined],
      ["tools/call", "kintone_apps_search"],
    ] as const) {
      const res = await rpc(query, method, name ? { name, arguments: {} } : {});
      expect(res.status, `${method} ${query}`).toBe(400);
      expect(res.headers.get("Content-Type")).toContain("application/json");
      const body = (await res.json()) as { error: string; error_description: string };
      expect(body.error).toBe("invalid_query");
      expect(body.error_description.length).toBeGreaterThan(0);
    }
    expect(m.calls).toHaveLength(0);
  });

  it("公開ツールが 0 個になる組み合わせは 400(M03 で実ツールは読み取りだけになったので、書き込みしかない合成の一覧で確かめる)", async () => {
    const writeOnly = [
      defineTool({
        name: "kintone_record_fake",
        title: "テスト",
        description: "write-only fake",
        category: "WriteNonIdempotent",
        toolset: "record",
        inputSchema: z.strictObject({}),
        handler: async () => ({}),
      }),
    ];
    const res = await createMcpApiHandler({ tools: writeOnly }).fetch(
      new Request("https://kintone-mcp.test/mcp?read_only=true", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json, text/event-stream",
        },
        body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list", params: {} }),
      }),
      env,
      makeCtx(),
    );
    expect(res.status).toBe(400);
    expect(((await res.json()) as { error: string }).error).toBe("invalid_query");
  });

  it("GET /mcp でも不正なクエリは 400", async () => {
    const res = await rpc("?readonly=true", "", {}, "GET");
    expect(res.status).toBe(400);
    expect(((await res.json()) as { error: string }).error).toBe("invalid_query");
  });

  it("tools/call は kintone の呼び出しに max_response_tokens を送らない(クエリ付き URL でも通る)", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [{ code: "CRM" }] }) });
    const res = await rpc("?read_only=true", "tools/call", {
      name: "kintone_apps_search",
      arguments: { limit: 2, max_response_tokens: 0 },
    });
    expect(res.status).toBe(200);
    const params = new URL(m.calls[0]?.url ?? "").searchParams;
    expect([...params.keys()]).toEqual(["limit"]);
  });
});

describe("/mcp とキャッシュの grant 単位のキー", () => {
  it("MCP トークンの <userId>:<grantId> を kcache のキーに使う(秘密の部分は使わない)", async () => {
    const m = mockOutboundFetch({
      [APPS]: () => jsonResponse({ apps: [{ appId: "12", code: "CRM", name: "顧客管理" }] }),
      "GET https://example.cybozu.com/k/v1/records.json": () => jsonResponse({ records: [] }),
    });
    const res = await createMcpApiHandler().fetch(
      new Request("https://kintone-mcp.test/mcp", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json, text/event-stream",
        },
        body: JSON.stringify({
          jsonrpc: "2.0",
          id: 1,
          method: "tools/call",
          params: { name: "kintone_records_query", arguments: { app_ref: "CRM" } },
        }),
      }),
      env,
      makeCtx(),
    );
    expect(res.status).toBe(200);
    expect(m.calls.length).toBeGreaterThan(0);
    const prefix = "kcache:v1:userid43chars:grant-unit-1:";
    const listed = await env.OAUTH_KV.list({ prefix });
    expect(listed.keys.length).toBeGreaterThan(0);
    expect(listed.keys.every((k) => k.name.startsWith(prefix))).toBe(true);
    expect(JSON.stringify(listed.keys)).not.toContain("mcp-secret-value");
    for (const k of listed.keys) await env.OAUTH_KV.delete(k.name);
  });
});
