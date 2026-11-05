import { afterEach, describe, expect, it, vi } from "vitest";
import type { ResolverError } from "../../src/errors";
import { KintoneApiError, type Query } from "../../src/kintone/client";
import { appCacheKey, createResolverCache, fieldsCacheKey } from "../../src/resolver/cache";
import { createResolver } from "../../src/resolver/resolver";
import { type AppRow, fieldsResponse, listApps, manyApps } from "../helpers/fake-kintone-data";
import { fakeKintoneClient } from "../helpers/fake-kintone-client";
import { fakeKv } from "../helpers/fake-kv";

afterEach(() => vi.restoreAllMocks());

const GRANT = { userId: "user-a", grantId: "grant-1" };
const CRM: AppRow = { appId: "12", code: "CRM", name: "顧客管理" };

/** 可変のアプリ一覧とフィールド定義を持つ kintone の偽物(呼び出しを数える)。 */
function setup(initial: { apps: AppRow[]; kv?: ReturnType<typeof fakeKv> | undefined }) {
  const state = { apps: initial.apps, calls: [] as { path: string; query: Query | undefined }[] };
  const f = initial.kv ?? fakeKv();
  const client = fakeKintoneClient({
    get: async (path, query) => {
      state.calls.push({ path, query });
      if (path === "/k/v1/apps.json") return listApps(state.apps, query);
      if (path === "/k/v1/app/form/fields.json") {
        return fieldsResponse([{ type: "NUMBER", code: "amount", label: "金額" }]);
      }
      throw new Error(`unexpected path ${path}`);
    },
  });
  const mk = () => createResolver({ client, cache: createResolverCache(f.kv, GRANT) });
  return { state, f, mk, resolver: mk() };
}

const failure = async (p: Promise<unknown>) => {
  try {
    await p;
  } catch (e) {
    return e as ResolverError;
  }
  throw new Error("expected rejection");
};

describe("キャッシュのヒット", () => {
  it("1 回目は API から取ってキャッシュし、(新しいリゾルバーの)2 回目は API を呼ばない", async () => {
    const { f, mk, state } = setup({ apps: [CRM] });
    const first = await mk().resolveApp("顧客管理", { mode: "read" });
    expect(first.fromCache).toBe(false);
    expect(state.calls).toHaveLength(2); // コード + 名前
    // コードの検索結果(空)も名前の検索結果もキャッシュされる。
    expect(f.puts.map((p) => p.key).sort()).toEqual(
      [await appCacheKey(GRANT, "code", "顧客管理"), await appCacheKey(GRANT, "name", "顧客管理")].sort(),
    );
    for (const p of f.puts) expect(p.options).toEqual({ expirationTtl: 600 });

    state.calls.length = 0;
    const second = await mk().resolveApp("顧客管理", { mode: "read" });
    expect(second.appId).toBe(12);
    expect(second.fromCache).toBe(true);
    expect(state.calls).toHaveLength(0);
  });

  it("コードで解決した場合、コードの検索結果がキャッシュされ、2 回目は API を呼ばない", async () => {
    const { mk, state } = setup({ apps: [CRM] });
    await mk().resolveApp("CRM", { mode: "read" });
    expect(state.calls).toHaveLength(1);
    state.calls.length = 0;
    expect((await mk().resolveApp("CRM", { mode: "read" })).appId).toBe(12);
    expect(state.calls).toHaveLength(0);
  });

  it("ID はキャッシュも API も使わない", async () => {
    const { f, mk, state } = setup({ apps: [] });
    await mk().resolveApp("12", { mode: "read" });
    expect(state.calls).toHaveLength(0);
    expect(f.gets).toHaveLength(0);
    expect(f.puts).toHaveLength(0);
  });

  it("キャッシュには値が両モードで同じ形(全ページ)で入り、モードは判定時に適用される", async () => {
    const { mk, state } = setup({ apps: [CRM, { appId: "13", code: "T", name: "タスク" }] });
    // read で部分一致を解決 → キャッシュ → write は同じキャッシュから exact_match_required。
    expect((await mk().resolveApp("顧客", { mode: "read" })).appId).toBe(12);
    state.calls.length = 0;
    const e = await failure(mk().resolveApp("顧客", { mode: "write" }));
    expect(e.code).toBe("resolver_app_exact_match_required");
    expect(state.calls).toHaveLength(0);
  });

  it("別の grant・別の利用者のキャッシュは使わない", async () => {
    const { f, state } = setup({ apps: [CRM] });
    const a = createResolver({
      client: fakeKintoneClient({
        get: async (path, query) => {
          state.calls.push({ path, query });
          return listApps(state.apps, query);
        },
      }),
      cache: createResolverCache(f.kv, GRANT),
    });
    await a.resolveApp("CRM", { mode: "read" });
    state.calls.length = 0;
    for (const other of [
      { userId: "user-a", grantId: "grant-2" },
      { userId: "user-b", grantId: "grant-1" },
    ]) {
      const r = createResolver({
        client: fakeKintoneClient({
          get: async (path, query) => {
            state.calls.push({ path, query });
            return listApps(state.apps, query);
          },
        }),
        cache: createResolverCache(f.kv, other),
      });
      expect((await r.resolveApp("CRM", { mode: "read" })).fromCache).toBe(false);
    }
    expect(state.calls).toHaveLength(2);
  });
});

describe("not_found の取り直し(1 回だけ。読んだ段階だけ)", () => {
  it("キャッシュに『ない』と入っていても、あとから作られたアプリは取り直しで見つかる", async () => {
    const { state, mk } = setup({ apps: [] });
    const e = await failure(mk().resolveApp("新規", { mode: "read" }));
    expect(e.code).toBe("resolver_app_not_found");
    expect(state.calls).toHaveLength(2);

    state.apps = [{ appId: "20", code: "NEW", name: "新規" }];
    state.calls.length = 0;
    const r = await mk().resolveApp("新規", { mode: "read" });
    expect(r.appId).toBe(20);
    // コードも名前も、キャッシュ由来だったので両方を取り直す(2 回)。
    expect(state.calls).toHaveLength(2);
    // 取り直した結果でキャッシュが更新され、次はキャッシュだけで解決する。
    state.calls.length = 0;
    expect((await mk().resolveApp("新規", { mode: "read" })).fromCache).toBe(true);
    expect(state.calls).toHaveLength(0);
  });

  it("取り直しても見つからなければ、そのまま resolver_app_not_found(取り直しは 1 回だけ)", async () => {
    const { state, mk } = setup({ apps: [] });
    await failure(mk().resolveApp("無い", { mode: "read" }));
    state.calls.length = 0;
    const e = await failure(mk().resolveApp("無い", { mode: "read" }));
    expect(e.code).toBe("resolver_app_not_found");
    expect(state.calls).toHaveLength(2); // コード 1 + 名前 1(1 回だけ取り直す)
  });

  it("キャッシュを使っていない(最初の取得で not_found)ときは取り直さない", async () => {
    const { state, mk } = setup({ apps: [] });
    await failure(mk().resolveApp("無い", { mode: "read" }));
    expect(state.calls).toHaveLength(2);
  });

  it("コードの結果だけがキャッシュ由来なら、コードの段階だけ取り直す(合計 11 回を超えない)", async () => {
    const rows = [...manyApps(949, "共通")];
    const { f, state, mk } = setup({ apps: rows });
    // コードの『なし』だけをキャッシュに入れておく。
    f.store.set(await appCacheKey(GRANT, "code", "共通ZZ"), "[]");
    state.calls.length = 0;
    const e = await failure(mk().resolveApp("共通ZZ", { mode: "read" }));
    expect(e.code).toBe("resolver_app_not_found");
    // 名前は 1 ページ(部分一致 0 件)+ コードの取り直し 1 回。
    const names = state.calls.filter((c) => c.query?.name !== undefined);
    const codes = state.calls.filter((c) => c.query?.codes !== undefined);
    expect(names).toHaveLength(1);
    expect(codes).toHaveLength(1);
  });

  it("部分一致が多数(ambiguous)なら取り直さない。コードがキャッシュ済みなら名前の 10 ページだけ", async () => {
    // 949 件が部分一致(10 ページ目は 49 件で終わる)。コードの『なし』だけキャッシュ済み。
    const rows = manyApps(949, "共通");
    const { f, state, mk } = setup({ apps: rows });
    f.store.set(await appCacheKey(GRANT, "code", "共"), "[]");
    state.calls.length = 0;
    // 「共」は部分一致 949 件 → ambiguous で終わる(not_found ではない)ので取り直さない。
    const e = await failure(mk().resolveApp("共", { mode: "read" }));
    expect(e.code).toBe("resolver_app_ambiguous");
    expect(state.calls).toHaveLength(10);
  });

  it("KV の書き込みが失敗しても、取り直しで同じ段階を二重に取らない(メモ化)", async () => {
    const f = fakeKv();
    const { state, mk } = setup({ apps: [], kv: f });
    // 『コードの結果だけキャッシュ済み・名前は未キャッシュ・書き込みは失敗』の状態にする。
    f.store.set(await appCacheKey(GRANT, "code", "無い"), "[]");
    f.fail.put = true;
    vi.spyOn(console, "warn").mockImplementation(() => {});
    state.calls.length = 0;
    const e = await failure(mk().resolveApp("無い", { mode: "read" }));
    expect(e.code).toBe("resolver_app_not_found");
    const names = state.calls.filter((c) => c.query?.name !== undefined);
    expect(names).toHaveLength(1); // 取り直しの 2 周目でも名前を再取得しない
    expect(state.calls.filter((c) => c.query?.codes !== undefined)).toHaveLength(1);
  });

  it("フィールド: キャッシュ由来の not_found は 1 回だけ取り直す", async () => {
    const { f, state, mk } = setup({ apps: [] });
    f.store.set(fieldsCacheKey(GRANT, 12), JSON.stringify([{ code: "old", label: "旧", type: "NUMBER" }]));
    const code = await mk().resolveField(12, "金額", { mode: "read" });
    expect(code).toBe("amount");
    expect(state.calls).toHaveLength(1);
    expect(state.calls[0]?.path).toBe("/k/v1/app/form/fields.json");
    // 取り直した定義でキャッシュが更新される。
    state.calls.length = 0;
    expect(await mk().resolveField(12, "金額", { mode: "read" })).toBe("amount");
    expect(state.calls).toHaveLength(0);
  });

  it("フィールド: キャッシュがなければ API を 1 回呼び、not_found でも取り直さない", async () => {
    const { state, mk } = setup({ apps: [] });
    const e = await failure(mk().resolveField(12, "存在しない", { mode: "read" }));
    expect(e.code).toBe("resolver_field_not_found");
    expect(state.calls).toHaveLength(1);
  });

  it("フィールド: キャッシュ済みの定義で解決でき、API を呼ばない。取り直しても無ければ not_found", async () => {
    const { f, state, mk } = setup({ apps: [] });
    f.store.set(fieldsCacheKey(GRANT, 12), JSON.stringify([{ code: "amount", label: "金額", type: "NUMBER" }]));
    expect(await mk().resolveField(12, "金額", { mode: "write" })).toBe("amount");
    expect(state.calls).toHaveLength(0);
    const e = await failure(mk().resolveField(12, "存在しない", { mode: "read" }));
    expect(e.code).toBe("resolver_field_not_found");
    expect(state.calls).toHaveLength(1);
  });
});

describe("KV が失敗しても続行する", () => {
  it("get が失敗しても API で解決できる(書き込みも失敗してよい)", async () => {
    const f = fakeKv();
    f.fail.get = true;
    f.fail.put = true;
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const { state, mk } = setup({ apps: [CRM], kv: f });
    const r = await mk().resolveApp("顧客管理", { mode: "read" });
    expect(r.appId).toBe(12);
    expect(state.calls).toHaveLength(2);
    expect(await mk().resolveField(12, "金額", { mode: "read" })).toBe("amount");
  });

  it("delete(invalidate)が失敗しても例外にならない", async () => {
    const f = fakeKv();
    const { mk } = setup({ apps: [CRM], kv: f });
    await mk().resolveApp("CRM", { mode: "read" });
    const r = await mk().resolveApp("CRM", { mode: "read" });
    expect(r.fromCache).toBe(true);
    f.fail.delete = true;
    vi.spyOn(console, "warn").mockImplementation(() => {});
    await expect(r.invalidate()).resolves.toBeUndefined();
  });
});

describe("useCache: false(app_describe 用)", () => {
  it("キャッシュを読まず API で解決するが、結果はキャッシュに書く", async () => {
    const { f, mk, state } = setup({ apps: [CRM] });
    await mk().resolveApp("CRM", { mode: "read" });
    state.calls.length = 0;
    f.gets.length = 0;
    f.puts.length = 0;
    const r = await mk().resolveApp("CRM", { mode: "read", useCache: false });
    expect(r.fromCache).toBe(false);
    expect(state.calls).toHaveLength(1);
    expect(f.gets).toHaveLength(0);
    expect(f.puts).toHaveLength(1);
  });

  it("not_found でも取り直さない(最初から最新を取っているため)", async () => {
    const { mk, state } = setup({ apps: [] });
    await failure(mk().resolveApp("無い", { mode: "read", useCache: false }));
    expect(state.calls).toHaveLength(2);
  });
});

describe("invalidate(キャッシュで解決したアプリへの呼び出しが not_found のとき)", () => {
  it("読んだ段階のキーとそのアプリのフィールドのキャッシュを消す。ほかのキーは消さない", async () => {
    const { f, mk } = setup({ apps: [CRM] });
    await mk().resolveApp("顧客管理", { mode: "read" });
    const cache = createResolverCache(f.kv, GRANT);
    await cache.putFields(12, [{ code: "a", label: "A", type: "NUMBER" }]);
    await cache.putFields(99, [{ code: "b", label: "B", type: "NUMBER" }]);
    await cache.putApps("name", "別の ref", [CRM]);

    const r = await mk().resolveApp("顧客管理", { mode: "read" });
    expect(r.fromCache).toBe(true);
    await r.invalidate();
    expect(await cache.getApps("code", "顧客管理")).toBeUndefined();
    expect(await cache.getApps("name", "顧客管理")).toBeUndefined();
    expect(await cache.getFields(12)).toBeUndefined();
    // 無関係なキーは残る。
    expect(await cache.getFields(99)).toBeDefined();
    expect(await cache.getApps("name", "別の ref")).toBeDefined();
  });

  it("API で取ったもの・ID 直接の解決の invalidate は何も消さない", async () => {
    const { f, mk } = setup({ apps: [CRM] });
    const fresh = await mk().resolveApp("CRM", { mode: "read" });
    const byId = await mk().resolveApp("12", { mode: "read" });
    f.deletes.length = 0;
    await fresh.invalidate();
    await byId.invalidate();
    expect(f.deletes).toHaveLength(0);
  });
});

describe("kintone のエラーはキャッシュ越しでも伝わる", () => {
  it("取り直しの API が失敗したらそのエラーを投げる", async () => {
    const f = fakeKv();
    f.store.set(await appCacheKey(GRANT, "code", "x"), "[]");
    f.store.set(await appCacheKey(GRANT, "name", "x"), "[]");
    const client = fakeKintoneClient({
      get: async () => {
        throw new KintoneApiError(503);
      },
    });
    const r = createResolver({ client, cache: createResolverCache(f.kv, GRANT) });
    await expect(r.resolveApp("x", { mode: "read" })).rejects.toBeInstanceOf(KintoneApiError);
  });
});
