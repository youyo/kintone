import { describe, expect, it } from "vitest";
import { ArgumentError, type ResolverError } from "../../src/errors";
import type { Query } from "../../src/kintone/client";
import { createResolver } from "../../src/resolver/resolver";
import { fakeKintoneClient } from "../helpers/fake-kintone-client";
import { type AppRow, listApps, manyApps } from "../helpers/fake-kintone-data";

interface Call {
  path: string;
  query: Query | undefined;
}

function setup(all: AppRow[]) {
  const calls: Call[] = [];
  const client = fakeKintoneClient({
    get: async (path, query) => {
      calls.push({ path, query });
      if (path === "/k/v1/apps.json") return listApps(all, query);
      throw new Error(`unexpected path ${path}`);
    },
  });
  // M03 のこのファイルはキャッシュなし(キャッシュは resolver-cache-behavior.test.ts)。
  const resolver = createResolver({ client });
  return { calls, resolver };
}

const failure = async (p: Promise<unknown>) => {
  try {
    await p;
  } catch (e) {
    return e as ResolverError;
  }
  throw new Error("expected rejection");
};

const CRM: AppRow = { appId: "12", code: "CRM", name: "顧客管理" };
const TASK: AppRow = { appId: "13", code: "TASK", name: "タスク" };

describe("resolveApp: 解決順序(ID → コード完全一致 → 名前完全一致 → 名前部分一致)", () => {
  it("空文字は argument_error(API を呼ばない)", async () => {
    const { resolver, calls } = setup([CRM]);
    await expect(resolver.resolveApp("", { mode: "read" })).rejects.toBeInstanceOf(ArgumentError);
    expect(calls).toHaveLength(0);
  });

  it.each(["write", "read"] as const)(
    "正の整数の文字列は ID としてそのまま採用し、API を呼ばない(%s)",
    async (mode) => {
      const { resolver, calls } = setup([CRM]);
      const r = await resolver.resolveApp("999", { mode });
      expect(r.appId).toBe(999);
      expect(r.fromCache).toBe(false);
      expect(calls).toHaveLength(0);
    },
  );

  it("数字だけのコードがあっても ID が優先される(コードの検索をしない)", async () => {
    const { resolver, calls } = setup([{ appId: "5", code: "100", name: "百" }]);
    expect((await resolver.resolveApp("100", { mode: "read" })).appId).toBe(100);
    expect(calls).toHaveLength(0);
  });

  it.each(["0", "-3", "1.5", "1e3", "12abc", " 12", "+5"])(
    "ID とみなさない文字列(%s)はコードの検索へ進む",
    async (ref) => {
      const { resolver, calls } = setup([]);
      await failure(resolver.resolveApp(ref, { mode: "read" }));
      expect(calls[0]).toMatchObject({ path: "/k/v1/apps.json", query: { codes: [ref] } });
    },
  );

  it("コード完全一致 1 件は採用。コードの検索 1 回で終わり、名前の検索はしない", async () => {
    const { resolver, calls } = setup([CRM, TASK]);
    const r = await resolver.resolveApp("CRM", { mode: "read" });
    expect(r.appId).toBe(12);
    expect(calls).toEqual([{ path: "/k/v1/apps.json", query: { codes: ["CRM"], limit: 100 } }]);
  });

  it("コードは大文字小文字を区別する(crm は CRM に当たらず、名前の検索へ進む)", async () => {
    const { resolver, calls } = setup([CRM]);
    const e = await failure(resolver.resolveApp("crm", { mode: "read" }));
    expect(e.code).toBe("resolver_app_not_found");
    expect(calls).toHaveLength(2);
  });

  it("コードが 2 件以上に一致したら resolver_app_ambiguous(名前へフォールバックしない)", async () => {
    const { resolver, calls } = setup([
      { appId: "1", code: "DUP", name: "A" },
      { appId: "2", code: "DUP", name: "B" },
    ]);
    const e = await failure(resolver.resolveApp("DUP", { mode: "read" }));
    expect(e.code).toBe("resolver_app_ambiguous");
    expect(e.details).toEqual({
      kind: "app",
      ref: "DUP",
      candidates: [
        { id: "1", code: "DUP", name: "A" },
        { id: "2", code: "DUP", name: "B" },
      ],
    });
    expect(calls).toHaveLength(1);
  });

  it("コードに当たらなければ名前の検索へ進む。名前の完全一致 1 件は採用", async () => {
    const { resolver, calls } = setup([CRM, { appId: "14", code: "CRM2", name: "顧客管理の予備" }]);
    const r = await resolver.resolveApp("顧客管理", { mode: "read" });
    expect(r.appId).toBe(12);
    expect(calls).toEqual([
      { path: "/k/v1/apps.json", query: { codes: ["顧客管理"], limit: 100 } },
      { path: "/k/v1/apps.json", query: { name: "顧客管理", limit: 100, offset: 0 } },
    ]);
  });

  it("名前の完全一致があれば、ほかに部分一致があっても完全一致が優先される(両モード)", async () => {
    for (const mode of ["read", "write"] as const) {
      const { resolver } = setup([
        { appId: "14", code: "B", name: "顧客管理の予備" },
        CRM,
        { appId: "15", code: "C", name: "旧顧客管理" },
      ]);
      expect((await resolver.resolveApp("顧客管理", { mode })).appId).toBe(12);
    }
  });

  it("名前の完全一致は大文字小文字を区別する(kintone の検索は区別しないが、完全一致は区別)", async () => {
    const { resolver } = setup([
      { appId: "1", code: "A", name: "Sales" },
      { appId: "2", code: "B", name: "sales" },
    ]);
    expect((await resolver.resolveApp("Sales", { mode: "write" })).appId).toBe(1);
  });

  it.each(["read", "write"] as const)(
    "名前の完全一致が 2 件以上なら resolver_app_ambiguous(%s)",
    async (mode) => {
      const { resolver } = setup([
        { appId: "1", code: "A", name: "同名" },
        { appId: "2", code: "B", name: "同名" },
        { appId: "3", code: "C", name: "同名の別アプリ" },
      ]);
      const e = await failure(resolver.resolveApp("同名", { mode }));
      expect(e.code).toBe("resolver_app_ambiguous");
      // 候補は完全一致だけ。
      expect(e.details?.candidates).toEqual([
        { id: "1", code: "A", name: "同名" },
        { id: "2", code: "B", name: "同名" },
      ]);
    },
  );
});

describe("resolveApp: 部分一致(read モード)", () => {
  it("完全一致がなく部分一致が 1 件なら採用", async () => {
    const { resolver } = setup([CRM, TASK]);
    expect((await resolver.resolveApp("顧客", { mode: "read" })).appId).toBe(12);
  });

  it("部分一致が 2 件以上なら resolver_app_ambiguous(candidates は {id, code, name})", async () => {
    const { resolver } = setup([CRM, { appId: "14", code: "OLD", name: "旧顧客台帳" }, TASK]);
    const e = await failure(resolver.resolveApp("顧客", { mode: "read" }));
    expect(e.code).toBe("resolver_app_ambiguous");
    expect(e.retryable).toBe(false);
    expect(e.details).toEqual({
      kind: "app",
      ref: "顧客",
      candidates: [
        { id: "12", code: "CRM", name: "顧客管理" },
        { id: "14", code: "OLD", name: "旧顧客台帳" },
      ],
    });
  });

  it("どれにも当たらなければ resolver_app_not_found(candidates は空配列)", async () => {
    const { resolver } = setup([CRM]);
    const e = await failure(resolver.resolveApp("存在しない", { mode: "read" }));
    expect(e.code).toBe("resolver_app_not_found");
    expect(e.details).toEqual({ kind: "app", ref: "存在しない", candidates: [] });
  });

  it("candidates の空の項目は省略する", async () => {
    const { resolver } = setup([
      { appId: "1", code: "", name: "共通A" },
      { appId: "2", code: "X", name: "共通B" },
    ]);
    const e = await failure(resolver.resolveApp("共通", { mode: "read" }));
    expect(e.details?.candidates).toEqual([
      { id: "1", name: "共通A" },
      { id: "2", code: "X", name: "共通B" },
    ]);
  });
});

describe("resolveApp: write モードは完全一致だけ(ADR 0007)", () => {
  it("ID は両モード共通で採用", async () => {
    const { resolver } = setup([]);
    expect((await resolver.resolveApp("12", { mode: "write" })).appId).toBe(12);
  });

  it("コード完全一致は採用", async () => {
    const { resolver } = setup([CRM]);
    expect((await resolver.resolveApp("CRM", { mode: "write" })).appId).toBe(12);
  });

  it("名前の完全一致は採用", async () => {
    const { resolver } = setup([CRM]);
    expect((await resolver.resolveApp("顧客管理", { mode: "write" })).appId).toBe(12);
  });

  it("部分一致が 1 件でも採用せず resolver_app_exact_match_required(候補つき)", async () => {
    const { resolver } = setup([CRM, TASK]);
    const e = await failure(resolver.resolveApp("顧客", { mode: "write" }));
    expect(e.code).toBe("resolver_app_exact_match_required");
    expect(e.retryable).toBe(false);
    expect(e.details).toEqual({
      kind: "app",
      ref: "顧客",
      candidates: [{ id: "12", code: "CRM", name: "顧客管理" }],
    });
    expect(e.message).toMatch(/exact/i);
  });

  it("部分一致が複数件でも resolver_app_exact_match_required(候補はすべて)", async () => {
    const { resolver } = setup([CRM, { appId: "14", code: "OLD", name: "旧顧客台帳" }]);
    const e = await failure(resolver.resolveApp("顧客", { mode: "write" }));
    expect(e.code).toBe("resolver_app_exact_match_required");
    expect(e.details?.candidates).toHaveLength(2);
  });

  it("部分一致も 0 件なら resolver_app_not_found", async () => {
    const { resolver } = setup([CRM]);
    const e = await failure(resolver.resolveApp("存在しない", { mode: "write" }));
    expect(e.code).toBe("resolver_app_not_found");
  });

  it("API の呼び出し回数・順序は read と同じ", async () => {
    const rows = [CRM, TASK];
    const read = setup(rows);
    const write = setup(rows);
    await read.resolver.resolveApp("顧客", { mode: "read" });
    await failure(write.resolver.resolveApp("顧客", { mode: "write" }));
    expect(write.calls).toEqual(read.calls);
  });
});

describe("resolveApp: 名前検索のページング(最大 10 ページ)", () => {
  const nameCalls = (calls: Call[]) => calls.filter((c) => c.query?.name !== undefined);

  it("100 件ずつ offset を進めて取得する(最後のページが 100 件未満で終わる)", async () => {
    const rows = [...manyApps(250, "共通"), { appId: "1", code: "T", name: "共通" }];
    const { resolver, calls } = setup(rows);
    const r = await resolver.resolveApp("共通", { mode: "read" });
    expect(r.appId).toBe(1);
    expect(nameCalls(calls).map((c) => c.query)).toEqual([
      { name: "共通", limit: 100, offset: 0 },
      { name: "共通", limit: 100, offset: 100 },
      { name: "共通", limit: 100, offset: 200 },
    ]);
  });

  it("10 ページ目が 100 件未満なら成功(コード 1 + 名前 10 = 11 回)", async () => {
    const rows = [...manyApps(949, "共通"), { appId: "1", code: "T", name: "共通" }];
    const { resolver, calls } = setup(rows);
    expect((await resolver.resolveApp("共通", { mode: "read" })).appId).toBe(1);
    expect(nameCalls(calls)).toHaveLength(10);
    expect(calls).toHaveLength(11);
  });

  it("10 ページ全部が満杯なら resolver_app_list_too_large(11 回で止まり、11 ページ目は取らない)", async () => {
    const { resolver, calls } = setup(manyApps(1500, "共通"));
    const e = await failure(resolver.resolveApp("共通", { mode: "read" }));
    expect(e.code).toBe("resolver_app_list_too_large");
    expect(e.retryable).toBe(false);
    expect(e.details).toMatchObject({ kind: "app", ref: "共通" });
    expect(nameCalls(calls)).toHaveLength(10);
    expect(calls).toHaveLength(11);
  });

  it("ちょうど 1000 件でも(10 ページ目が満杯なので)resolver_app_list_too_large", async () => {
    const { resolver } = setup(manyApps(1000, "共通"));
    const e = await failure(resolver.resolveApp("共通", { mode: "read" }));
    expect(e.code).toBe("resolver_app_list_too_large");
  });

  it("write モードでも同じ上限", async () => {
    const { resolver } = setup(manyApps(1500, "共通"));
    const e = await failure(resolver.resolveApp("共通", { mode: "write" }));
    expect(e.code).toBe("resolver_app_list_too_large");
  });
});

describe("resolveApp: kintone のエラーはそのまま伝わる", () => {
  it("apps.json の失敗は握りつぶさず投げ直す", async () => {
    const client = fakeKintoneClient({
      get: async () => {
        throw new Error("boom");
      },
    });
    const resolver = createResolver({ client });
    await expect(resolver.resolveApp("CRM", { mode: "read" })).rejects.toThrow("boom");
  });
});
