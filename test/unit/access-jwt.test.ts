import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AccessJwtError, clearJwksCache, verifyAccessJwt } from "../../src/auth/access-jwt";
import {
  ACCESS_AUD,
  CERTS_URL,
  createTestKey,
  jwksResponse,
  signAccessJwt,
  signHs256Jwt,
  TEAM_DOMAIN,
  type TestKey,
} from "../helpers/access";
import { mockOutboundFetch } from "../helpers/mock-fetch";

const cfg = { teamDomain: TEAM_DOMAIN, aud: ACCESS_AUD };

let key: TestKey;
let otherKey: TestKey;

beforeEach(async () => {
  clearJwksCache();
  key = await createTestKey("kid-1");
  otherKey = await createTestKey("kid-1"); // 同じ kid だが別の鍵(署名不正の検証用)
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

async function rejection(token: string | null | undefined): Promise<AccessJwtError> {
  const err = await verifyAccessJwt(token, cfg).then(
    () => null,
    (e: unknown) => e,
  );
  expect(err).toBeInstanceOf(AccessJwtError);
  return err as AccessJwtError;
}

describe("verifyAccessJwt: 受け入れ", () => {
  it("正しい JWT を受け入れ、email と sub を返す", async () => {
    mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(key) });
    const token = await signAccessJwt(key, { email: "bob@example.com", sub: "sub-bob" });
    expect(await verifyAccessJwt(token, cfg)).toEqual({
      email: "bob@example.com",
      subject: "sub-bob",
    });
  });

  it("aud が配列でも(ACCESS_AUD を含んでいれば)通る", async () => {
    mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(key) });
    const token = await signAccessJwt(key, { aud: ["another-app", ACCESS_AUD] });
    expect((await verifyAccessJwt(token, cfg)).email).toBe("alice@example.com");
  });
});

describe("verifyAccessJwt: 拒否", () => {
  beforeEach(() => {
    mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(key) });
  });

  it("ヘッダーなし(null・undefined・空文字)は missing", async () => {
    for (const t of [undefined, null, ""]) {
      expect((await rejection(t)).reason).toBe("missing");
    }
  });

  it("JWT の形をしていない文字列は invalid", async () => {
    expect((await rejection("not-a-jwt")).reason).toBe("invalid");
  });

  it("偽署名(同じ kid の別の鍵で署名)を拒否する", async () => {
    expect((await rejection(await signAccessJwt(otherKey))).reason).toBe("invalid");
  });

  it("aud 違いを拒否する(配列で ACCESS_AUD を含まない場合も)", async () => {
    expect((await rejection(await signAccessJwt(key, { aud: "another-app" }))).reason).toBe(
      "invalid",
    );
    expect((await rejection(await signAccessJwt(key, { aud: ["a", "b"] }))).reason).toBe("invalid");
  });

  it("iss 違い(別チーム)を拒否する", async () => {
    const token = await signAccessJwt(key, { teamDomain: "evil.cloudflareaccess.com" });
    expect((await rejection(token)).reason).toBe("invalid");
  });

  it("期限切れを拒否する", async () => {
    const past = Math.floor(Date.now() / 1000) - 3600;
    const token = await signAccessJwt(key, { iat: past, exp: past + 300 });
    expect((await rejection(token)).reason).toBe("invalid");
  });

  it("exp がない JWT を拒否する", async () => {
    expect((await rejection(await signAccessJwt(key, { omit: ["exp"] }))).reason).toBe("invalid");
  });

  it.each([
    ["claim 欠落", { omit: ["email"] as ("email" | "sub" | "exp")[] }],
    ["空文字", { email: "" }],
    ["空白だけ", { email: "   " }],
    ["数値", { email: 123 }],
    ["null", { email: null }],
  ])("email が使えない JWT を拒否する: %s", async (_name, opts) => {
    expect((await rejection(await signAccessJwt(key, opts))).reason).toBe("invalid");
  });

  it("sub がない JWT を拒否する", async () => {
    expect((await rejection(await signAccessJwt(key, { omit: ["sub"] }))).reason).toBe("invalid");
  });

  it("alg が RS256 でない JWT(HS256 を JWKS の公開鍵で署名)を拒否する", async () => {
    expect((await rejection(await signHs256Jwt(key))).reason).toBe("invalid");
  });

  it("エラーに JWT も email も含まれない", async () => {
    const email = "leak-check@example.com";
    const forged = await signAccessJwt(otherKey, { email });
    const expired = await signAccessJwt(key, { email, iat: 1_000_000, exp: 1_000_300 });
    for (const token of [forged, expired, await signAccessJwt(key, { aud: "x", email })]) {
      const err = await rejection(token);
      expect(err.message).not.toContain(token);
      expect(err.message).not.toContain(email);
      expect(String(err.stack)).not.toContain(email);
    }
  });
});

describe("verifyAccessJwt: JWKS を取得できないとき(fail closed)", () => {
  it.each([
    ["404", () => new Response("not found", { status: 404 })],
    ["500", () => new Response("boom", { status: 500 })],
    ["fetch が例外", () => Promise.reject(new Error("network down"))],
    ["JSON でない本文", () => new Response("<html>", { status: 200 })],
    ["keys が配列でない", () => Response.json({ keys: "nope" })],
    ["本文が null", () => Response.json(null)],
  ])("%s なら jwks_unavailable", async (_name, handler) => {
    mockOutboundFetch({ [CERTS_URL]: handler });
    const token = await signAccessJwt(key);
    expect((await rejection(token)).reason).toBe("jwks_unavailable");
  });
});

describe("verifyAccessJwt: JWKS のキャッシュと鍵ローテーション", () => {
  it("JWKS を 5 分キャッシュし、2 回目以降は取得しない。5 分を過ぎると取り直す", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const m = mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(key) });
    const long = (extra = 0) =>
      signAccessJwt(key, { exp: Math.floor(Date.now() / 1000) + 3600 + extra });
    const token = await long();
    await verifyAccessJwt(token, cfg);
    await verifyAccessJwt(token, cfg);
    expect(m.callsTo(CERTS_URL)).toHaveLength(1);

    vi.setSystemTime(Date.now() + 4 * 60_000);
    await verifyAccessJwt(token, cfg);
    expect(m.callsTo(CERTS_URL)).toHaveLength(1);

    vi.setSystemTime(Date.now() + 61_000); // 取得から 5 分 1 秒
    await verifyAccessJwt(token, cfg);
    expect(m.callsTo(CERTS_URL)).toHaveLength(2);
  });

  it("未知の kid は、JWKS の取得から 60 秒以上たっていれば鍵ローテーションとみなして取り直す", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const rotated = await createTestKey("kid-2");
    let current = [key];
    const m = mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(...current) });
    await verifyAccessJwt(await signAccessJwt(key), cfg);
    expect(m.callsTo(CERTS_URL)).toHaveLength(1);

    current = [key, rotated];
    vi.setSystemTime(Date.now() + 61_000);
    const identity = await verifyAccessJwt(await signAccessJwt(rotated), cfg);
    expect(identity.email).toBe("alice@example.com");
    expect(m.callsTo(CERTS_URL)).toHaveLength(2);
  });

  it("未知の kid でも、JWKS の取得から 60 秒未満なら取り直さない(連投で Access への通信を増やさない)", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const rotated = await createTestKey("kid-2");
    const m = mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(key) });
    await verifyAccessJwt(await signAccessJwt(key), cfg);

    vi.setSystemTime(Date.now() + 30_000);
    for (let i = 0; i < 3; i++) {
      expect((await rejection(await signAccessJwt(rotated))).reason).toBe("invalid");
    }
    expect(m.callsTo(CERTS_URL)).toHaveLength(1);
  });

  it("キャッシュが空の最初の検証で未知の kid でも、取得は 1 回だけ(取得直後は取り直さない)", async () => {
    const rotated = await createTestKey("kid-2");
    const m = mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(key) });
    expect((await rejection(await signAccessJwt(rotated))).reason).toBe("invalid");
    expect(m.callsTo(CERTS_URL)).toHaveLength(1);
  });
});

describe("verifyAccessJwt: JWKS 取得のタイムアウトと同時取得", () => {
  /** signal の abort を受けて reject する fetch(実時間を待たない)。 */
  function hangingFetch() {
    return vi.spyOn(globalThis, "fetch").mockImplementation(
      (_input, init) =>
        new Promise<Response>((_resolve, reject) => {
          const signal = init?.signal;
          if (!signal) return; // signal がなければ永遠に待つ(テストがタイムアウトで失敗する)
          const onAbort = () => reject(signal.reason);
          if (signal.aborted) onAbort();
          else signal.addEventListener("abort", onAbort);
        }),
    );
  }

  it("fetch に AbortSignal.timeout を渡す", async () => {
    const m = mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(key) });
    await verifyAccessJwt(await signAccessJwt(key), cfg);
    const signal = m.spy.mock.calls[0]?.[1]?.signal;
    expect(signal).toBeInstanceOf(AbortSignal);
  });

  it("JWKS の取得がタイムアウトしたら jwks_unavailable(fail closed)", async () => {
    const token = await signAccessJwt(key);
    const timeoutSpy = vi.spyOn(AbortSignal, "timeout").mockImplementation(() => {
      const c = new AbortController();
      c.abort(new DOMException("timed out", "TimeoutError"));
      return c.signal;
    });
    hangingFetch();
    expect((await rejection(token)).reason).toBe("jwks_unavailable");
    expect(timeoutSpy).toHaveBeenCalledWith(5000);
  });

  it("同時のリクエストは同じ JWKS 取得を待つ(取得は 1 回)", async () => {
    const m = mockOutboundFetch({ [CERTS_URL]: () => jwksResponse(key) });
    const tokens = await Promise.all([signAccessJwt(key), signAccessJwt(key), signAccessJwt(key)]);
    const results = await Promise.all(tokens.map((t) => verifyAccessJwt(t, cfg)));
    expect(results).toHaveLength(3);
    expect(m.callsTo(CERTS_URL)).toHaveLength(1);
  });

  it("取得に失敗したら Promise のキャッシュを消し、次のリクエストで取り直す", async () => {
    let fail = true;
    const m = mockOutboundFetch({
      [CERTS_URL]: () => (fail ? new Response("boom", { status: 500 }) : jwksResponse(key)),
    });
    const token = await signAccessJwt(key);
    const [a, b] = await Promise.all([rejection(token), rejection(token)]);
    expect(a.reason).toBe("jwks_unavailable");
    expect(b.reason).toBe("jwks_unavailable");
    expect(m.callsTo(CERTS_URL)).toHaveLength(1);

    fail = false;
    expect((await verifyAccessJwt(token, cfg)).email).toBe("alice@example.com");
    expect(m.callsTo(CERTS_URL)).toHaveLength(2);
  });
});
