import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createTestKey, signAccessJwt, signHs256Jwt } from "../helpers/access";
import {
  accessHeaders,
  authorizeUrl,
  CookieJar,
  KINTONE_ORIGIN,
  MCP_URL,
  ORIGIN,
  registerClient,
  resetEnv,
  runAuthorizationFlow,
  setupMocks,
  useEnv,
  worker,
} from "../helpers/oauth-flow";

afterEach(() => {
  resetEnv();
  vi.restoreAllMocks();
});

const kintoneCalls = (calls: { url: string }[]) =>
  calls.filter((c) => c.url.startsWith(KINTONE_ORIGIN));

const grantKeys = async () =>
  (await env.OAUTH_KV.list({ prefix: "grant:" })).keys.map((k) => k.name);

function postApprove(handle: string, headers: Record<string, string>) {
  return worker.fetch(`${ORIGIN}/authorize`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded", ...headers },
    body: new URLSearchParams({ handle, decision: "approve" }),
  });
}

describe("E2E 7: /authorize と /callback は正しい Cf-Access-Jwt-Assertion がないと 403(ADR 0010)", () => {
  it("JWT なしの GET /authorize・POST /authorize・GET /callback はすべて 403 で、kintone は呼ばれない", async () => {
    const { calls } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId);
    expect((await worker.fetch(url)).status).toBe(403);
    expect((await postApprove("x", {})).status).toBe(403);
    expect((await worker.fetch(`${ORIGIN}/callback?code=c&state=s`)).status).toBe(403);
    expect(kintoneCalls(calls)).toHaveLength(0);
  });

  it("不正な JWT(期限切れ・aud 違い・偽署名・別チーム・email なし・exp なし・sub なし・HS256・JWT でない)は 3 経路すべてで 403", async () => {
    const { accessKey, calls } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId);
    const past = Math.floor(Date.now() / 1000) - 3600;
    const rogue = await createTestKey(accessKey.kid); // 同じ kid の別の鍵
    const bad = [
      await signAccessJwt(accessKey, { iat: past, exp: past + 300 }),
      await signAccessJwt(accessKey, { aud: "another-app" }),
      await signAccessJwt(rogue),
      await signAccessJwt(accessKey, { teamDomain: "evil.cloudflareaccess.com" }),
      await signAccessJwt(accessKey, { omit: ["email"] }),
      await signAccessJwt(accessKey, { email: "" }),
      await signAccessJwt(accessKey, { omit: ["exp"] }),
      await signAccessJwt(accessKey, { omit: ["sub"] }),
      await signHs256Jwt(accessKey),
      "not-a-jwt",
    ];
    for (const [i, jwt] of bad.entries()) {
      const h = { "Cf-Access-Jwt-Assertion": jwt };
      expect((await worker.fetch(url, { headers: h })).status, `GET authorize #${i}`).toBe(403);
      expect((await postApprove("x", h)).status, `POST authorize #${i}`).toBe(403);
      expect(
        (await worker.fetch(`${ORIGIN}/callback?code=c&state=s`, { headers: h })).status,
        `callback #${i}`,
      ).toBe(403);
    }
    expect(kintoneCalls(calls)).toHaveLength(0);
  });

  it("JWKS を取得できないときは 3 経路すべてで 503(fail closed)で、kintone は呼ばれない", async () => {
    const { accessKey, calls } = await setupMocks({
      jwks: () => new Response("boom", { status: 500 }),
    });
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId);
    const h = await accessHeaders(accessKey);
    expect((await worker.fetch(url, { headers: h })).status).toBe(503);
    expect((await postApprove("x", h)).status).toBe(503);
    expect((await worker.fetch(`${ORIGIN}/callback?code=c&state=s`, { headers: h })).status).toBe(
      503,
    );
    expect(kintoneCalls(calls)).toHaveLength(0);
  });

  it("403 / 503 の応答に JWT も email も理由も出さない", async () => {
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId);
    const jwt = await signAccessJwt(accessKey, { aud: "another-app", email: "leak@example.com" });
    const res = await worker.fetch(url, { headers: { "Cf-Access-Jwt-Assertion": jwt } });
    const text = await res.text();
    expect(res.status).toBe(403);
    expect(text).not.toContain(jwt);
    expect(text).not.toContain("leak@example.com");
    expect(text.toLowerCase()).not.toContain("aud");
  });

  it("ログには拒否の種別だけを出し、JWT・email は出さない", async () => {
    const lines: string[] = [];
    for (const level of ["log", "info", "warn", "error"] as const) {
      vi.spyOn(console, level).mockImplementation((...a: unknown[]) => {
        lines.push(a.map(String).join(" "));
      });
    }
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId);
    const jwt = await signAccessJwt(accessKey, { aud: "another-app", email: "leak@example.com" });
    await worker.fetch(url, { headers: { "Cf-Access-Jwt-Assertion": jwt } });
    const all = lines.join("\n");
    expect(all).toContain("access_jwt_rejected");
    expect(all).not.toContain(jwt);
    expect(all).not.toContain("leak@example.com");
  });

  it("正しい JWT なら /authorize は同意画面(200)を返し、画面に email が出る", async () => {
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId);
    const res = await worker.fetch(url, {
      headers: await accessHeaders(accessKey, { email: "carol@example.com" }),
    });
    expect(res.status).toBe(200);
    expect(await res.text()).toContain("carol@example.com");
  });

  it("aud が配列で ACCESS_AUD を含む JWT も通る", async () => {
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId);
    const res = await worker.fetch(url, {
      headers: await accessHeaders(accessKey, { aud: ["other", "test-access-aud"] }),
    });
    expect(res.status).toBe(200);
  });

  it("/mcp・/token・/register・/.well-known/* と /healthz は Access の JWT なしで到達できる(Worker 発行トークンで守る)", async () => {
    await setupMocks();
    expect((await worker.fetch(`${ORIGIN}/healthz`)).status).toBe(200);
    expect((await worker.fetch(`${ORIGIN}/.well-known/oauth-authorization-server`)).status).toBe(
      200,
    );
    expect((await worker.fetch(`${ORIGIN}/.well-known/oauth-protected-resource/mcp`)).status).toBe(
      200,
    );
    expect((await registerClient()).length).toBeGreaterThan(0);
    // /token は JWT 不要(未登録クライアントなので invalid_client の 401 になるが、Access の 403 ではない)
    const token = await worker.fetch(`${ORIGIN}/token`, {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({ grant_type: "authorization_code", code: "x", client_id: "y" }),
    });
    expect(token.status).toBe(401);
    // /mcp はトークンなしで 401(403 ではない)
    expect((await worker.fetch(MCP_URL, { method: "POST", body: "{}" })).status).toBe(401);
  });
});

describe("E2E 7b: /callback の Access の email が /authorize と違う・欠けているとき", () => {
  async function beginAuthorization(
    accessKey: Awaited<ReturnType<typeof setupMocks>>["accessKey"],
  ) {
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId);
    const jar = new CookieJar();
    const consent = await worker.fetch(url, {
      headers: await accessHeaders(accessKey, { email: "alice@example.com" }),
    });
    jar.update(consent);
    const handle = /name="handle" value="([^"]+)"/.exec(await consent.text())?.[1] ?? "";
    const up = await postApprove(handle, {
      Cookie: jar.header(),
      ...(await accessHeaders(accessKey, { email: "alice@example.com" })),
    });
    jar.update(up);
    const state = new URL(up.headers.get("Location") ?? "").searchParams.get("state") ?? "";
    return { jar, state };
  }

  it("email A で始めた state を email B の JWT で /callback に送ると 403、コード交換は呼ばれず、grant は作られない", async () => {
    const { accessKey, calls } = await setupMocks();
    const { jar, state } = await beginAuthorization(accessKey);
    const before = await grantKeys();
    const res = await worker.fetch(
      `${ORIGIN}/callback?code=kintone-code&state=${encodeURIComponent(state)}`,
      {
        headers: {
          Cookie: jar.header(),
          ...(await accessHeaders(accessKey, { email: "mallory@example.com" })),
        },
      },
    );
    expect(res.status).toBe(403);
    expect(res.headers.get("Location")).toBeNull();
    expect(kintoneCalls(calls)).toHaveLength(0);
    expect(await grantKeys()).toEqual(before);

    // state は使い切られているので、正しい email で送り直しても進まない
    const retry = await worker.fetch(
      `${ORIGIN}/callback?code=kintone-code&state=${encodeURIComponent(state)}`,
      {
        headers: {
          Cookie: jar.header(),
          ...(await accessHeaders(accessKey, { email: "alice@example.com" })),
        },
      },
    );
    expect(retry.status).toBeGreaterThanOrEqual(400);
    expect(kintoneCalls(calls)).toHaveLength(0);
    expect(await grantKeys()).toEqual(before);
  });

  it("大文字小文字・前後の空白だけが違う同じ email は一致として扱う", async () => {
    const { accessKey } = await setupMocks();
    const flow = await runAuthorizationFlow({
      accessKey,
      email: "alice@example.com",
      callbackEmail: "  ALICE@example.com ",
    });
    expect(flow.callbackResponse.status).toBe(302);
    expect(flow.tokenResponse.status).toBe(200);
  });

  it("runAuthorizationFlow で email が違うと、MCP の認可コードは発行されず /token は失敗する", async () => {
    const { accessKey, calls } = await setupMocks();
    const flow = await runAuthorizationFlow({
      accessKey,
      email: "alice@example.com",
      callbackEmail: "mallory@example.com",
    });
    expect(flow.callbackResponse.status).toBe(403);
    expect(flow.tokenResponse.status).toBeGreaterThanOrEqual(400);
    expect(kintoneCalls(calls)).toHaveLength(0);
  });

  it("/callback で JWT が欠けても 403(state を使い切らずに弾く)", async () => {
    const { accessKey, calls } = await setupMocks();
    const { jar, state } = await beginAuthorization(accessKey);
    const res = await worker.fetch(
      `${ORIGIN}/callback?code=kintone-code&state=${encodeURIComponent(state)}`,
      { headers: { Cookie: jar.header() } },
    );
    expect(res.status).toBe(403);
    expect(kintoneCalls(calls)).toHaveLength(0);

    // JWT の検証は state の消費より前なので、同じ state を正しい JWT で送り直せば進む
    const retry = await worker.fetch(
      `${ORIGIN}/callback?code=kintone-code&state=${encodeURIComponent(state)}`,
      {
        headers: {
          Cookie: jar.header(),
          ...(await accessHeaders(accessKey, { email: "alice@example.com" })),
        },
      },
    );
    expect(retry.status).toBe(302);
    expect(new URL(retry.headers.get("Location") ?? "").searchParams.get("code")).toBeTruthy();
  });
});

describe("E2E 7c: ACCESS_* が未設定の Worker では認可が通らない(UUID へのフォールバックの回帰)", () => {
  it.each(["ACCESS_TEAM_DOMAIN", "ACCESS_AUD"] as const)(
    "%s がないと、正しい形の JWT を付けても /authorize は 500 server_misconfigured で、同意画面も出ない",
    async (name) => {
      vi.spyOn(console, "error").mockImplementation(() => {});
      const { accessKey, calls } = await setupMocks();
      const clientId = await registerClient();
      const { url } = await authorizeUrl(clientId);
      useEnv({ [name]: undefined });
      const res = await worker.fetch(url, { headers: await accessHeaders(accessKey) });
      expect(res.status).toBe(500);
      expect(await res.json()).toEqual({ error: "server_misconfigured" });
      const post = await postApprove("x", await accessHeaders(accessKey));
      expect(post.status).toBe(500);
      const cb = await worker.fetch(`${ORIGIN}/callback?code=c&state=s`, {
        headers: await accessHeaders(accessKey),
      });
      expect(cb.status).toBe(500);
      expect(kintoneCalls(calls)).toHaveLength(0);
    },
  );
});
