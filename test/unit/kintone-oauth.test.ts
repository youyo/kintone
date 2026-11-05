import { afterEach, describe, expect, it, vi } from "vitest";
import {
  buildAuthorizeUrl,
  exchangeCode,
  KINTONE_SCOPES,
  type KintoneOAuthConfig,
  KintoneOAuthError,
  refreshTokens,
} from "../../src/auth/kintone-oauth";
import { jsonResponse, mockOutboundFetch } from "../helpers/mock-fetch";

const TOKEN = "POST https://example.cybozu.com/oauth2/token";
const cfg: KintoneOAuthConfig = {
  host: "example.cybozu.com",
  clientId: "test-client-id",
  clientSecret: "test-client-secret",
  pkce: "s256",
};
const REDIRECT = "https://kintone-mcp.test/callback";

afterEach(() => vi.restoreAllMocks());

describe("buildAuthorizeUrl", () => {
  it("kintone の認可エンドポイントに、全パラメーター(PKCE S256 と scope 6 つのスペース区切り)を付ける", () => {
    const url = new URL(
      buildAuthorizeUrl(cfg, { redirectUri: REDIRECT, state: "st-1", codeChallenge: "chal-1" }),
    );
    expect(url.origin + url.pathname).toBe("https://example.cybozu.com/oauth2/authorization");
    const p = url.searchParams;
    expect(p.get("response_type")).toBe("code");
    expect(p.get("client_id")).toBe("test-client-id");
    expect(p.get("redirect_uri")).toBe(REDIRECT);
    expect(p.get("state")).toBe("st-1");
    expect(p.get("code_challenge")).toBe("chal-1");
    expect(p.get("code_challenge_method")).toBe("S256");
    expect(p.get("scope")).toBe(
      "k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write",
    );
    expect(p.get("scope")?.split(" ")).toEqual([...KINTONE_SCOPES]);
    expect(KINTONE_SCOPES).toHaveLength(6);
  });

  it("client_secret を URL に載せない", () => {
    const url = buildAuthorizeUrl(cfg, { redirectUri: REDIRECT, state: "s", codeChallenge: "c" });
    expect(url).not.toContain("test-client-secret");
    expect(new URL(url).searchParams.get("client_secret")).toBeNull();
  });

  it("pkce=off では code_challenge / code_challenge_method を付けない", () => {
    const url = new URL(
      buildAuthorizeUrl({ ...cfg, pkce: "off" }, { redirectUri: REDIRECT, state: "s" }),
    );
    expect(url.searchParams.get("code_challenge")).toBeNull();
    expect(url.searchParams.get("code_challenge_method")).toBeNull();
    expect(url.searchParams.get("scope")).toContain("k:app_record:read");
  });
});

describe("exchangeCode", () => {
  const okBody = {
    access_token: "kt-access-1",
    refresh_token: "kt-refresh-1",
    token_type: "Bearer",
    expires_in: 3600,
  };

  it("Basic 認証ヘッダー + フォームで送る。フォームに client_secret は入らず、redirect_uri と code_verifier が入る", async () => {
    vi.spyOn(console, "log").mockImplementation(() => {});
    const m = mockOutboundFetch({ [TOKEN]: () => jsonResponse(okBody) });
    const tokens = await exchangeCode(
      cfg,
      { code: "the-code", redirectUri: REDIRECT, codeVerifier: "the-verifier" },
      1_000_000,
    );
    const call = m.callsTo(TOKEN)[0];
    expect(call?.headers.get("authorization")).toBe(
      `Basic ${btoa("test-client-id:test-client-secret")}`,
    );
    expect(call?.headers.get("content-type")).toContain("application/x-www-form-urlencoded");
    const form = new URLSearchParams(call?.body);
    expect(Object.fromEntries(form)).toEqual({
      grant_type: "authorization_code",
      code: "the-code",
      redirect_uri: REDIRECT,
      code_verifier: "the-verifier",
    });
    expect(call?.body).not.toContain("test-client-secret");
    expect(tokens).toEqual({
      accessToken: "kt-access-1",
      refreshToken: "kt-refresh-1",
      expiresAt: 1_000_000 + 3600_000,
    });
  });

  it("pkce=off では code_verifier を送らない", async () => {
    vi.spyOn(console, "log").mockImplementation(() => {});
    const m = mockOutboundFetch({ [TOKEN]: () => jsonResponse(okBody) });
    await exchangeCode({ ...cfg, pkce: "off" }, { code: "c", redirectUri: REDIRECT });
    expect(new URLSearchParams(m.callsTo(TOKEN)[0]?.body).has("code_verifier")).toBe(false);
  });

  it("refresh_token がない応答は invalid_response", async () => {
    vi.spyOn(console, "log").mockImplementation(() => {});
    mockOutboundFetch({
      [TOKEN]: () => jsonResponse({ access_token: "a", token_type: "Bearer", expires_in: 3600 }),
    });
    await expect(exchangeCode(cfg, { code: "c", redirectUri: REDIRECT })).rejects.toMatchObject({
      kind: "invalid_response",
    });
  });
});

describe("refreshTokens", () => {
  it("リフレッシュ応答に refresh_token がなければ元のものを維持する", async () => {
    vi.spyOn(console, "log").mockImplementation(() => {});
    const m = mockOutboundFetch({
      [TOKEN]: () =>
        jsonResponse({ access_token: "kt-access-2", token_type: "Bearer", expires_in: 3599 }),
    });
    const tokens = await refreshTokens(cfg, "kt-refresh-1", 5_000);
    expect(tokens).toEqual({
      accessToken: "kt-access-2",
      refreshToken: "kt-refresh-1",
      expiresAt: 5_000 + 3599_000,
    });
    const call = m.callsTo(TOKEN)[0];
    expect(Object.fromEntries(new URLSearchParams(call?.body))).toEqual({
      grant_type: "refresh_token",
      refresh_token: "kt-refresh-1",
    });
    expect(call?.headers.get("authorization")).toBe(
      `Basic ${btoa("test-client-id:test-client-secret")}`,
    );
  });

  it("応答に refresh_token があれば差し替える", async () => {
    vi.spyOn(console, "log").mockImplementation(() => {});
    mockOutboundFetch({
      [TOKEN]: () =>
        jsonResponse({
          access_token: "kt-access-2",
          refresh_token: "kt-refresh-2",
          token_type: "Bearer",
          expires_in: 3600,
        }),
    });
    expect((await refreshTokens(cfg, "kt-refresh-1")).refreshToken).toBe("kt-refresh-2");
  });

  it("診断ログ kintone_token は値を出さず、ローテーションの有無だけを出す", async () => {
    const lines: string[] = [];
    vi.spyOn(console, "log").mockImplementation((...a: unknown[]) => {
      lines.push(a.map(String).join(" "));
    });
    mockOutboundFetch({
      [TOKEN]: () =>
        jsonResponse({
          access_token: "kt-access-9",
          refresh_token: "kt-refresh-9",
          expires_in: 3600,
        }),
    });
    await refreshTokens(cfg, "kt-refresh-1");
    const event = JSON.parse(lines[0] ?? "{}");
    expect(event).toEqual({
      event: "kintone_token",
      grant_type: "refresh_token",
      expires_in: 3600,
      refresh_token_rotated: true,
    });
    expect(lines.join("\n")).not.toContain("kt-access-9");
    expect(lines.join("\n")).not.toContain("kt-refresh-9");
  });
});

describe("エラー分類", () => {
  const run = () => refreshTokens(cfg, "kt-refresh-1");
  const classify = async (response: () => Response | Promise<Response>) => {
    vi.spyOn(console, "log").mockImplementation(() => {});
    mockOutboundFetch({ [TOKEN]: response });
    const err = await run().then(
      () => null,
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(KintoneOAuthError);
    return err as KintoneOAuthError;
  };

  it.each([
    ["400 invalid_grant", () => jsonResponse({ error: "invalid_grant" }, 400)],
    ["401 invalid_grant", () => jsonResponse({ error: "invalid_grant" }, 401)],
  ])("invalid_grant: %s", async (_name, response) => {
    expect((await classify(response)).kind).toBe("invalid_grant");
  });

  // 前段のプロキシや WAF が返す本文なしの 4xx で grant を失効させない(失効すると利用者が再認可になる)。
  it.each([
    ["400 で本文が JSON でない", () => new Response("<html>bad</html>", { status: 400 })],
    ["401 で本文が空", () => new Response("", { status: 401 })],
    ["400 で error が文字列でない", () => jsonResponse({ error: 5 }, 400)],
    ["401 で error がない", () => jsonResponse({ message: "denied" }, 401)],
    ["400 で本文が null", () => jsonResponse(null, 400)],
  ])("rejected(grant は失効させない): %s", async (_name, response) => {
    expect((await classify(response)).kind).toBe("rejected");
  });

  it.each([
    ["invalid_client", 401],
    ["unauthorized_client", 400],
    ["invalid_client", 400],
  ])("運用者の設定ミス(grant は失効させない): error=%s status=%i", async (error, status) => {
    expect((await classify(() => jsonResponse({ error }, status))).kind).toBe("invalid_client");
  });

  it.each([500, 502, 503, 429])("一時障害: HTTP %i は temporarily_unavailable", async (status) => {
    expect((await classify(() => jsonResponse({}, status))).kind).toBe("temporarily_unavailable");
  });

  it("通信失敗(fetch が例外)は temporarily_unavailable", async () => {
    expect((await classify(() => Promise.reject(new Error("network down")))).kind).toBe(
      "temporarily_unavailable",
    );
  });

  it("成功ステータスで JSON でない応答は invalid_response", async () => {
    expect((await classify(() => new Response("OK", { status: 200 }))).kind).toBe(
      "invalid_response",
    );
  });

  it("上記以外の 4xx(invalid_request など)は rejected(再試行で直らない設定・互換性の問題)", async () => {
    expect((await classify(() => jsonResponse({ error: "invalid_request" }, 400))).kind).toBe(
      "rejected",
    );
    expect((await classify(() => jsonResponse({}, 403))).kind).toBe("rejected");
  });

  it("トークン・シークレットがエラーメッセージとログに出ない(応答本文に含まれていても)", async () => {
    const lines: string[] = [];
    for (const level of ["log", "info", "warn", "error"] as const) {
      vi.spyOn(console, level).mockImplementation((...a: unknown[]) => {
        lines.push(a.map(String).join(" "));
      });
    }
    const leaky = {
      error: "invalid_grant",
      error_description:
        "refresh_token kt-refresh-1 access_token kt-access-leak test-client-secret",
    };
    for (const response of [
      () => jsonResponse(leaky, 400),
      () => jsonResponse(leaky, 500),
      () => new Response("kt-access-leak test-client-secret", { status: 200 }),
    ]) {
      mockOutboundFetch({ [TOKEN]: response });
      const err = (await run().then(
        () => null,
        (e: unknown) => e,
      )) as KintoneOAuthError;
      for (const secret of ["kt-refresh-1", "kt-access-leak", "test-client-secret"]) {
        expect(err.message).not.toContain(secret);
        expect(String(err.stack)).not.toContain(secret);
      }
      vi.mocked(globalThis.fetch).mockRestore();
    }
    for (const secret of ["kt-refresh-1", "kt-access-leak", "test-client-secret"]) {
      expect(lines.join("\n")).not.toContain(secret);
    }
  });
});
