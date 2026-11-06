/**
 * MCP grant の寿命(最大 7 日の固定寿命)と DCR クライアント登録の寿命(30 日)。
 *
 * workers-oauth-provider は、リフレッシュ時に grant の `expiresAt` を `Date.now()` と比べて期限切れなら
 * invalid_grant にする(`refreshTokenTTL` による固定寿命。`refreshTokenIdleTTL` は設定しないので、使い続けても延びない)。
 * Access から外した利用者も、最大 7 日で /mcp を使えなくなる(ADR 0010)。
 */
import { env } from "cloudflare:workers";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createFakeKintoneOAuth } from "../helpers/fake-kintone-oauth";
import {
  refreshRequest,
  registerClient,
  runAuthorizationFlow,
  setupMocks,
  type TokenSet,
} from "../helpers/oauth-flow";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const DAY_MS = 24 * 3600 * 1000;
const DAY = 24 * 3600;

async function authorized() {
  const kintone = createFakeKintoneOAuth({ mode: "A", rotate: true });
  const mocks = await setupMocks({
    kintoneToken: (call) => kintone.token(call),
    kintoneApps: (call) => kintone.apps(call),
  });
  const flow = await runAuthorizationFlow({ accessKey: mocks.accessKey });
  const first = (await flow.tokenResponse.json()) as TokenSet;
  return { flow, first, startMs: Date.now() };
}

describe("E2E: grant の最大寿命は 7 日(固定)", () => {
  it("grant の expiresAt は認可から 7 日後(30 日ではない)", async () => {
    const { first } = await authorized();
    const [userId, grantId] = first.refresh_token.split(":");
    const grant = (await env.OAUTH_KV.get(`grant:${userId}:${grantId}`, { type: "json" })) as {
      expiresAt?: number;
    } | null;
    expect(grant?.expiresAt).toBeDefined();
    const remaining = (grant?.expiresAt ?? 0) - Math.floor(Date.now() / 1000);
    expect(remaining).toBeGreaterThan(7 * DAY - 120);
    expect(remaining).toBeLessThanOrEqual(7 * DAY);
  });

  it("7 日より前なら、リフレッシュは成功する", async () => {
    const { flow, first, startMs } = await authorized();
    vi.useFakeTimers({ toFake: ["Date"], now: startMs + 6 * DAY_MS });
    const res = await refreshRequest(flow.clientId, first.refresh_token);
    expect(res.status).toBe(200);
  });

  it("7 日を過ぎたら、リフレッシュは invalid_grant で失敗する", async () => {
    const { flow, first, startMs } = await authorized();
    vi.useFakeTimers({ toFake: ["Date"], now: startMs + 7 * DAY_MS + 3600 * 1000 });
    const res = await refreshRequest(flow.clientId, first.refresh_token);
    expect(res.status).toBe(400);
    expect(((await res.json()) as { error: string }).error).toBe("invalid_grant");
  });

  it("使い続けても延びない: 6 日目にリフレッシュしても、認可から 7 日を過ぎた次のリフレッシュは失敗する", async () => {
    const { flow, first, startMs } = await authorized();
    vi.useFakeTimers({ toFake: ["Date"], now: startMs + 6 * DAY_MS });
    const mid = await refreshRequest(flow.clientId, first.refresh_token);
    expect(mid.status).toBe(200);
    const second = (await mid.json()) as TokenSet;

    vi.setSystemTime(startMs + 7 * DAY_MS + 3600 * 1000);
    const res = await refreshRequest(flow.clientId, second.refresh_token);
    expect(res.status).toBe(400);
    expect(((await res.json()) as { error: string }).error).toBe("invalid_grant");
  });
});

describe("E2E: DCR のクライアント登録は 30 日で期限が付く", () => {
  it("登録した client の KV レコードに registrationExpiresAt と KV の期限が付く(30 日後)", async () => {
    await setupMocks();
    const clientId = await registerClient();
    const record = (await env.OAUTH_KV.get(`client:${clientId}`, { type: "json" })) as {
      registrationExpiresAt?: number;
    } | null;
    const now = Math.floor(Date.now() / 1000);
    expect(record?.registrationExpiresAt).toBeDefined();
    expect((record?.registrationExpiresAt ?? 0) - now).toBeGreaterThan(30 * DAY - 120);
    expect((record?.registrationExpiresAt ?? 0) - now).toBeLessThanOrEqual(30 * DAY);

    const listed = await env.OAUTH_KV.list({ prefix: `client:${clientId}` });
    const key = listed.keys.find((k) => k.name === `client:${clientId}`);
    expect(key?.expiration).toBeDefined();
    expect((key?.expiration ?? 0) - now).toBeGreaterThan(30 * DAY - 120);
    expect((key?.expiration ?? 0) - now).toBeLessThanOrEqual(30 * DAY + 120);
  });
});
