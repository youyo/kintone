import { afterEach, describe, expect, it, vi } from "vitest";
import {
  appCacheKey,
  CACHE_TTL_SECONDS,
  createResolverCache,
  fieldsCacheKey,
} from "../../src/resolver/cache";
import { fakeKv } from "../helpers/fake-kv";

afterEach(() => vi.restoreAllMocks());

const GRANT = { userId: "user-a", grantId: "grant-1" };
const APPS = [{ appId: "12", code: "CRM", name: "顧客管理" }];
const FIELDS = [{ code: "customer", label: "顧客名", type: "SINGLE_LINE_TEXT" }];

/** 文字列の SHA-256 の先頭 16 文字(16 進)。 */
async function sha16(text: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return [...new Uint8Array(digest)]
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("")
    .slice(0, 16);
}

describe("キャッシュのキー(仕様書 §10。grant 単位)", () => {
  it("TTL は 600 秒", () => {
    expect(CACHE_TTL_SECONDS).toBe(600);
  });

  it("アプリは kcache:v1:<userId>:<grantId>:appcode|appname:<ref の SHA-256 先頭 16 文字>", async () => {
    const h = await sha16("顧客管理");
    expect(h).toHaveLength(16);
    expect(await appCacheKey(GRANT, "code", "顧客管理")).toBe(
      `kcache:v1:user-a:grant-1:appcode:${h}`,
    );
    expect(await appCacheKey(GRANT, "name", "顧客管理")).toBe(
      `kcache:v1:user-a:grant-1:appname:${h}`,
    );
  });

  it("フィールドは kcache:v1:<userId>:<grantId>:fields:<appId>", () => {
    expect(fieldsCacheKey(GRANT, 12)).toBe("kcache:v1:user-a:grant-1:fields:12");
  });

  it("userId と grantId の両方がキーに入り、片方が違えば別のキーになる(email 単位にしない)", async () => {
    const keys = await Promise.all([
      appCacheKey(GRANT, "name", "x"),
      appCacheKey({ userId: "user-a", grantId: "grant-2" }, "name", "x"),
      appCacheKey({ userId: "user-b", grantId: "grant-1" }, "name", "x"),
    ]);
    expect(new Set(keys).size).toBe(3);
  });

  it("キーは ref の中身(アプリ名)をそのまま含まず、長い ref でも 512 バイトに収まる", async () => {
    const key = await appCacheKey(GRANT, "name", "顧客管理".repeat(500));
    expect(key).not.toContain("顧客");
    expect(new TextEncoder().encode(key).length).toBeLessThan(512);
  });
});

describe("アプリのキャッシュ", () => {
  it("put は TTL 600 秒つきで書き、get は同じ値を返す", async () => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    expect(await cache.getApps("name", "顧客")).toBeUndefined();
    await cache.putApps("name", "顧客", APPS);
    expect(f.puts).toHaveLength(1);
    expect(f.puts[0]?.key).toBe(await appCacheKey(GRANT, "name", "顧客"));
    expect(f.puts[0]?.options).toEqual({ expirationTtl: 600 });
    expect(await cache.getApps("name", "顧客")).toEqual(APPS);
  });

  it("空配列(見つからなかった結果)もキャッシュできる", async () => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    await cache.putApps("code", "nothing", []);
    expect(await cache.getApps("code", "nothing")).toEqual([]);
  });

  it("コードと名前は別のキー。grant が違えば共有しない", async () => {
    const f = fakeKv();
    await createResolverCache(f.kv, GRANT).putApps("code", "x", APPS);
    expect(await createResolverCache(f.kv, GRANT).getApps("name", "x")).toBeUndefined();
    expect(
      await createResolverCache(f.kv, { userId: "user-a", grantId: "grant-2" }).getApps("code", "x"),
    ).toBeUndefined();
    expect(
      await createResolverCache(f.kv, { userId: "user-b", grantId: "grant-1" }).getApps("code", "x"),
    ).toBeUndefined();
  });

  it("deleteApps で消える", async () => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    await cache.putApps("name", "x", APPS);
    await cache.deleteApps("name", "x");
    expect(await cache.getApps("name", "x")).toBeUndefined();
  });

  it.each([
    ["JSON でない", "not json"],
    ["配列でない", JSON.stringify({ appId: "1" })],
    ["要素の形が違う", JSON.stringify([{ appId: 1, code: "A", name: "B" }])],
    ["必須のキーがない", JSON.stringify([{ appId: "1", code: "A" }])],
  ])("壊れた値(%s)は「なし」として扱う", async (_n, raw) => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    f.store.set(await appCacheKey(GRANT, "name", "x"), raw);
    expect(await cache.getApps("name", "x")).toBeUndefined();
  });
});

describe("フィールドのキャッシュ", () => {
  it("put は TTL 600 秒つき。get は同じ値を返し、deleteFields で消える", async () => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    expect(await cache.getFields(12)).toBeUndefined();
    await cache.putFields(12, FIELDS);
    expect(f.puts[0]).toMatchObject({
      key: "kcache:v1:user-a:grant-1:fields:12",
      options: { expirationTtl: 600 },
    });
    expect(await cache.getFields(12)).toEqual(FIELDS);
    expect(await cache.getFields(13)).toBeUndefined();
    await cache.deleteFields(12);
    expect(await cache.getFields(12)).toBeUndefined();
  });

  it("壊れた値は「なし」", async () => {
    const f = fakeKv({
      [fieldsCacheKey(GRANT, 12)]: JSON.stringify([{ code: "a", label: "b" }]),
    });
    expect(await createResolverCache(f.kv, GRANT).getFields(12)).toBeUndefined();
  });

  it("値は code / label / type だけ(余計な項目を保存しない)", async () => {
    const f = fakeKv();
    const cache = createResolverCache(f.kv, GRANT);
    await cache.putFields(12, [
      { code: "a", label: "A", type: "NUMBER", extra: "creator@example.com" } as never,
    ]);
    expect(JSON.parse(f.puts[0]?.value ?? "null")).toEqual([
      { code: "a", label: "A", type: "NUMBER" },
    ]);
  });
});

describe("KV の失敗はツールを止めない", () => {
  it("get / put / delete が例外でも、例外を出さずに「なし」・何もしないになる", async () => {
    const f = fakeKv();
    f.fail.get = true;
    f.fail.put = true;
    f.fail.delete = true;
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const cache = createResolverCache(f.kv, GRANT);
    expect(await cache.getApps("name", "x")).toBeUndefined();
    expect(await cache.getFields(1)).toBeUndefined();
    await expect(cache.putApps("name", "x", APPS)).resolves.toBeUndefined();
    await expect(cache.putFields(1, FIELDS)).resolves.toBeUndefined();
    await expect(cache.deleteApps("name", "x")).resolves.toBeUndefined();
    await expect(cache.deleteFields(1)).resolves.toBeUndefined();
    expect(warn).toHaveBeenCalled();
  });

  it("ログには例外名だけを出し、例外の message・キー・値(アプリ名)を出さない", async () => {
    const f = fakeKv();
    f.fail.get = true;
    f.fail.put = true;
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const cache = createResolverCache(f.kv, GRANT);
    await cache.getApps("name", "顧客管理");
    await cache.putApps("name", "顧客管理", APPS);
    const logged = [...warn.mock.calls, ...error.mock.calls].flat().join("\n");
    expect(logged).toContain("Error");
    expect(logged).not.toContain("secret-kv-detail");
    expect(logged).not.toContain("kcache");
    expect(logged).not.toContain("user-a");
    expect(logged).not.toContain("grant-1");
    expect(logged).not.toContain("顧客管理");
  });
});

describe("キャッシュを使えないとき", () => {
  it("KV がない・grant が分からないときは、何も読まず・書かない", async () => {
    const f = fakeKv();
    for (const cache of [
      createResolverCache(undefined, GRANT),
      createResolverCache(f.kv, undefined),
      createResolverCache(undefined, undefined),
    ]) {
      await cache.putApps("name", "x", APPS);
      await cache.putFields(1, FIELDS);
      expect(await cache.getApps("name", "x")).toBeUndefined();
      expect(await cache.getFields(1)).toBeUndefined();
      await cache.deleteApps("name", "x");
      await cache.deleteFields(1);
    }
    expect(f.puts).toHaveLength(0);
    expect(f.gets).toHaveLength(0);
    expect(f.deletes).toHaveLength(0);
  });
});
