import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import { userIdFromEmail } from "../../src/auth/user-id";
import { jsonResponse } from "../helpers/mock-fetch";
import {
  accessHeaders,
  authorizeUrl,
  CLIENT_REDIRECT,
  CookieJar,
  firstText,
  KINTONE_APPS,
  KINTONE_ORIGIN,
  KINTONE_TOKEN,
  MCP_URL,
  mcpRequest,
  ORIGIN,
  readRpc,
  registerClient,
  resetEnv,
  runAuthorizationFlow,
  setupMocks,
  type TokenSet,
  useEnv,
  worker,
} from "../helpers/oauth-flow";

const SCOPE_6 =
  "k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write";

afterEach(() => {
  resetEnv();
  vi.restoreAllMocks();
});

async function sha256Base64url(text: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return btoa(String.fromCharCode(...new Uint8Array(digest)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

function parseCalls(calls: { method: string; url: string }[], key: string) {
  return calls.filter((c) => {
    const u = new URL(c.url);
    return `${c.method} ${u.origin}${u.pathname}` === key;
  });
}

describe("E2E 1: 未認証の /mcp は 401 とディスカバリを返す", () => {
  it("トークンなしの /mcp は 401 + WWW-Authenticate: Bearer resource_metadata=... を返し、その URL が authorization_servers と resource を含む JSON を返す", async () => {
    await setupMocks();
    const res = await mcpRequest(null, "tools/list");
    expect(res.status).toBe(401);
    const challenge = res.headers.get("WWW-Authenticate") ?? "";
    expect(challenge).toMatch(/^Bearer /);
    const metadataUrl = /resource_metadata="([^"]+)"/.exec(challenge)?.[1];
    expect(metadataUrl).toBe(`${ORIGIN}/.well-known/oauth-protected-resource/mcp`);

    // メタデータは Access を通さずに取れる
    const meta = await worker.fetch(metadataUrl ?? "");
    expect(meta.status).toBe(200);
    const body = (await meta.json()) as { resource: string; authorization_servers: string[] };
    expect(body.resource).toBe(MCP_URL);
    expect(body.authorization_servers).toContain(ORIGIN);

    const as = await worker.fetch(`${ORIGIN}/.well-known/oauth-authorization-server`);
    expect(as.status).toBe(200);
    const asBody = (await as.json()) as Record<string, string>;
    expect(asBody.authorization_endpoint).toBe(`${ORIGIN}/authorize`);
    expect(asBody.token_endpoint).toBe(`${ORIGIN}/token`);
    expect(asBody.registration_endpoint).toBe(`${ORIGIN}/register`);
  });
});

describe("E2E 2: DCR → /authorize → kintone → /callback → /token", () => {
  it("同意画面(クライアント名・送信先・kintone ホスト・Access の email)を出し、kintone の認可 URL へ送る", async () => {
    const { accessKey } = await setupMocks();
    const flow = await runAuthorizationFlow({ accessKey });

    expect(flow.consent.status).toBe(200);
    expect(flow.consent.headers.get("Content-Type")).toContain("text/html");
    expect(flow.consent.headers.get("X-Frame-Options")).toBe("DENY");
    expect(flow.html).toContain("Test MCP Client");
    expect(flow.html).toContain("client.example");
    expect(flow.html).toContain("example.cybozu.com");
    expect(flow.html).toContain("alice@example.com");

    expect(flow.kintoneRedirect.status).toBe(302);
    const kintoneUrl = new URL(flow.kintoneLocation);
    expect(kintoneUrl.origin + kintoneUrl.pathname).toBe(`${KINTONE_ORIGIN}/oauth2/authorization`);
    const p = kintoneUrl.searchParams;
    expect(p.get("client_id")).toBe("test-kintone-client-id");
    expect(p.get("redirect_uri")).toBe(`${ORIGIN}/callback`);
    expect(p.get("response_type")).toBe("code");
    expect(p.get("scope")).toBe(SCOPE_6);
    expect(p.get("code_challenge")).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(p.get("code_challenge_method")).toBe("S256");
    expect(p.get("client_secret")).toBeNull();
    expect(flow.kintoneLocation).not.toContain("test-kintone-client-secret");
  });

  it("コード交換は Basic 認証ヘッダー + フォーム(code_verifier を含み client_secret を含まない)で、challenge と verifier が対応する。続いて apps.json?limit=1 で疎通確認する", async () => {
    const { accessKey, calls } = await setupMocks();
    const flow = await runAuthorizationFlow({ accessKey });

    expect(flow.callbackResponse.status).toBe(302);
    const back = new URL(flow.callbackResponse.headers.get("Location") ?? "");
    expect(back.origin + back.pathname).toBe(CLIENT_REDIRECT);
    expect(back.searchParams.get("state")).toBe("client-state-1");
    expect(back.searchParams.get("code")).toBeTruthy();

    const tokenCalls = parseCalls(calls, KINTONE_TOKEN);
    expect(tokenCalls).toHaveLength(1);
    const tokenCall = calls.find((c) => c.url.endsWith("/oauth2/token"));
    expect(tokenCall?.headers.get("authorization")).toBe(
      `Basic ${btoa("test-kintone-client-id:test-kintone-client-secret")}`,
    );
    const form = new URLSearchParams(tokenCall?.body);
    expect(form.get("grant_type")).toBe("authorization_code");
    expect(form.get("code")).toBe("kintone-code");
    expect(form.get("redirect_uri")).toBe(`${ORIGIN}/callback`);
    expect(form.has("client_secret")).toBe(false);
    expect(tokenCall?.body).not.toContain("test-kintone-client-secret");
    const verifier = form.get("code_verifier") ?? "";
    expect(verifier).toMatch(/^[A-Za-z0-9_-]{43}$/);
    const challenge = new URL(flow.kintoneLocation).searchParams.get("code_challenge");
    expect(await sha256Base64url(verifier)).toBe(challenge);

    // 疎通確認: コード交換のあとに GET /k/v1/apps.json?limit=1 を、取得したアクセストークンで 1 回
    const appsCalls = parseCalls(calls, KINTONE_APPS);
    expect(appsCalls).toHaveLength(1);
    expect(new URL(appsCalls[0]?.url ?? "").searchParams.get("limit")).toBe("1");
    const appsCall = calls.find((c) => c.url.includes("/k/v1/apps.json"));
    expect(appsCall?.headers.get("authorization")).toBe("Bearer kintone-at-1");
    expect(calls.indexOf(tokenCall as never)).toBeLessThan(calls.indexOf(appsCall as never));
  });

  it("/token でアクセストークンを取得でき、kintone のトークンは MCP クライアントに渡らない", async () => {
    const { accessKey } = await setupMocks();
    const flow = await runAuthorizationFlow({ accessKey });
    expect(flow.tokenResponse.status).toBe(200);
    const tokens = (await flow.tokenResponse.json()) as TokenSet;
    expect(tokens.access_token).toBeTruthy();
    expect(tokens.refresh_token).toBeTruthy();
    expect(tokens.token_type.toLowerCase()).toBe("bearer");
    expect(tokens.expires_in).toBeLessThan(3600);
    expect(JSON.stringify(tokens)).not.toContain("kintone-at-1");
    expect(JSON.stringify(tokens)).not.toContain("kintone-rt-1");
  });

  it("KINTONE_OAUTH_PKCE=off では認可 URL に code_challenge が付かず、コード交換に code_verifier が送られない", async () => {
    const { accessKey, calls } = await setupMocks();
    useEnv({ KINTONE_OAUTH_PKCE: "off" });
    const flow = await runAuthorizationFlow({ accessKey });
    const p = new URL(flow.kintoneLocation).searchParams;
    expect(p.has("code_challenge")).toBe(false);
    expect(p.has("code_challenge_method")).toBe(false);
    expect(p.get("scope")).toBe(SCOPE_6);
    const tokenCall = calls.find((c) => c.url.endsWith("/oauth2/token"));
    expect(new URLSearchParams(tokenCall?.body).has("code_verifier")).toBe(false);
    expect(flow.tokenResponse.status).toBe(200);
  });

  it("同意画面で Deny すると access_denied でクライアントへ戻し、kintone へは行かない", async () => {
    const { accessKey, calls } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId, "s2");
    const jar = new CookieJar();
    const consent = await worker.fetch(url, { headers: await accessHeaders(accessKey) });
    jar.update(consent);
    const handle = /name="handle" value="([^"]+)"/.exec(await consent.text())?.[1] ?? "";
    const denied = await worker.fetch(`${ORIGIN}/authorize`, {
      method: "POST",
      headers: {
        Cookie: jar.header(),
        "Content-Type": "application/x-www-form-urlencoded",
        ...(await accessHeaders(accessKey)),
      },
      body: new URLSearchParams({ handle, decision: "deny" }),
    });
    expect(denied.status).toBe(302);
    const loc = new URL(denied.headers.get("Location") ?? "");
    expect(loc.origin + loc.pathname).toBe(CLIENT_REDIRECT);
    expect(loc.searchParams.get("error")).toBe("access_denied");
    expect(loc.searchParams.get("state")).toBe("s2");
    expect(calls.filter((c) => c.url.startsWith(KINTONE_ORIGIN))).toHaveLength(0);
  });

  describe("kintone 側の失敗はクライアントへエラーで戻す(認可コードは発行しない)", () => {
    async function runCallback(
      mockOpts: Parameters<typeof setupMocks>[0],
      query: (state: string) => string,
    ) {
      const { accessKey, calls } = await setupMocks(mockOpts);
      const clientId = await registerClient();
      const { url } = await authorizeUrl(clientId, "s3");
      const jar = new CookieJar();
      const consent = await worker.fetch(url, { headers: await accessHeaders(accessKey) });
      jar.update(consent);
      const handle = /name="handle" value="([^"]+)"/.exec(await consent.text())?.[1] ?? "";
      const up = await worker.fetch(`${ORIGIN}/authorize`, {
        method: "POST",
        headers: {
          Cookie: jar.header(),
          "Content-Type": "application/x-www-form-urlencoded",
          ...(await accessHeaders(accessKey)),
        },
        body: new URLSearchParams({ handle, decision: "approve" }),
      });
      jar.update(up);
      const state = new URL(up.headers.get("Location") ?? "").searchParams.get("state") ?? "";
      const cb = await worker.fetch(`${ORIGIN}/callback?${query(encodeURIComponent(state))}`, {
        headers: { Cookie: jar.header(), ...(await accessHeaders(accessKey)) },
      });
      expect(cb.status).toBe(302);
      const loc = new URL(cb.headers.get("Location") ?? "");
      expect(loc.origin + loc.pathname).toBe(CLIENT_REDIRECT);
      expect(loc.searchParams.get("state")).toBe("s3");
      expect(loc.searchParams.get("code")).toBeNull();
      return { loc, calls };
    }

    it("kintone が認可を拒否して error 付きで戻したら access_denied", async () => {
      const { loc, calls } = await runCallback({}, (s) => `error=access_denied&state=${s}`);
      expect(loc.searchParams.get("error")).toBe("access_denied");
      expect(calls.filter((c) => c.url.startsWith(KINTONE_ORIGIN))).toHaveLength(0);
    });

    it("code がなければ invalid_request", async () => {
      const { loc } = await runCallback({}, (s) => `state=${s}`);
      expect(loc.searchParams.get("error")).toBe("invalid_request");
    });

    it("コード交換が invalid_grant なら server_error(疎通確認は呼ばない)", async () => {
      const { loc, calls } = await runCallback(
        { kintoneToken: () => jsonResponse({ error: "invalid_grant" }, 400) },
        (s) => `code=c&state=${s}`,
      );
      expect(loc.searchParams.get("error")).toBe("server_error");
      expect(parseCalls(calls, KINTONE_APPS)).toHaveLength(0);
    });

    it("コード交換が本文なしの 401 でも server_error(invalid_grant と同じ結果。疎通確認は呼ばない)", async () => {
      const { loc, calls } = await runCallback(
        { kintoneToken: () => new Response("", { status: 401 }) },
        (s) => `code=c&state=${s}`,
      );
      expect(loc.searchParams.get("error")).toBe("server_error");
      expect(parseCalls(calls, KINTONE_APPS)).toHaveLength(0);
    });

    it("コード交換が 503 なら temporarily_unavailable", async () => {
      const { loc } = await runCallback(
        { kintoneToken: () => jsonResponse({}, 503) },
        (s) => `code=c&state=${s}`,
      );
      expect(loc.searchParams.get("error")).toBe("temporarily_unavailable");
    });

    it("疎通確認が 401(IP 制限・利用者の許可がない等)なら server_error", async () => {
      const { loc, calls } = await runCallback(
        { kintoneApps: () => jsonResponse({ code: "CB_AU01", message: "x" }, 401) },
        (s) => `code=c&state=${s}`,
      );
      expect(loc.searchParams.get("error")).toBe("server_error");
      expect(parseCalls(calls, KINTONE_APPS)).toHaveLength(1);
    });

    it("疎通確認が 503 なら temporarily_unavailable", async () => {
      const { loc } = await runCallback(
        { kintoneApps: () => jsonResponse({}, 503) },
        (s) => `code=c&state=${s}`,
      );
      expect(loc.searchParams.get("error")).toBe("temporarily_unavailable");
    });
  });
});

describe("E2E 2b: 利用者 ID は Access の email から決まる(ADR 0010)", () => {
  it("MCP トークンの userId は userIdFromEmail(email)。同じ email なら同じ、別の email なら別。`:` を含まない", async () => {
    const { accessKey } = await setupMocks();
    const a1 = await runAuthorizationFlow({ accessKey, email: "alice@example.com" });
    const a2 = await runAuthorizationFlow({ accessKey, email: "  Alice@Example.com " });
    const b = await runAuthorizationFlow({ accessKey, email: "bob@example.com" });
    const userIdOf = async (f: typeof a1) =>
      ((await f.tokenResponse.json()) as TokenSet).access_token.split(":")[0];
    const [idA1, idA2, idB] = [await userIdOf(a1), await userIdOf(a2), await userIdOf(b)];
    const expectedAlice = await userIdFromEmail("alice@example.com");
    expect(idA1).toBe(expectedAlice);
    expect(idA2).toBe(expectedAlice);
    expect(idB).toBe(await userIdFromEmail("bob@example.com"));
    expect(idB).not.toBe(expectedAlice);
    expect(expectedAlice).toMatch(/^[A-Za-z0-9_-]{43}$/);
  });

  it("生の email が MCP トークンにも KV のキーにも値にも現れず、kintone のトークンも props 以外に平文で残らない", async () => {
    const { accessKey } = await setupMocks();
    const flow = await runAuthorizationFlow({ accessKey, email: "leak.check@example.com" });
    const tokens = (await flow.tokenResponse.json()) as TokenSet;
    expect(JSON.stringify(tokens).toLowerCase()).not.toContain("leak.check");

    const listed = await env.OAUTH_KV.list();
    expect(listed.keys.length).toBeGreaterThan(0);
    for (const { name } of listed.keys) {
      expect(name.toLowerCase()).not.toContain("leak.check");
      const value = (await env.OAUTH_KV.get(name)) ?? "";
      expect(value.toLowerCase(), `KV value of ${name}`).not.toContain("leak.check");
      for (const secret of ["kintone-at-1", "kintone-rt-1", "test-kintone-client-secret"]) {
        expect(name).not.toContain(secret);
        expect(value, `KV value of ${name}`).not.toContain(secret);
      }
    }
  });
});

describe("E2E 3: 取得したトークンで /mcp が使える", () => {
  it("tools/list と tools/call kintone_apps_search が成功し、kintone へは grant の props のトークンで呼ぶ", async () => {
    const { accessKey, calls } = await setupMocks();
    const flow = await runAuthorizationFlow({ accessKey });
    const { access_token } = (await flow.tokenResponse.json()) as TokenSet;

    const list = await mcpRequest(access_token, "tools/list");
    expect(list.status).toBe(200);
    const listBody = await readRpc(list);
    expect((listBody.result?.tools ?? []).map((t) => t.name)).toEqual(["kintone_apps_search"]);

    const before = parseCalls(calls, KINTONE_APPS).length;
    const call = await mcpRequest(access_token, "tools/call", {
      name: "kintone_apps_search",
      arguments: { limit: 5 },
    });
    expect(call.status).toBe(200);
    const callBody = await readRpc(call);
    expect(callBody.result?.isError).toBeFalsy();
    expect(JSON.parse(firstText(callBody))).toEqual({
      apps: [{ appId: "1", code: "CRM", name: "顧客管理" }],
    });
    const appsCalls = calls.filter((c) => c.url.includes("/k/v1/apps.json"));
    expect(appsCalls.length).toBe(before + 1);
    const last = appsCalls.at(-1);
    expect(last?.headers.get("authorization")).toBe("Bearer kintone-at-1");
    expect(new URL(last?.url ?? "").searchParams.get("limit")).toBe("5");
  });

  it("不正なトークンの /mcp は 401", async () => {
    await setupMocks();
    const res = await mcpRequest("not-a-valid-token", "tools/list");
    expect(res.status).toBe(401);
    expect(res.headers.get("WWW-Authenticate")).toContain("Bearer");
  });
});
