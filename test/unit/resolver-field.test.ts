import { describe, expect, it } from "vitest";
import { ArgumentError, type ResolverError } from "../../src/errors";
import type { Query } from "../../src/kintone/client";
import { createResolver } from "../../src/resolver/resolver";
import { type FieldProp, fieldsResponse } from "../helpers/fake-kintone-data";
import { fakeKintoneClient } from "../helpers/fake-kintone-client";

const PROPS: FieldProp[] = [
  { type: "SINGLE_LINE_TEXT", code: "customer", label: "顧客名" },
  { type: "SINGLE_LINE_TEXT", code: "customer_code", label: "顧客コード" },
  { type: "NUMBER", code: "amount", label: "金額" },
  { type: "NUMBER", code: "tax", label: "金額(税込)" },
  { type: "SINGLE_LINE_TEXT", code: "Name", label: "Name Label" },
  { type: "SINGLE_LINE_TEXT", code: "dup1", label: "重複" },
  { type: "SINGLE_LINE_TEXT", code: "dup2", label: "重複" },
  {
    type: "SUBTABLE",
    code: "items",
    label: "明細",
    fields: { sub_code: { type: "NUMBER", code: "sub_code", label: "内側の項目" } },
  },
];

function setup(props: FieldProp[] = PROPS) {
  const calls: { path: string; query: Query | undefined }[] = [];
  const client = fakeKintoneClient({
    get: async (path, query) => {
      calls.push({ path, query });
      if (path === "/k/v1/app/form/fields.json") return fieldsResponse(props);
      throw new Error(`unexpected path ${path}`);
    },
  });
  return { calls, resolver: createResolver({ client }) };
}

const failure = async (p: Promise<unknown>) => {
  try {
    await p;
  } catch (e) {
    return e as ResolverError;
  }
  throw new Error("expected rejection");
};

describe("resolveField: コード完全一致 → label 完全一致 → label 部分一致", () => {
  it("form/fields.json?app=<id> を 1 回だけ呼ぶ", async () => {
    const { resolver, calls } = setup();
    await resolver.resolveField(12, "customer", { mode: "read" });
    expect(calls).toEqual([{ path: "/k/v1/app/form/fields.json", query: { app: 12 } }]);
  });

  it.each(["read", "write"] as const)("コード完全一致は採用(%s)", async (mode) => {
    const { resolver } = setup();
    expect(await resolver.resolveField(12, "customer", { mode })).toBe("customer");
  });

  it("コードが label の部分一致より優先される(customer は『顧客…』ではなくコードに当たる)", async () => {
    const { resolver } = setup([
      { type: "SINGLE_LINE_TEXT", code: "a", label: "customer note" },
      { type: "SINGLE_LINE_TEXT", code: "customer", label: "顧客" },
    ]);
    expect(await resolver.resolveField(1, "customer", { mode: "read" })).toBe("customer");
  });

  it("コードは大文字小文字を区別する(name は Name に当たらない)", async () => {
    const { resolver } = setup();
    const e = await failure(resolver.resolveField(12, "name", { mode: "read" }));
    expect(e.code).toBe("resolver_field_not_found");
  });

  it.each(["read", "write"] as const)("label の完全一致 1 件は採用(%s)", async (mode) => {
    const { resolver } = setup();
    expect(await resolver.resolveField(12, "顧客名", { mode })).toBe("customer");
  });

  it("label の完全一致があれば、部分一致が他にあっても完全一致を採用する", async () => {
    const { resolver } = setup();
    // 「金額」は amount の完全一致。tax の「金額(税込)」は部分一致。
    expect(await resolver.resolveField(12, "金額", { mode: "read" })).toBe("amount");
    expect(await resolver.resolveField(12, "金額", { mode: "write" })).toBe("amount");
  });

  it.each(["read", "write"] as const)(
    "label の完全一致が 2 件以上なら resolver_field_ambiguous(%s。候補は {code, label} のコード順)",
    async (mode) => {
      const { resolver } = setup();
      const e = await failure(resolver.resolveField(12, "重複", { mode }));
      expect(e.code).toBe("resolver_field_ambiguous");
      expect(e.retryable).toBe(false);
      expect(e.details).toEqual({
        kind: "field",
        ref: "重複",
        candidates: [
          { code: "dup1", label: "重複" },
          { code: "dup2", label: "重複" },
        ],
      });
    },
  );

  it("label は大文字小文字を区別する", async () => {
    const { resolver } = setup();
    expect(await resolver.resolveField(12, "Name Label", { mode: "write" })).toBe("Name");
    const e = await failure(resolver.resolveField(12, "name label", { mode: "read" }));
    expect(e.code).toBe("resolver_field_not_found");
  });

  it("サブテーブル内のフィールド(コード・label)は探さない", async () => {
    const { resolver } = setup();
    for (const ref of ["sub_code", "内側の項目"]) {
      const e = await failure(resolver.resolveField(12, ref, { mode: "read" }));
      expect(e.code, ref).toBe("resolver_field_not_found");
    }
  });

  it("どれにも当たらなければ resolver_field_not_found(candidates は空配列)", async () => {
    const { resolver } = setup();
    const e = await failure(resolver.resolveField(12, "存在しない", { mode: "read" }));
    expect(e.code).toBe("resolver_field_not_found");
    expect(e.details).toEqual({ kind: "field", ref: "存在しない", candidates: [] });
  });

  it("空文字は argument_error(API を呼ばない)", async () => {
    const { resolver, calls } = setup();
    await expect(resolver.resolveField(12, "", { mode: "read" })).rejects.toBeInstanceOf(
      ArgumentError,
    );
    expect(calls).toHaveLength(0);
  });
});

describe("resolveField: label の部分一致(read モード)", () => {
  it("1 件なら採用", async () => {
    const { resolver } = setup();
    expect(await resolver.resolveField(12, "顧客コ", { mode: "read" })).toBe("customer_code");
  });

  it("2 件以上なら resolver_field_ambiguous(候補はコード順)", async () => {
    const { resolver } = setup();
    const e = await failure(resolver.resolveField(12, "顧客", { mode: "read" }));
    expect(e.code).toBe("resolver_field_ambiguous");
    expect(e.details?.candidates).toEqual([
      { code: "customer", label: "顧客名" },
      { code: "customer_code", label: "顧客コード" },
    ]);
  });
});

describe("resolveField: write モードは完全一致だけ(ADR 0007)", () => {
  it("label の部分一致が 1 件でも採用せず resolver_field_exact_match_required(候補つき)", async () => {
    const { resolver } = setup();
    const e = await failure(resolver.resolveField(12, "顧客コ", { mode: "write" }));
    expect(e.code).toBe("resolver_field_exact_match_required");
    expect(e.retryable).toBe(false);
    expect(e.details).toEqual({
      kind: "field",
      ref: "顧客コ",
      candidates: [{ code: "customer_code", label: "顧客コード" }],
    });
  });

  it("部分一致が複数件でも resolver_field_exact_match_required(コード順)", async () => {
    const { resolver } = setup();
    const e = await failure(resolver.resolveField(12, "顧客", { mode: "write" }));
    expect(e.code).toBe("resolver_field_exact_match_required");
    expect(e.details?.candidates).toEqual([
      { code: "customer", label: "顧客名" },
      { code: "customer_code", label: "顧客コード" },
    ]);
  });

  it("部分一致も 0 件なら resolver_field_not_found", async () => {
    const { resolver } = setup();
    const e = await failure(resolver.resolveField(12, "存在しない", { mode: "write" }));
    expect(e.code).toBe("resolver_field_not_found");
  });
});
