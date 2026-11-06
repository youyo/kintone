import { env } from "cloudflare:workers";
import { GrantType, OAuthError } from "@cloudflare/workers-oauth-provider";
import { afterEach, describe, expect, it, vi } from "vitest";
import { isKintoneStale, kintoneStaleKey, markKintoneStale } from "../../src/auth/kintone-stale";
import type { GrantProps } from "../../src/auth/props";
import {
  createTokenExchangeCallback,
  KINTONE_EXPIRY_GRACE_SECONDS,
  MCP_ACCESS_TOKEN_TTL_SECONDS,
  MCP_CLIENT_REGISTRATION_TTL_SECONDS,
  MCP_REFRESH_TOKEN_TTL_SECONDS,
} from "../../src/auth/token-exchange";
import { jsonResponse, mockOutboundFetch } from "../helpers/mock-fetch";

const TOKEN_ENDPOINT = "POST https://example.cybozu.com/oauth2/token";
const NOW = 1_800_000_000_000;
const callback = createTokenExchangeCallback({ now: () => NOW });

function propsExpiringIn(seconds: number): GrantProps {
  return {
    kintoneAccessToken: "old-at",
    kintoneRefreshToken: "old-rt",
    kintoneExpiresAt: NOW + seconds * 1000,
  };
}

function call(grantType: GrantType, props: GrantProps, grantId = "g") {
  return callback({
    grantType,
    props,
    env,
    clientId: "c",
    subjectClientId: "c",
    userId: "42",
    grantId,
    scope: [],
    requestedScope: [],
    resource: "https://kintone-mcp.test/mcp",
  });
}

afterEach(() => vi.restoreAllMocks());

describe("tokenExchangeCallback", () => {
  it("MCP のアクセストークンは kintone のアクセストークン(約 1 時間)より短い", () => {
    expect(MCP_ACCESS_TOKEN_TTL_SECONDS).toBeLessThan(3600);
  });

  it("refresh_token: kintone の期限が切れていれば kintone も更新し props を差し替える", async () => {
    const m = mockOutboundFetch({
      [TOKEN_ENDPOINT]: () =>
        jsonResponse({ access_token: "new-at", refresh_token: "new-rt", expires_in: 3600 }),
    });
    const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(-10)); // 期限切れ
    expect(m.calls).toHaveLength(1);
    expect(new URLSearchParams(m.calls[0]?.body).get("refresh_token")).toBe("old-rt");
    expect(result?.newProps).toMatchObject({
      kintoneAccessToken: "new-at",
      kintoneRefreshToken: "new-rt",
      kintoneExpiresAt: NOW + 3600 * 1000,
    });
    expect(result?.accessTokenProps).toEqual(result?.newProps);
    expect(result?.accessTokenTTL).toBe(MCP_ACCESS_TOKEN_TTL_SECONDS);
  });

  it("refresh_token: kintone がリフレッシュトークンをローテーションしない場合は元のものを維持する", async () => {
    mockOutboundFetch({
      [TOKEN_ENDPOINT]: () => jsonResponse({ access_token: "new-at", expires_in: 3600 }),
    });
    const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0));
    const next = result?.newProps as GrantProps | undefined;
    expect(next?.kintoneRefreshToken).toBe("old-rt");
    expect(next?.kintoneAccessToken).toBe("new-at");
  });

  it("refresh_token: kintone の期限に十分な余裕があれば kintone を呼ばない", async () => {
    const m = mockOutboundFetch({});
    const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(3500));
    expect(m.calls).toHaveLength(0);
    expect(result?.newProps).toBeUndefined();
  });

  it("MCP のトークンの寿命は kintone のトークンの残り寿命を超えない", async () => {
    const m = mockOutboundFetch({
      [TOKEN_ENDPOINT]: () => jsonResponse({ access_token: "new-at", expires_in: 600 }),
    });
    const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0));
    expect(m.calls).toHaveLength(1);
    // 600 秒 - 余裕 60 秒
    expect(result?.accessTokenTTL).toBe(540);
  });

  describe("kintone が期限内のリフレッシュで同じトークンを残り寿命つきで返す場合(実挙動は未検証 V01。返しても壊れない。ADR 0005)", () => {
    /** 想定モード A: 同じアクセストークンを、残り秒数を expires_in として返す(ローテーションなし)。 */
    const sameTokenWithRemaining = (seconds: number) => () =>
      jsonResponse({ access_token: "old-at", expires_in: seconds });

    it("残りが MCP の寿命より短くても、期限内なら kintone を呼ばず、MCP の寿命を kintone の残り + 余裕にする", async () => {
      const m = mockOutboundFetch({ [TOKEN_ENDPOINT]: sameTokenWithRemaining(600) });
      const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(600));
      expect(m.calls).toHaveLength(0);
      expect(result?.newProps).toBeUndefined();
      expect(result?.accessTokenTTL).toBe(600 + KINTONE_EXPIRY_GRACE_SECONDS);
    });

    it("残りが 1 分を切っていても期限内なら kintone を呼ばない(MCP の寿命は KV の最小 60 秒)", async () => {
      const m = mockOutboundFetch({ [TOKEN_ENDPOINT]: sameTokenWithRemaining(30) });
      const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(30));
      expect(m.calls).toHaveLength(0);
      expect(result?.accessTokenTTL).toBe(60);
    });

    it("期限ちょうど(残り 0)は期限切れとして kintone を更新する", async () => {
      const m = mockOutboundFetch({
        [TOKEN_ENDPOINT]: () => jsonResponse({ access_token: "new-at", expires_in: 3600 }),
      });
      await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0));
      expect(m.calls).toHaveLength(1);
    });

    it("stale 印で更新して同じトークンが返ったら、MCP の寿命は kintone の残り + 余裕(印は消す)", async () => {
      await markKintoneStale(env.OAUTH_KV, "42", "g-same-1");
      const m = mockOutboundFetch({ [TOKEN_ENDPOINT]: sameTokenWithRemaining(1755) });
      const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(1760), "g-same-1");
      expect(m.calls).toHaveLength(1);
      // 期限は kintone が返した残り寿命で保存し直す(延びていない)
      expect(result?.newProps).toMatchObject({
        kintoneAccessToken: "old-at",
        kintoneExpiresAt: NOW + 1755 * 1000,
      });
      expect(result?.accessTokenTTL).toBe(1755 + KINTONE_EXPIRY_GRACE_SECONDS);
      expect(await isKintoneStale(env.OAUTH_KV, "42", "g-same-1")).toBe(false);
    });

    it("同じトークンが残り長めで返っても、MCP の寿命の上限は MCP_ACCESS_TOKEN_TTL_SECONDS", async () => {
      await markKintoneStale(env.OAUTH_KV, "42", "g-same-2");
      mockOutboundFetch({ [TOKEN_ENDPOINT]: sameTokenWithRemaining(3000) });
      const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(3000), "g-same-2");
      expect(result?.accessTokenTTL).toBe(MCP_ACCESS_TOKEN_TTL_SECONDS);
    });

    it("期限切れ後の更新でも同じトークン(残り 0 以下)が返ったら、MCP の寿命は最小の 60 秒(1 分後に再試行)", async () => {
      mockOutboundFetch({ [TOKEN_ENDPOINT]: sameTokenWithRemaining(0) });
      const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(-5));
      expect(result?.accessTokenTTL).toBe(60);
    });

    it("余裕は小さい(数秒)", () => {
      expect(KINTONE_EXPIRY_GRACE_SECONDS).toBeGreaterThan(0);
      expect(KINTONE_EXPIRY_GRACE_SECONDS).toBeLessThanOrEqual(10);
    });
  });

  it("authorization_code: 取りたての kintone トークンはそのまま使い、MCP の寿命だけ設定する", async () => {
    const m = mockOutboundFetch({});
    const result = await call(GrantType.AUTHORIZATION_CODE, propsExpiringIn(3600));
    expect(m.calls).toHaveLength(0);
    expect(result?.accessTokenTTL).toBe(MCP_ACCESS_TOKEN_TTL_SECONDS);
  });

  it("kintone 側で失効(invalid_grant)していたら OAuthError('invalid_grant') を投げる", async () => {
    mockOutboundFetch({ [TOKEN_ENDPOINT]: () => jsonResponse({ error: "invalid_grant" }, 400) });
    const err = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0)).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(OAuthError);
    expect((err as OAuthError).code).toBe("invalid_grant");
    expect(JSON.stringify((err as OAuthError).description)).not.toContain("old-rt");
  });

  it("kintone の一時障害は temporarily_unavailable(grant は残す)", async () => {
    mockOutboundFetch({ [TOKEN_ENDPOINT]: () => jsonResponse({}, 503) });
    const err = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0)).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(OAuthError);
    expect((err as OAuthError).code).toBe("temporarily_unavailable");
    expect((err as OAuthError).statusCode).toBe(503);
  });

  it("更新は Basic 認証で行い、フォームに client_secret を入れない", async () => {
    const m = mockOutboundFetch({
      [TOKEN_ENDPOINT]: () => jsonResponse({ access_token: "new-at", expires_in: 3600 }),
    });
    await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0));
    expect(m.calls[0]?.headers.get("authorization")).toBe(
      `Basic ${btoa("test-kintone-client-id:test-kintone-client-secret")}`,
    );
    expect(m.calls[0]?.body).not.toContain("test-kintone-client-secret");
  });

  it.each([
    ["invalid_client", 401],
    ["unauthorized_client", 400],
  ])("運用者の設定ミス(%s)は server_error(503)で、grant を失効させない", async (error, status) => {
    mockOutboundFetch({ [TOKEN_ENDPOINT]: () => jsonResponse({ error }, status) });
    const err = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0)).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(OAuthError);
    expect((err as OAuthError).code).toBe("server_error");
    expect((err as OAuthError).statusCode).toBe(503);
  });

  it.each([
    ["本文なしの 401", () => new Response("", { status: 401 })],
    ["JSON でない 400", () => new Response("<html>waf</html>", { status: 400 })],
  ])("error が読めない 4xx(%s)は server_error(503)で、grant を失効させない", async (_n, res) => {
    mockOutboundFetch({ [TOKEN_ENDPOINT]: res });
    const err = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0)).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(OAuthError);
    expect((err as OAuthError).code).toBe("server_error");
    expect((err as OAuthError).statusCode).toBe(503);
  });

  it.each([
    ["5xx", () => jsonResponse({}, 502)],
    ["429", () => jsonResponse({}, 429)],
    ["通信失敗", () => Promise.reject(new Error("network down"))],
  ])("一時障害(%s)は temporarily_unavailable(503)", async (_name, response) => {
    mockOutboundFetch({ [TOKEN_ENDPOINT]: response });
    const err = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0)).catch((e: unknown) => e);
    expect((err as OAuthError).code).toBe("temporarily_unavailable");
    expect((err as OAuthError).statusCode).toBe(503);
  });

  it("MCP の寿命は new_token では clamp(残り - 60, 60, 1800)、下限は 60 秒", async () => {
    mockOutboundFetch({
      [TOKEN_ENDPOINT]: () => jsonResponse({ access_token: "new-at", expires_in: 100 }),
    });
    expect((await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0)))?.accessTokenTTL).toBe(60);
  });

  describe("kintone の更新が必要という印(kintone-stale)", () => {
    it("印があれば、期限に余裕があっても kintone を更新し、成功したら印を消す", async () => {
      await markKintoneStale(env.OAUTH_KV, "42", "g-stale-1");
      expect(await isKintoneStale(env.OAUTH_KV, "42", "g-stale-1")).toBe(true);
      const m = mockOutboundFetch({
        [TOKEN_ENDPOINT]: () =>
          jsonResponse({ access_token: "new-at", refresh_token: "new-rt", expires_in: 3600 }),
      });
      const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(3500), "g-stale-1");
      expect(m.calls).toHaveLength(1);
      expect(result?.newProps).toMatchObject({ kintoneAccessToken: "new-at" });
      expect(await isKintoneStale(env.OAUTH_KV, "42", "g-stale-1")).toBe(false);
    });

    it("印は grant ごと: 別の grant の印では更新しない", async () => {
      await markKintoneStale(env.OAUTH_KV, "42", "g-stale-other");
      const m = mockOutboundFetch({});
      const result = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(3500), "g-stale-mine");
      expect(m.calls).toHaveLength(0);
      expect(result?.newProps).toBeUndefined();
      expect(await isKintoneStale(env.OAUTH_KV, "42", "g-stale-other")).toBe(true);
    });

    it("印があって kintone の更新が一時障害で失敗したら、印は残す(次の更新で再試行する)", async () => {
      await markKintoneStale(env.OAUTH_KV, "42", "g-stale-2");
      mockOutboundFetch({ [TOKEN_ENDPOINT]: () => jsonResponse({}, 503) });
      const err = await call(GrantType.REFRESH_TOKEN, propsExpiringIn(3500), "g-stale-2").catch(
        (e: unknown) => e,
      );
      expect((err as OAuthError).code).toBe("temporarily_unavailable");
      expect(await isKintoneStale(env.OAUTH_KV, "42", "g-stale-2")).toBe(true);
    });

    it("authorization_code では印を読まない(印があっても更新せず、KV も見ず、印は残る)", async () => {
      await markKintoneStale(env.OAUTH_KV, "42", "g-stale-code");
      const kvGet = vi.spyOn(env.OAUTH_KV, "get");
      const m = mockOutboundFetch({});
      const result = await call(
        GrantType.AUTHORIZATION_CODE,
        propsExpiringIn(3500),
        "g-stale-code",
      );
      expect(m.calls).toHaveLength(0);
      expect(result?.newProps).toBeUndefined();
      expect(kvGet).not.toHaveBeenCalled();
      expect(await isKintoneStale(env.OAUTH_KV, "42", "g-stale-code")).toBe(true);
    });

    it('印の値は "1" だけ', async () => {
      await markKintoneStale(env.OAUTH_KV, "42", "g-stale-val");
      expect(await env.OAUTH_KV.get(kintoneStaleKey("42", "g-stale-val"))).toBe("1");
    });

    it("印の値にトークンは入らない(TTL つきの目印だけ)", async () => {
      await markKintoneStale(env.OAUTH_KV, "42", "g-stale-3");
      const raw = await env.OAUTH_KV.getWithMetadata(kintoneStaleKey("42", "g-stale-3"));
      expect(raw.value).not.toContain("old-at");
      expect(raw.value).not.toContain("old-rt");
      expect(kintoneStaleKey("42", "g-stale-3")).toBe("kintone-stale:42:g-stale-3");
    });
  });

  describe("診断ログ(値を出さない)", () => {
    function captureLogs() {
      const lines: string[] = [];
      for (const level of ["log", "info", "warn", "error"] as const) {
        vi.spyOn(console, level).mockImplementation((...args: unknown[]) => {
          lines.push(args.map(String).join(" "));
        });
      }
      return lines;
    }
    const events = (lines: string[]) =>
      lines.flatMap((l) => {
        try {
          return [JSON.parse(l) as Record<string, unknown>];
        } catch {
          return [];
        }
      });

    it("更新の判定(grant_type・残り秒数・needs_refresh・stale・refreshed)を出し、トークンは出さない", async () => {
      const lines = captureLogs();
      mockOutboundFetch({
        [TOKEN_ENDPOINT]: () =>
          jsonResponse({ access_token: "new-at", refresh_token: "new-rt", expires_in: 3600 }),
      });
      await call(GrantType.REFRESH_TOKEN, propsExpiringIn(-10), "g-log-1");
      const decision = events(lines).find((e) => e.event === "token_exchange");
      expect(decision).toEqual({
        event: "token_exchange",
        grant_type: "refresh_token",
        kintone_remaining_seconds: -10,
        needs_refresh: true,
        stale_marker: false,
        refreshed: true,
        access_token_changed: true,
        ttl_basis: "new_token",
        access_token_ttl: MCP_ACCESS_TOKEN_TTL_SECONDS,
      });
      const kintone = events(lines).find((e) => e.event === "kintone_token");
      expect(kintone).toEqual({
        event: "kintone_token",
        grant_type: "refresh_token",
        expires_in: 3600,
        refresh_token_rotated: true,
      });
      const all = lines.join("\n");
      for (const secret of ["old-at", "old-rt", "new-at", "new-rt", "test-kintone-client-secret"]) {
        expect(all).not.toContain(secret);
      }
    });

    it("kintone が同じアクセストークンを返したら access_token_changed: false、ローテーションなしなら rotated: false", async () => {
      const lines = captureLogs();
      mockOutboundFetch({
        [TOKEN_ENDPOINT]: () => jsonResponse({ access_token: "old-at", expires_in: 3600 }),
      });
      await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0), "g-log-2");
      const decision = events(lines).find((e) => e.event === "token_exchange");
      expect(decision?.access_token_changed).toBe(false);
      expect(events(lines).find((e) => e.event === "kintone_token")?.refresh_token_rotated).toBe(
        false,
      );
      expect(lines.join("\n")).not.toContain("old-at");
    });

    it("更新しないときも判定を出す(stale 印の有無つき)", async () => {
      const lines = captureLogs();
      mockOutboundFetch({});
      await call(GrantType.REFRESH_TOKEN, propsExpiringIn(3500), "g-log-3");
      expect(events(lines).find((e) => e.event === "token_exchange")).toEqual({
        event: "token_exchange",
        grant_type: "refresh_token",
        kintone_remaining_seconds: 3500,
        needs_refresh: false,
        stale_marker: false,
        refreshed: false,
        ttl_basis: "kintone_remaining",
        access_token_ttl: MCP_ACCESS_TOKEN_TTL_SECONDS,
      });
    });

    it("同じトークンが返ったときは ttl_basis: kintone_remaining", async () => {
      const lines = captureLogs();
      mockOutboundFetch({
        [TOKEN_ENDPOINT]: () => jsonResponse({ access_token: "old-at", expires_in: 100 }),
      });
      await call(GrantType.REFRESH_TOKEN, propsExpiringIn(0), "g-log-4");
      const decision = events(lines).find((e) => e.event === "token_exchange");
      expect(decision).toMatchObject({
        refreshed: true,
        access_token_changed: false,
        ttl_basis: "kintone_remaining",
        access_token_ttl: 100 + KINTONE_EXPIRY_GRACE_SECONDS,
      });
    });
  });
});

describe("grant の寿命(ADR 0010)", () => {
  it("MCP のリフレッシュトークン(grant)の最大寿命は 7 日", async () => {
    expect(MCP_REFRESH_TOKEN_TTL_SECONDS).toBe(7 * 24 * 3600);
    expect(MCP_CLIENT_REGISTRATION_TTL_SECONDS).toBe(30 * 24 * 3600);
  });

  it("tokenExchangeCallback は grant の寿命(refreshTokenTTL / refreshTokenIdleTTL)を返さない(固定寿命を延ばさない)", async () => {
    mockOutboundFetch({
      [TOKEN_ENDPOINT]: () =>
        jsonResponse({ access_token: "new-at", refresh_token: "new-rt", expires_in: 3600 }),
    });
    for (const props of [propsExpiringIn(-10), propsExpiringIn(3000)]) {
      const refreshed = await call(GrantType.REFRESH_TOKEN, props);
      expect(refreshed).not.toHaveProperty("refreshTokenTTL");
      expect(refreshed).not.toHaveProperty("refreshTokenIdleTTL");
    }
    const authCode = await call(GrantType.AUTHORIZATION_CODE, propsExpiringIn(3000));
    expect(authCode ?? {}).not.toHaveProperty("refreshTokenTTL");
    expect(authCode ?? {}).not.toHaveProperty("refreshTokenIdleTTL");
  });
});
