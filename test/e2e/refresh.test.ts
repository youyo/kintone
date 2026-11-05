import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createFakeKintoneOAuth,
  type FakeKintoneOAuthOptions,
} from "../helpers/fake-kintone-oauth";
import { jsonResponse } from "../helpers/mock-fetch";
import {
  KINTONE_APPS,
  KINTONE_TOKEN,
  mcpRequest,
  ORIGIN,
  readRpc,
  refreshRequest,
  runAuthorizationFlow,
  setupMocks,
  type TokenSet,
} from "../helpers/oauth-flow";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function key(c: { method: string; url: string }) {
  const u = new URL(c.url);
  return `${c.method} ${u.origin}${u.pathname}`;
}

/** kintone の OAuth と REST を fake にして、Access の JWKS とあわせてモックする。 */
async function mockFakeKintone(opts: FakeKintoneOAuthOptions = {}) {
  const kintone = createFakeKintoneOAuth(opts);
  const mocks = await setupMocks({
    kintoneToken: (call) => kintone.token(call),
    kintoneApps: (call) => kintone.apps(call),
  });
  const callsTo = (k: string) => mocks.calls.filter((c) => key(c) === k);
  return { kintone, ...mocks, callsTo };
}

const callApps = (token: string) =>
  mcpRequest(token, "tools/call", { name: "kintone_apps_search", arguments: {} });

/** 通常の MCP クライアントのように、指定時刻に更新する。 */
async function refreshAt(clientId: string, refreshToken: string, atMs: number) {
  vi.setSystemTime(atMs);
  const res = await refreshRequest(clientId, refreshToken);
  expect(res.status).toBe(200);
  return (await res.json()) as TokenSet;
}

// kintone の更新の実挙動は未検証(V01)なので、2 つのモードで同じテストを回し、どちらでも壊れないことを固定する(ADR 0005)。
describe.each([
  ["A: 期限内の更新は同じトークンを残り寿命つきで返す", "A"],
  ["B: 更新のたびに新しいトークン", "B"],
] as const)("E2E 4(モード %s): リフレッシュで kintone のトークンも更新される", (_label, mode) => {
  it("kintone のアクセストークンが期限切れの状態で MCP クライアントがリフレッシュすると、kintone のトークンも Basic 認証で更新され /mcp で新トークンが使われる(ローテーションあり)", async () => {
    const { callsTo, accessKey } = await mockFakeKintone({ mode, rotate: true });
    const flow = await runAuthorizationFlow({ accessKey });
    const first = (await flow.tokenResponse.json()) as TokenSet;
    // コード交換時は取りたてなので kintone のリフレッシュは起きていない
    expect(callsTo(KINTONE_TOKEN)).toHaveLength(1);

    // 2 時間後(kintone のアクセストークンは期限切れ。MCP のリフレッシュトークンは長寿命)
    vi.useFakeTimers({ toFake: ["Date"], now: Date.now() + 2 * 3600 * 1000 });
    const refreshed = await refreshRequest(flow.clientId, first.refresh_token);
    expect(refreshed.status).toBe(200);
    const second = (await refreshed.json()) as TokenSet;
    expect(second.access_token).not.toBe(first.access_token);

    const kintoneCalls = callsTo(KINTONE_TOKEN);
    expect(kintoneCalls).toHaveLength(2);
    const form = new URLSearchParams(kintoneCalls[1]?.body);
    expect(form.get("grant_type")).toBe("refresh_token");
    expect(form.get("refresh_token")).toBe("kintone-rt-1");
    expect(form.has("client_secret")).toBe(false);
    expect(kintoneCalls[1]?.headers.get("authorization")).toBe(
      `Basic ${btoa("test-kintone-client-id:test-kintone-client-secret")}`,
    );

    // 新しい MCP トークンで /mcp を呼ぶと、更新後の kintone トークンが使われる
    const call = await callApps(second.access_token);
    expect(call.status).toBe(200);
    expect((await readRpc(call)).result?.isError).toBeFalsy();
    expect(callsTo(KINTONE_APPS).at(-1)?.headers.get("authorization")).toBe("Bearer kintone-at-2");

    // 2 回目のリフレッシュ(さらに 2 時間後)では、ローテーション後のリフレッシュトークンが使われる
    vi.setSystemTime(Date.now() + 2 * 3600 * 1000);
    const third = await refreshRequest(flow.clientId, second.refresh_token);
    expect(third.status).toBe(200);
    expect(new URLSearchParams(callsTo(KINTONE_TOKEN)[2]?.body).get("refresh_token")).toBe(
      "kintone-rt-2",
    );
  });

  it("kintone がリフレッシュトークンをローテーションしない場合も、元のリフレッシュトークンで更新を続けられる", async () => {
    const { callsTo, accessKey } = await mockFakeKintone({ mode, rotate: false });
    const flow = await runAuthorizationFlow({ accessKey });
    const first = (await flow.tokenResponse.json()) as TokenSet;

    vi.useFakeTimers({ toFake: ["Date"], now: Date.now() + 2 * 3600 * 1000 });
    const r1 = await refreshRequest(flow.clientId, first.refresh_token);
    expect(r1.status).toBe(200);
    const second = (await r1.json()) as TokenSet;

    vi.setSystemTime(Date.now() + 2 * 3600 * 1000);
    const r2 = await refreshRequest(flow.clientId, second.refresh_token);
    expect(r2.status).toBe(200);
    expect(new URLSearchParams(callsTo(KINTONE_TOKEN)[2]?.body).get("refresh_token")).toBe(
      "kintone-rt-1",
    );
  });

  it("kintone の期限に余裕があるうちは、MCP のリフレッシュで kintone を呼ばない", async () => {
    const { callsTo, accessKey } = await mockFakeKintone({ mode, lifetimeSeconds: 3 * 3600 });
    const flow = await runAuthorizationFlow({ accessKey });
    const first = (await flow.tokenResponse.json()) as TokenSet;
    const r = await refreshRequest(flow.clientId, first.refresh_token);
    expect(r.status).toBe(200);
    expect(callsTo(KINTONE_TOKEN)).toHaveLength(1);
  });

  it("kintone 側で失効(invalid_grant)していたらリフレッシュは invalid_grant になり、grant は失効して再認可が必要になる", async () => {
    const { accessKey } = await mockFakeKintone({ mode, refreshStatus: 400 });
    const flow = await runAuthorizationFlow({ accessKey });
    const first = (await flow.tokenResponse.json()) as TokenSet;

    vi.useFakeTimers({ toFake: ["Date"], now: Date.now() + 2 * 3600 * 1000 });
    const res = await refreshRequest(flow.clientId, first.refresh_token);
    expect(res.status).toBe(400);
    expect(((await res.json()) as { error: string }).error).toBe("invalid_grant");

    // grant は失効済み: 同じリフレッシュトークンはもう使えない
    const again = await refreshRequest(flow.clientId, first.refresh_token);
    expect(again.status).toBe(400);
  });

  it("クライアント ID / シークレットの誤り(invalid_client)は 503 server_error で、grant は残る(直れば再試行できる)", async () => {
    let misconfigured = true;
    const kintone = createFakeKintoneOAuth({ mode });
    const { accessKey } = await setupMocks({
      kintoneToken: (call) => {
        const form = new URLSearchParams(call.body);
        if (form.get("grant_type") === "refresh_token" && misconfigured) {
          return jsonResponse({ error: "invalid_client" }, 401);
        }
        return kintone.token(call);
      },
      kintoneApps: (call) => kintone.apps(call),
    });
    const flow = await runAuthorizationFlow({ accessKey });
    const first = (await flow.tokenResponse.json()) as TokenSet;

    vi.useFakeTimers({ toFake: ["Date"], now: Date.now() + 2 * 3600 * 1000 });
    const res = await refreshRequest(flow.clientId, first.refresh_token);
    expect(res.status).toBe(503);
    expect(((await res.json()) as { error: string }).error).toBe("server_error");

    misconfigured = false;
    const retry = await refreshRequest(flow.clientId, first.refresh_token);
    expect(retry.status).toBe(200);
  });

  it("kintone の一時障害は 503 temporarily_unavailable で、grant は残る(再試行できる)", async () => {
    let down = true;
    const kintone = createFakeKintoneOAuth({ mode });
    const { accessKey } = await setupMocks({
      kintoneToken: (call) => {
        const form = new URLSearchParams(call.body);
        if (form.get("grant_type") === "refresh_token" && down) return jsonResponse({}, 503);
        return kintone.token(call);
      },
      kintoneApps: (call) => kintone.apps(call),
    });
    const flow = await runAuthorizationFlow({ accessKey });
    const first = (await flow.tokenResponse.json()) as TokenSet;

    vi.useFakeTimers({ toFake: ["Date"], now: Date.now() + 2 * 3600 * 1000 });
    const res = await refreshRequest(flow.clientId, first.refresh_token);
    expect(res.status).toBe(503);
    expect(((await res.json()) as { error: string }).error).toBe("temporarily_unavailable");

    down = false;
    const retry = await refreshRequest(flow.clientId, first.refresh_token);
    expect(retry.status).toBe(200);
  });

  it("/mcp で kintone が 401 を返したら、MCP も 401 + invalid_token + resource_metadata を返してクライアントにリフレッシュさせる", async () => {
    const { kintone, accessKey } = await mockFakeKintone({ mode });
    const flow = await runAuthorizationFlow({ accessKey });
    const first = (await flow.tokenResponse.json()) as TokenSet;
    kintone.revoke();

    const res = await callApps(first.access_token);
    expect(res.status).toBe(401);
    const challenge = res.headers.get("WWW-Authenticate") ?? "";
    expect(challenge).toContain('error="invalid_token"');
    expect(challenge).toContain(
      `resource_metadata="${ORIGIN}/.well-known/oauth-protected-resource/mcp"`,
    );
  });

  it("保存した期限より前に kintone が 401 を返し始めても、401 → リフレッシュ → 再呼び出しで回復する(完了条件 5)", async () => {
    const { kintone, callsTo, accessKey } = await mockFakeKintone({ mode, rotate: true });
    const flow = await runAuthorizationFlow({ accessKey });
    const first = (await flow.tokenResponse.json()) as TokenSet;
    expect(callsTo(KINTONE_TOKEN)).toHaveLength(1);

    // 期限(props 上はまだ約 1 時間)の前に kintone 側で失効する
    kintone.revoke();
    const denied = await callApps(first.access_token);
    expect(denied.status).toBe(401);

    // MCP クライアントは 401 を受けてリフレッシュする(期限に余裕があっても kintone を更新させる)
    const refreshed = await refreshRequest(flow.clientId, first.refresh_token);
    expect(refreshed.status).toBe(200);
    const second = (await refreshed.json()) as TokenSet;
    const kintoneCalls = callsTo(KINTONE_TOKEN);
    expect(kintoneCalls).toHaveLength(2);
    expect(new URLSearchParams(kintoneCalls[1]?.body).get("grant_type")).toBe("refresh_token");

    const retried = await callApps(second.access_token);
    expect(retried.status).toBe(200);
    expect((await readRpc(retried)).result?.isError).toBeFalsy();
    expect(callsTo(KINTONE_APPS).at(-1)?.headers.get("authorization")).toBe("Bearer kintone-at-2");

    // 回復後のリフレッシュでは(期限に余裕があり印も消えているので)kintone を呼ばない
    const again = await refreshRequest(flow.clientId, second.refresh_token);
    expect(again.status).toBe(200);
    expect(callsTo(KINTONE_TOKEN)).toHaveLength(2);
  });

  describe("10 分ごとの更新と失効の境目", () => {
    const EXPECTED_TTL: Record<number, number> = {
      10: 1800,
      20: 1800,
      30: 1800,
      40: 20 * 60 + 5,
      50: 10 * 60 + 5,
    };

    it("期限内は kintone を呼ばず、MCP の寿命は kintone の残り + 余裕。失効直後の更新で新しいトークンになる", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      const t0 = Date.now();
      const { kintone, callsTo, accessKey } = await mockFakeKintone({ mode });
      const flow = await runAuthorizationFlow({ accessKey });
      let tokens = (await flow.tokenResponse.json()) as TokenSet;
      expect(tokens.expires_in).toBe(1800);

      for (const [minute, ttl] of Object.entries(EXPECTED_TTL)) {
        tokens = await refreshAt(flow.clientId, tokens.refresh_token, t0 + Number(minute) * 60_000);
        expect(tokens.expires_in).toBe(ttl);
        expect(tokens.expires_in).toBeGreaterThan(60);
      }
      expect(kintone.refreshCalls).toBe(0);
      expect(callsTo(KINTONE_TOKEN)).toHaveLength(1);

      // 失効の 1 秒前でもまだ kintone のトークンで呼べる
      vi.setSystemTime(t0 + 3599_000);
      expect((await callApps(tokens.access_token)).status).toBe(200);

      // MCP の寿命(= kintone の失効 + 5 秒)で更新すると、kintone から新しいトークンを得る
      tokens = await refreshAt(flow.clientId, tokens.refresh_token, t0 + 3605_000);
      expect(kintone.refreshCalls).toBe(1);
      expect(kintone.refreshesReturningSameToken).toBe(0);
      expect(kintone.currentAccessToken).toBe("kintone-at-2");
      expect(tokens.expires_in).toBe(1800);
      const res = await callApps(tokens.access_token);
      expect(res.status).toBe(200);
      expect(callsTo(KINTONE_APPS).at(-1)?.headers.get("authorization")).toBe(
        "Bearer kintone-at-2",
      );
    });

    it("失効と次の更新の隙間で 401 が出ても、1 回の更新(stale 印)で回復する", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      const t0 = Date.now();
      const { kintone, callsTo, accessKey } = await mockFakeKintone({ mode });
      const flow = await runAuthorizationFlow({ accessKey });
      let tokens = (await flow.tokenResponse.json()) as TokenSet;
      tokens = await refreshAt(flow.clientId, tokens.refresh_token, t0 + 50 * 60_000);
      expect(tokens.expires_in).toBe(605);

      // kintone は失効したが MCP のトークンはあと 3 秒有効: 401 が出て stale 印が置かれる
      vi.setSystemTime(t0 + 3602_000);
      const denied = await callApps(tokens.access_token);
      expect(denied.status).toBe(401);
      expect(denied.headers.get("WWW-Authenticate")).toContain('error="invalid_token"');

      const logs: Record<string, unknown>[] = [];
      vi.spyOn(console, "log").mockImplementation((...args: unknown[]) => {
        try {
          logs.push(JSON.parse(String(args[0])) as Record<string, unknown>);
        } catch {
          // JSON 以外のログは見ない
        }
      });
      const refreshed = await refreshRequest(flow.clientId, tokens.refresh_token);
      expect(refreshed.status).toBe(200);
      tokens = (await refreshed.json()) as TokenSet;
      expect(logs.find((e) => e.event === "token_exchange")).toMatchObject({
        stale_marker: true,
        refreshed: true,
        access_token_changed: true,
        ttl_basis: "new_token",
      });
      expect(kintone.refreshCalls).toBe(1);

      const retried = await callApps(tokens.access_token);
      expect(retried.status).toBe(200);
      expect(callsTo(KINTONE_APPS).at(-1)?.headers.get("authorization")).toBe(
        "Bearer kintone-at-2",
      );

      // 回復後の更新では(期限に余裕があり印も消えているので)kintone を呼ばない
      tokens = await refreshAt(flow.clientId, tokens.refresh_token, t0 + 3700_000);
      expect(kintone.refreshCalls).toBe(1);
    });
  });
});

describe("E2E 4b-A: モード A(同じトークンが残り寿命つきで返る)での stale 印", () => {
  it("stale 印で更新して同じトークンが返っても、期限は保存し直され、次の更新は kintone を呼ばず、失効直後に呼ぶ", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const t0 = Date.now();
    // apps を 1 回だけ 401 にして stale 印を置かせる(fake 自体は期限内で有効なまま)
    let forceDeny = false;
    const kintone = createFakeKintoneOAuth({ mode: "A" });
    const { accessKey, calls } = await setupMocks({
      kintoneToken: (call) => kintone.token(call),
      kintoneApps: (call) => (forceDeny ? jsonResponse({}, 401) : kintone.apps(call)),
    });
    const flow = await runAuthorizationFlow({ accessKey });
    let tokens = (await flow.tokenResponse.json()) as TokenSet;
    for (const minute of [10, 20, 30, 40]) {
      tokens = await refreshAt(flow.clientId, tokens.refresh_token, t0 + minute * 60_000);
    }
    expect(kintone.refreshCalls).toBe(0);

    // 50 分 + 0.9 秒: kintone の残りは 599.1 秒。MCP のトークンはまだ有効
    const t1 = t0 + 3_000_900;
    vi.setSystemTime(t1);
    forceDeny = true;
    expect((await callApps(tokens.access_token)).status).toBe(401);
    forceDeny = false;

    const logs: Record<string, unknown>[] = [];
    vi.spyOn(console, "log").mockImplementation((...args: unknown[]) => {
      try {
        logs.push(JSON.parse(String(args[0])) as Record<string, unknown>);
      } catch {
        // JSON 以外のログは見ない
      }
    });
    const refreshed = await refreshRequest(flow.clientId, tokens.refresh_token);
    expect(refreshed.status).toBe(200);
    tokens = (await refreshed.json()) as TokenSet;
    // 期限内なので kintone は同じトークンを返した。MCP の寿命 = kintone の残り(599)+ 5
    expect(logs.find((e) => e.event === "token_exchange")).toMatchObject({
      stale_marker: true,
      refreshed: true,
      access_token_changed: false,
      ttl_basis: "kintone_remaining",
    });
    expect(kintone.refreshCalls).toBe(1);
    expect(kintone.refreshesReturningSameToken).toBe(1);
    expect(tokens.expires_in).toBe(599 + 5);
    expect((await callApps(tokens.access_token)).status).toBe(200);
    expect(
      calls
        .filter((c) => c.url.includes("/k/v1/apps.json"))
        .at(-1)
        ?.headers.get("authorization"),
    ).toBe("Bearer kintone-at-1");

    // 保存し直した期限は「応答時刻 + 599 秒」= 元の期限より 100ms 早い。
    // 100ms 後の更新では残りが 598.9 秒になり、寿命が 598 + 5 に縮む(保存し直さないと 599 + 5 のまま)。
    tokens = await refreshAt(flow.clientId, tokens.refresh_token, t1 + 100);
    expect(tokens.expires_in).toBe(598 + 5);
    expect(kintone.refreshCalls).toBe(1);

    // 失効直後(元の期限 + 1 秒)の更新では kintone を呼び、新しいトークンを得る
    tokens = await refreshAt(flow.clientId, tokens.refresh_token, t0 + 3601_000);
    expect(kintone.refreshCalls).toBe(2);
    expect(kintone.refreshesReturningSameToken).toBe(1);
    expect(kintone.currentAccessToken).toBe("kintone-at-2");
    expect((await callApps(tokens.access_token)).status).toBe(200);
    expect(
      calls
        .filter((c) => c.url.includes("/k/v1/apps.json"))
        .at(-1)
        ?.headers.get("authorization"),
    ).toBe("Bearer kintone-at-2");
  });
});

describe("E2E 4b-B: モード B(更新のたびに新しいトークン)での stale 印", () => {
  it("期限内に stale 印で更新すると新しいトークンを得て、MCP の寿命は clamp(残り - 60, 60, 1800)", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const t0 = Date.now();
    const kintone = createFakeKintoneOAuth({ mode: "B" });
    let forceDeny = false;
    const { accessKey } = await setupMocks({
      kintoneToken: (call) => kintone.token(call),
      kintoneApps: (call) => (forceDeny ? jsonResponse({}, 401) : kintone.apps(call)),
    });
    const flow = await runAuthorizationFlow({ accessKey });
    let tokens = (await flow.tokenResponse.json()) as TokenSet;
    tokens = await refreshAt(flow.clientId, tokens.refresh_token, t0 + 10 * 60_000);

    vi.setSystemTime(t0 + 11 * 60_000);
    forceDeny = true;
    expect((await callApps(tokens.access_token)).status).toBe(401);
    forceDeny = false;

    const refreshed = await refreshRequest(flow.clientId, tokens.refresh_token);
    expect(refreshed.status).toBe(200);
    tokens = (await refreshed.json()) as TokenSet;
    expect(kintone.refreshCalls).toBe(1);
    expect(kintone.currentAccessToken).toBe("kintone-at-2");
    // kintone の新しい期限は 3600 秒後 → 残り 3600 - 60 を 1800 に丸める
    expect(tokens.expires_in).toBe(1800);
    expect((await callApps(tokens.access_token)).status).toBe(200);
  });
});
