import { afterEach, describe, expect, it, vi } from "vitest";
import {
  accessHeaders,
  authorizeUrl,
  CookieJar,
  KINTONE_ORIGIN,
  MCP_URL,
  ORIGIN,
  pkcePair,
  registerClient,
  setupMocks,
  worker,
} from "../helpers/oauth-flow";

afterEach(() => {
  vi.restoreAllMocks();
});

const kintoneCalls = (calls: { url: string }[]) =>
  calls.filter((c) => c.url.startsWith(KINTONE_ORIGIN));

describe("E2E 6: 認可フローの入力検証(Access の JWT 付き)", () => {
  it("client_name に <script> を入れて登録しても、同意画面ではエスケープされる", async () => {
    const { accessKey } = await setupMocks();
    const payload = "<script>alert(1)</script>";
    const clientId = await registerClient(payload);
    const { url } = await authorizeUrl(clientId, "s");
    const res = await worker.fetch(url, { headers: await accessHeaders(accessKey) });
    expect(res.status).toBe(200);
    const html = await res.text();
    expect(html).not.toContain("<script>");
    expect(html).not.toContain(payload);
    expect(html).toContain("&#60;script&#62;alert(1)&#60;/script&#62;");
  });

  it("Access の email が <script> を含む文字列でも、同意画面ではエスケープされる", async () => {
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId, "s");
    const res = await worker.fetch(url, {
      headers: await accessHeaders(accessKey, { email: "<script>alert(2)</script>@example.com" }),
    });
    expect(res.status).toBe(200);
    const html = await res.text();
    expect(html).not.toContain("<script>alert(2)");
    expect(html).toContain("&#60;script&#62;alert(2)");
  });

  it("偽造した state の /callback は拒否し、kintone のトークン交換をしない", async () => {
    const { accessKey, calls } = await setupMocks();
    const res = await worker.fetch(`${ORIGIN}/callback?code=kintone-code&state=forged-state`, {
      headers: await accessHeaders(accessKey),
    });
    expect(res.status).toBeGreaterThanOrEqual(400);
    expect(res.status).toBeLessThan(500);
    expect(res.headers.get("Location")).toBeNull();
    expect(kintoneCalls(calls)).toHaveLength(0);
  });

  it("同意後に発行された正規の state でも、Cookie なし(別ブラウザ)の /callback は拒否し、kintone のトークン交換をしない", async () => {
    const { accessKey, calls } = await setupMocks();
    const clientId = await registerClient();
    const { url } = await authorizeUrl(clientId, "s");
    const jar = new CookieJar();
    const consent = await worker.fetch(url, { headers: await accessHeaders(accessKey) });
    jar.update(consent);
    const handle = /name="handle" value="([^"]+)"/.exec(await consent.text())?.[1] ?? "";
    expect(handle).not.toBe("");
    const approve = await worker.fetch(`${ORIGIN}/authorize`, {
      method: "POST",
      headers: {
        Cookie: jar.header(),
        "Content-Type": "application/x-www-form-urlencoded",
        ...(await accessHeaders(accessKey)),
      },
      body: new URLSearchParams({ handle, decision: "approve" }),
    });
    const state =
      new URL(approve.headers.get("Location") ?? "", ORIGIN).searchParams.get("state") ?? "";
    expect(state).not.toBe("");

    // 正規の state だが Cookie を持たない別ブラウザからの /callback
    const res = await worker.fetch(
      `${ORIGIN}/callback?code=kintone-code&state=${encodeURIComponent(state)}`,
      { headers: await accessHeaders(accessKey) },
    );
    expect(res.status).toBeGreaterThanOrEqual(400);
    expect(res.status).toBeLessThan(500);
    expect(res.headers.get("Location")).toBeNull();
    expect(kintoneCalls(calls)).toHaveLength(0);
  });

  it("未登録の redirect_uri の /authorize は同意画面を出さず拒否する(クライアントへもリダイレクトしない)", async () => {
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const { challenge } = await pkcePair();
    const url = new URL(`${ORIGIN}/authorize`);
    url.search = new URLSearchParams({
      response_type: "code",
      client_id: clientId,
      redirect_uri: "https://evil.example/cb",
      state: "s",
      code_challenge: challenge,
      code_challenge_method: "S256",
      resource: MCP_URL,
    }).toString();
    const res = await worker.fetch(url.toString(), { headers: await accessHeaders(accessKey) });
    expect(res.status).toBe(400);
    expect(res.headers.get("Location")).toBeNull();
    expect(await res.text()).not.toContain('name="handle"');
  });

  it("未登録の client_id の /authorize も同意画面を出さない", async () => {
    const { accessKey } = await setupMocks();
    const { url } = await authorizeUrl("no-such-client", "s");
    const res = await worker.fetch(url, { headers: await accessHeaders(accessKey) });
    expect(res.status).toBe(400);
    expect(await res.text()).not.toContain('name="handle"');
  });

  it("POST /authorize の handle が不正なら承認されず、kintone へ送らない", async () => {
    const { accessKey, calls } = await setupMocks();
    const res = await worker.fetch(`${ORIGIN}/authorize`, {
      method: "POST",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        ...(await accessHeaders(accessKey)),
      },
      body: new URLSearchParams({ handle: "forged-handle", decision: "approve" }),
    });
    expect(res.status).toBeGreaterThanOrEqual(400);
    expect(res.status).toBeLessThan(500);
    expect(res.headers.get("Location") ?? "").not.toContain("oauth2/authorization");
    expect(kintoneCalls(calls)).toHaveLength(0);
  });
});
