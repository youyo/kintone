import { env } from "cloudflare:workers";
import { describe, expect, it } from "vitest";
import {
  clearKintoneStale,
  grantRefFromAccessToken,
  isKintoneStale,
  KINTONE_STALE_TTL_SECONDS,
  kintoneStaleKey,
  markKintoneStale,
} from "../../src/auth/kintone-stale";

describe("stale 印", () => {
  it("キーは kintone-stale:<userId>:<grantId>、TTL は 15 分", () => {
    expect(kintoneStaleKey("u1", "g1")).toBe("kintone-stale:u1:g1");
    expect(KINTONE_STALE_TTL_SECONDS).toBe(15 * 60);
  });

  it('値は "1" だけで、TTL つきで書かれ、読み取り・削除できる', async () => {
    const puts: { key: string; value: string; options?: KVNamespacePutOptions }[] = [];
    const kv = {
      put: async (key: string, value: string, options?: KVNamespacePutOptions) => {
        puts.push({ key, value, options });
      },
      get: async () => "1",
      delete: async () => {},
    } as unknown as KVNamespace;
    await markKintoneStale(kv, "u1", "g1");
    expect(puts).toEqual([
      { key: "kintone-stale:u1:g1", value: "1", options: { expirationTtl: 15 * 60 } },
    ]);

    await markKintoneStale(env.OAUTH_KV, "u-real", "g-real");
    expect(await env.OAUTH_KV.get(kintoneStaleKey("u-real", "g-real"))).toBe("1");
    expect(await isKintoneStale(env.OAUTH_KV, "u-real", "g-real")).toBe(true);
    expect(await isKintoneStale(env.OAUTH_KV, "u-real", "g-other")).toBe(false);
    await clearKintoneStale(env.OAUTH_KV, "u-real", "g-real");
    expect(await isKintoneStale(env.OAUTH_KV, "u-real", "g-real")).toBe(false);
  });
});

describe("grantRefFromAccessToken", () => {
  it("{userId}:{grantId}:{secret} から userId と grantId を返す", () => {
    expect(grantRefFromAccessToken("user_Id-43:grant-1:secret")).toEqual({
      userId: "user_Id-43",
      grantId: "grant-1",
    });
  });

  it.each([
    ["undefined", undefined],
    ["空文字", ""],
    ["`:` が 0 個", "plain-token"],
    ["`:` が 1 個", "42:grant-1"],
    ["4 要素(`:` が 3 個)", "42:grant-1:secret:extra"],
    ["空の userId", ":grant-1:secret"],
    ["空の grantId", "42::secret"],
    ["空の userId と grantId", "::secret"],
  ])("印を置かない: %s", (_name, token) => {
    expect(grantRefFromAccessToken(token)).toBeUndefined();
  });
});
