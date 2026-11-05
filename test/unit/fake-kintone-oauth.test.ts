import { afterEach, describe, expect, it, vi } from "vitest";
import { createFakeKintoneOAuth } from "../helpers/fake-kintone-oauth";
import type { RecordedCall } from "../helpers/mock-fetch";

afterEach(() => {
  vi.useRealTimers();
});

const BASIC = `Basic ${btoa("test-kintone-client-id:test-kintone-client-secret")}`;

function tokenCall(
  grantType: "authorization_code" | "refresh_token",
  refreshToken?: string,
  authorization: string | null = BASIC,
) {
  const body = new URLSearchParams({ grant_type: grantType });
  if (refreshToken) body.set("refresh_token", refreshToken);
  return {
    method: "POST",
    url: "https://example.cybozu.com/oauth2/token",
    headers: new Headers(authorization ? { authorization } : {}),
    body: body.toString(),
  } as RecordedCall;
}

function appsCall(token: string) {
  return {
    method: "GET",
    url: "https://example.cybozu.com/k/v1/apps.json",
    headers: new Headers({ authorization: `Bearer ${token}` }),
    body: "",
  } as RecordedCall;
}

async function json(res: Response) {
  return (await res.json()) as Record<string, unknown>;
}

describe("fake kintone OAuth", () => {
  it("認可コード交換はアクセストークン・リフレッシュトークン・寿命(3600)を返し、Basic 認証がなければ invalid_client", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const fake = createFakeKintoneOAuth();
    expect(await json(fake.token(tokenCall("authorization_code")))).toMatchObject({
      access_token: "kintone-at-1",
      refresh_token: "kintone-rt-1",
      expires_in: 3600,
    });
    const noAuth = fake.token(tokenCall("authorization_code", undefined, null));
    expect(noAuth.status).toBe(401);
    expect((await json(noAuth)).error).toBe("invalid_client");
  });

  describe("モード A(期限内の更新は同じトークンを残り寿命つきで返す)", () => {
    it("期限内の refresh は同じ access_token・expires_in=残り秒数で、refresh_token は返さず、期限は延びない", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      const fake = createFakeKintoneOAuth({ mode: "A" });
      await fake.token(tokenCall("authorization_code"));
      vi.setSystemTime(Date.now() + 600_500);
      const body = await json(fake.token(tokenCall("refresh_token", "kintone-rt-1")));
      expect(body.access_token).toBe("kintone-at-1");
      expect(body.expires_in).toBe(2999);
      expect(body).not.toHaveProperty("refresh_token");
      expect(fake.refreshCalls).toBe(1);
      expect(fake.refreshesReturningSameToken).toBe(1);
      vi.setSystemTime(Date.now() + 2_998_000);
      expect(fake.apps(appsCall("kintone-at-1")).status).toBe(200);
      vi.setSystemTime(Date.now() + 2_000);
      expect(fake.apps(appsCall("kintone-at-1")).status).toBe(401);
    });

    it("失効後の refresh は新しいトークン(expires_in 3600)を発行する", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      const fake = createFakeKintoneOAuth({ mode: "A" });
      await fake.token(tokenCall("authorization_code"));
      vi.setSystemTime(Date.now() + 3600_000);
      const body = await json(fake.token(tokenCall("refresh_token", "kintone-rt-1")));
      expect(body.access_token).toBe("kintone-at-2");
      expect(body.expires_in).toBe(3600);
      expect(body).not.toHaveProperty("refresh_token");
      expect(fake.refreshesReturningSameToken).toBe(0);
      expect(fake.currentAccessToken).toBe("kintone-at-2");
    });
  });

  describe("モード B(更新のたびに新しいトークン)", () => {
    it("期限内でも refresh は毎回新しいトークン(expires_in 3600)を発行し、古いトークンは無効になる", async () => {
      vi.useFakeTimers({ toFake: ["Date"] });
      const fake = createFakeKintoneOAuth({ mode: "B" });
      await fake.token(tokenCall("authorization_code"));
      vi.setSystemTime(Date.now() + 600_000);
      const b1 = await json(fake.token(tokenCall("refresh_token", "kintone-rt-1")));
      expect(b1).toMatchObject({ access_token: "kintone-at-2", expires_in: 3600 });
      const b2 = await json(fake.token(tokenCall("refresh_token", "kintone-rt-1")));
      expect(b2.access_token).toBe("kintone-at-3");
      expect(fake.refreshesReturningSameToken).toBe(0);
      expect(fake.apps(appsCall("kintone-at-1")).status).toBe(401);
      expect(fake.apps(appsCall("kintone-at-3")).status).toBe(200);
    });
  });

  it("rotate: true の失効後 refresh は新しい refresh_token も返し、古いものは invalid_grant", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const fake = createFakeKintoneOAuth({ mode: "A", rotate: true });
    await fake.token(tokenCall("authorization_code"));
    vi.setSystemTime(Date.now() + 3600_000);
    const body = await json(fake.token(tokenCall("refresh_token", "kintone-rt-1")));
    expect(body.refresh_token).toBe("kintone-rt-2");
    const old = fake.token(tokenCall("refresh_token", "kintone-rt-1"));
    expect(old.status).toBe(400);
    expect((await json(old)).error).toBe("invalid_grant");
  });

  it("revoke() 後は期限内でも apps が 401 になり、次の refresh は新しいトークンを発行する", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const fake = createFakeKintoneOAuth({ mode: "A" });
    await fake.token(tokenCall("authorization_code"));
    expect(fake.apps(appsCall("kintone-at-1")).status).toBe(200);
    fake.revoke();
    expect(fake.apps(appsCall("kintone-at-1")).status).toBe(401);
    const body = await json(fake.token(tokenCall("refresh_token", "kintone-rt-1")));
    expect(body).toMatchObject({ access_token: "kintone-at-2", expires_in: 3600 });
    expect(fake.refreshesReturningSameToken).toBe(0);
    expect(fake.apps(appsCall("kintone-at-2")).status).toBe(200);
  });

  it("apps は現在のトークンで期限内なら 200、古い・不明なトークン・ヘッダーなし・期限切れは 401", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const fake = createFakeKintoneOAuth({ mode: "A" });
    await fake.token(tokenCall("authorization_code"));
    expect(fake.apps(appsCall("kintone-at-1")).status).toBe(200);
    expect(fake.apps(appsCall("other")).status).toBe(401);
    expect(fake.apps({ ...appsCall(""), headers: new Headers() }).status).toBe(401);
    vi.setSystemTime(Date.now() + 3600_000);
    expect(fake.apps(appsCall("kintone-at-1")).status).toBe(401);
  });

  it("refreshStatus を指定すると refresh は常にそのステータスと invalid_grant で失敗する", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    const fake = createFakeKintoneOAuth({ refreshStatus: 400 });
    await fake.token(tokenCall("authorization_code"));
    const res = fake.token(tokenCall("refresh_token", "kintone-rt-1"));
    expect(res.status).toBe(400);
    expect((await json(res)).error).toBe("invalid_grant");
  });
});
