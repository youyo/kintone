/**
 * record_create の冪等キャッシュ(仕様書 §10、ADR 0009 / 0011)。
 * キーは idem:<userId>:<grantId>:<tool>:<explicit|hash>:<値>(grant 単位)。生の email・トークン・明示キーそのものを入れない。
 * 成功した結果だけを TTL 300 秒で保存し、同じキーの再送は kintone を一度も呼ばずに前回の結果を返す。
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { userIdFromEmail } from "../../src/auth/user-id";
import { KintoneApiError } from "../../src/kintone/client";
import {
  canonicalJson,
  explicitKeyFrom,
  IDEMPOTENCY_TTL_SECONDS,
  idempotencyCacheKey,
  MAX_EXPLICIT_KEY_BYTES,
} from "../../src/mcp/idempotency";
import { type AppRow, listApps } from "../helpers/fake-kintone-data";
import { type FakeKv, fakeKv } from "../helpers/fake-kv";
import { toolHarness } from "../helpers/tool-harness";

afterEach(() => vi.restoreAllMocks());

const GRANT = { userId: "user-a", grantId: "grant-1" };
const ROWS: AppRow[] = [
  { appId: "12", code: "CRM", name: "顧客管理" },
  { appId: "15", code: "SALES3", name: "売上報告" },
];
const CREATE = "kintone_record_create";
const REC = { customer: { value: "山田" }, amount: { value: "100" } };

interface SetupOptions {
  kv?: FakeKv | null;
  grant?: { userId: string; grantId: string } | null;
  write?: (n: number) => unknown;
}

function setup(options: SetupOptions = {}) {
  const f = options.kv === undefined ? fakeKv() : options.kv;
  let n = 0;
  const h = toolHarness({
    ...(f ? { kv: f.kv } : {}),
    ...(options.grant === null ? {} : { grant: options.grant ?? GRANT }),
    get: async (path, query) => {
      if (path === "/k/v1/apps.json") return listApps(ROWS, query);
      throw new Error(`unexpected GET ${path}`);
    },
    write: (method) => {
      n++;
      if (options.write) return options.write(n);
      return method === "POST"
        ? { ids: [String(100 + n)], revisions: ["1"] }
        : method === "PUT"
          ? { revision: "2" }
          : {};
    },
  });
  return { h, f };
}

const idemKeys = (f: FakeKv) => [...f.store.keys()].filter((k) => k.startsWith("idem:"));

describe("冪等キャッシュの関数", () => {
  it("TTL は 300 秒、明示キーは UTF-8 で 200 バイトまで", () => {
    expect(IDEMPOTENCY_TTL_SECONDS).toBe(300);
    expect(MAX_EXPLICIT_KEY_BYTES).toBe(200);
  });

  it("canonicalJson はキーの順序に依存しない", () => {
    expect(canonicalJson({ b: 1, a: { d: [{ y: 1, x: 2 }], c: 3 } })).toBe(
      canonicalJson({ a: { c: 3, d: [{ x: 2, y: 1 }] }, b: 1 }),
    );
    expect(canonicalJson({ a: [1, 2] })).not.toBe(canonicalJson({ a: [2, 1] }));
  });

  it("明示キー: _meta.idempotency_key → 引数 idempotency_key の順。空文字・文字列でない値は無いものとして扱う", () => {
    expect(explicitKeyFrom({ idempotency_key: "m" }, { idempotency_key: "a" })).toBe("m");
    expect(explicitKeyFrom(undefined, { idempotency_key: "a" })).toBe("a");
    expect(explicitKeyFrom({ idempotency_key: "" }, { idempotency_key: "a" })).toBe("a");
    expect(explicitKeyFrom({ idempotency_key: 5 }, { idempotency_key: 7 })).toBeUndefined();
    expect(explicitKeyFrom({}, {})).toBeUndefined();
    expect(explicitKeyFrom(undefined, undefined)).toBeUndefined();
  });

  it("明示キーが 200 バイトを超えたら argument_error(文字数でなくバイト数)", () => {
    expect(() => explicitKeyFrom({ idempotency_key: "a".repeat(200) }, {})).not.toThrow();
    expect(() => explicitKeyFrom({ idempotency_key: "a".repeat(201) }, {})).toThrow(/200/);
    // 日本語は 1 文字 3 バイト: 67 文字 = 201 バイト
    expect(() => explicitKeyFrom({ idempotency_key: "あ".repeat(67) }, {})).toThrow(/200/);
    expect(() => explicitKeyFrom({ idempotency_key: "あ".repeat(66) }, {})).not.toThrow();
  });

  it("キーは idem:<userId>:<grantId>:<tool>:hash:<SHA-256 16 進 64 文字>(引数のキー順に依存しない)", async () => {
    const a = await idempotencyCacheKey(GRANT, CREATE, undefined, { x: 1, y: 2 });
    const b = await idempotencyCacheKey(GRANT, CREATE, undefined, { y: 2, x: 1 });
    expect(a.key).toBe(b.key);
    expect(a.key).toMatch(/^idem:user-a:grant-1:kintone_record_create:hash:[0-9a-f]{64}$/);
  });

  it("明示キーは explicit:<SHA-256 16 進>(生の値を入れない)。引数ハッシュは別に返す", async () => {
    const a = await idempotencyCacheKey(GRANT, CREATE, "my-raw-key", { x: 1 });
    const b = await idempotencyCacheKey(GRANT, CREATE, "my-raw-key", { x: 2 });
    expect(a.key).toMatch(/^idem:user-a:grant-1:kintone_record_create:explicit:[0-9a-f]{64}$/);
    expect(a.key).not.toContain("my-raw-key");
    expect(a.key).toBe(b.key);
    expect(a.argsHash).not.toBe(b.argsHash);
  });

  it("grant が違えばキーが違う", async () => {
    const a = await idempotencyCacheKey(GRANT, CREATE, undefined, { x: 1 });
    const b = await idempotencyCacheKey(
      { userId: "user-a", grantId: "grant-2" },
      CREATE,
      undefined,
      { x: 1 },
    );
    expect(a.key).not.toBe(b.key);
  });
});

describe("kintone_record_create の冪等キャッシュ", () => {
  it("初回は POST して結果を保存し、同じ引数の再送は kintone を一度も呼ばずに前回の結果を返す", async () => {
    const { h, f } = setup();
    const first = await h.call(CREATE, { app: 12, record: REC });
    expect(first.body).toEqual({ ids: [101], revisions: [1] });
    expect(h.writes).toHaveLength(1);

    const second = await h.call(CREATE, { app: 12, record: REC });
    expect(second.isError).toBe(false);
    expect(second.text).toBe(first.text);
    expect(h.writes).toHaveLength(1);
    expect(h.calls).toEqual([]);
    expect(f?.puts.filter((p) => p.key.startsWith("idem:"))).toHaveLength(1);
  });

  it("保存のキーは idem:<userId>:<grantId>:kintone_record_create:hash:...、TTL 300 秒、値は結果の JSON", async () => {
    const { h, f } = setup();
    const first = await h.call(CREATE, { app: 12, record: REC });
    const put = f?.puts.find((p) => p.key.startsWith("idem:"));
    expect(put?.key).toMatch(/^idem:user-a:grant-1:kintone_record_create:hash:[0-9a-f]{64}$/);
    expect(put?.options).toEqual({ expirationTtl: 300 });
    expect(JSON.parse(put?.value ?? "null").result).toBe(first.text);
  });

  it("引数のキー順が違っても同じ呼び出しとみなす", async () => {
    const { h } = setup();
    await h.call(CREATE, { app: 12, record: { a: { value: "1" }, b: { value: "2" } } });
    await h.call(CREATE, { record: { b: { value: "2" }, a: { value: "1" } }, app: 12 });
    expect(h.writes).toHaveLength(1);
  });

  it("引数が違えば別の呼び出し(2 回 POST する)", async () => {
    const { h } = setup();
    await h.call(CREATE, { app: 12, record: REC });
    await h.call(CREATE, { app: 12, record: { ...REC, amount: { value: "200" } } });
    expect(h.writes).toHaveLength(2);
  });

  it("app_ref の再送は名前解決の GET も呼ばない(kintone を一度も呼ばない)", async () => {
    const { h } = setup();
    await h.call(CREATE, { app_ref: "CRM", record: REC });
    const getsAfterFirst = h.calls.length;
    expect(getsAfterFirst).toBeGreaterThan(0);
    await h.call(CREATE, { app_ref: "CRM", record: REC });
    expect(h.calls).toHaveLength(getsAfterFirst);
    expect(h.writes).toHaveLength(1);
  });

  it("キーに生の email・MCP トークン・クライアントが渡した明示キーを入れない(userId は email のハッシュ)", async () => {
    const email = "alice@example.com";
    const userId = await userIdFromEmail(email);
    const { h, f } = setup({ grant: { userId, grantId: "grant-9" } });
    await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "raw-explicit-key-xyz" });
    await h.call(CREATE, { app: 12, record: REC });
    const keys = idemKeys(f as FakeKv);
    expect(keys).toHaveLength(2);
    for (const k of keys) {
      expect(k.startsWith(`idem:${userId}:grant-9:`)).toBe(true);
      expect(k).not.toContain(email);
      expect(k).not.toContain("alice");
      expect(k).not.toContain("raw-explicit-key-xyz");
    }
    // 値にも生の明示キー・email は入らない。
    for (const v of (f as FakeKv).store.values()) {
      expect(v).not.toContain("raw-explicit-key-xyz");
      expect(v).not.toContain(email);
    }
  });

  it("別の grant とは共有しない(同じ引数でも POST する)", async () => {
    const shared = fakeKv();
    const a = setup({ kv: shared, grant: GRANT });
    const b = setup({ kv: shared, grant: { userId: "user-a", grantId: "grant-2" } });
    const c = setup({ kv: shared, grant: { userId: "user-b", grantId: "grant-1" } });
    await a.h.call(CREATE, { app: 12, record: REC });
    await b.h.call(CREATE, { app: 12, record: REC });
    await c.h.call(CREATE, { app: 12, record: REC });
    expect(b.h.writes).toHaveLength(1);
    expect(c.h.writes).toHaveLength(1);
    expect(idemKeys(shared)).toHaveLength(3);
    // 同じ grant なら共有される。
    const a2 = setup({ kv: shared, grant: GRANT });
    await a2.h.call(CREATE, { app: 12, record: REC });
    expect(a2.h.writes).toHaveLength(0);
  });
});

describe("明示キー(_meta.idempotency_key / 引数 idempotency_key)", () => {
  it("同じ明示キー・同じ引数の再送は前回の結果を返す(_meta)", async () => {
    const { h } = setup();
    const first = await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "k-1" });
    const second = await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "k-1" });
    expect(second.text).toBe(first.text);
    expect(h.writes).toHaveLength(1);
  });

  it("引数 idempotency_key も使える。kintone には送らず、未知の引数エラーにもならない", async () => {
    const { h, f } = setup();
    const first = await h.call(CREATE, { app: 12, record: REC, idempotency_key: "k-arg" });
    expect(first.isError).toBe(false);
    expect(JSON.stringify(h.writes[0]?.body)).not.toContain("idempotency_key");
    expect(JSON.stringify(h.writes[0]?.body)).not.toContain("k-arg");
    const second = await h.call(CREATE, { app: 12, record: REC, idempotency_key: "k-arg" });
    expect(second.text).toBe(first.text);
    expect(h.writes).toHaveLength(1);
    expect(idemKeys(f as FakeKv)[0]).toContain(":explicit:");
  });

  it("_meta が引数より優先される", async () => {
    const { h } = setup();
    await h.call(
      CREATE,
      { app: 12, record: REC, idempotency_key: "arg" },
      { idempotency_key: "meta" },
    );
    await h.call(
      CREATE,
      { app: 12, record: REC, idempotency_key: "other" },
      { idempotency_key: "meta" },
    );
    expect(h.writes).toHaveLength(1);
  });

  it("別の明示キーなら、同じ引数でも別の呼び出し(意図して同じレコードを 2 件作れる)", async () => {
    const { h } = setup();
    await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "k-1" });
    await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "k-2" });
    expect(h.writes).toHaveLength(2);
  });

  it("同じ明示キーを別の引数で使うと、前回の結果を返さず argument_error(kintone を呼ばない)", async () => {
    const { h } = setup();
    await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "k-1" });
    const r = await h.call(
      CREATE,
      { app: 12, record: { customer: { value: "別人" } } },
      { idempotency_key: "k-1" },
    );
    expect(r.isError).toBe(true);
    expect(r.body.error.code).toBe("argument_error");
    expect(r.body.error.message).toMatch(/idempotency_key/);
    expect(h.writes).toHaveLength(1);
  });

  it("明示キーが 200 バイトを超えたら argument_error。kintone を呼ばない", async () => {
    const { h } = setup();
    const r = await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "a".repeat(201) });
    expect(r.body.error.code).toBe("argument_error");
    expect(h.writes).toEqual([]);
    expect(h.calls).toEqual([]);
  });

  it("空文字の明示キーは無いものとして扱う(引数ハッシュで保護される)", async () => {
    const { h, f } = setup();
    await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "" });
    await h.call(CREATE, { app: 12, record: REC }, { idempotency_key: "" });
    expect(h.writes).toHaveLength(1);
    expect(idemKeys(f as FakeKv)[0]).toContain(":hash:");
  });
});

describe("保存するもの・しないもの", () => {
  it("エラーは保存しない: 失敗のあとの再送は、もう一度 POST する", async () => {
    const { h, f } = setup({
      write: (n) => {
        if (n === 1) throw new KintoneApiError(500, {});
        return { ids: ["200"], revisions: ["1"] };
      },
    });
    const first = await h.call(CREATE, { app: 12, record: REC });
    expect(first.body.error.code).toBe("api_error");
    expect(idemKeys(f as FakeKv)).toEqual([]);
    const second = await h.call(CREATE, { app: 12, record: REC });
    expect(second.body).toEqual({ ids: [200], revisions: [1] });
    expect(h.writes).toHaveLength(2);
  });

  it("名前解決のエラー(部分一致のみ)も保存しない", async () => {
    const { h, f } = setup();
    const r = await h.call(CREATE, { app_ref: "顧客", record: REC });
    expect(r.body.error.code).toBe("resolver_app_exact_match_required");
    expect(idemKeys(f as FakeKv)).toEqual([]);
  });

  it("入力の検証エラーは保存しない", async () => {
    const { h, f } = setup();
    await h.call(CREATE, { app: 12 });
    expect(idemKeys(f as FakeKv)).toEqual([]);
  });

  it("record_update / record_delete は冪等キャッシュの対象外(同じ呼び出しを 2 回すれば 2 回書き込む)", async () => {
    const { h, f } = setup();
    await h.call("kintone_record_update", { app: 12, id: 1, record: REC });
    await h.call("kintone_record_update", { app: 12, id: 1, record: REC });
    await h.call("kintone_record_delete", { app: 12, ids: [1] });
    await h.call("kintone_record_delete", { app: 12, ids: [1] });
    expect(h.writes.map((w) => w.method)).toEqual(["PUT", "PUT", "DELETE", "DELETE"]);
    expect(idemKeys(f as FakeKv)).toEqual([]);
    expect(f?.gets.some((k) => k.startsWith("idem:"))).toBe(false);
  });
});

describe("KV の失敗・KV なし・grant なし", () => {
  it("KV の get が失敗しても実行する。ログには例外名だけ(キー・値・message は出さない)", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const f = fakeKv();
    f.fail.get = true;
    const { h } = setup({ kv: f });
    const r = await h.call(CREATE, { app: 12, record: REC });
    expect(r.isError).toBe(false);
    expect(h.writes).toHaveLength(1);
    const logs = [...warn.mock.calls, ...error.mock.calls].map((c) => c.join(" ")).join("\n");
    expect(logs).not.toContain("secret-kv-detail");
    expect(logs).not.toContain("idem:");
    expect(logs).not.toContain("user-a");
  });

  it("KV の put が失敗しても結果は返す(次の再送は保護されない)", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(console, "error").mockImplementation(() => {});
    const f = fakeKv();
    f.fail.put = true;
    const { h } = setup({ kv: f });
    const first = await h.call(CREATE, { app: 12, record: REC });
    expect(first.body).toEqual({ ids: [101], revisions: [1] });
    await h.call(CREATE, { app: 12, record: REC });
    expect(h.writes).toHaveLength(2);
  });

  it("保存された値が壊れていても(JSON でない・形が違う)実行する", async () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    vi.spyOn(console, "error").mockImplementation(() => {});
    const { h, f } = setup();
    await h.call(CREATE, { app: 12, record: REC });
    const key = idemKeys(f as FakeKv)[0] as string;
    for (const bad of ["not json", '{"result":5}', "[]", "null"]) {
      (f as FakeKv).store.set(key, bad);
      const before = h.writes.length;
      const r = await h.call(CREATE, { app: 12, record: REC });
      expect(r.isError, bad).toBe(false);
      expect(h.writes.length, bad).toBe(before + 1);
    }
  });

  it("KV がない・grant が分からないときは冪等キャッシュを使わず、通常どおり書き込む", async () => {
    const noKv = setup({ kv: null });
    await noKv.h.call(CREATE, { app: 12, record: REC });
    await noKv.h.call(CREATE, { app: 12, record: REC });
    expect(noKv.h.writes).toHaveLength(2);

    const noGrant = setup({ grant: null });
    await noGrant.h.call(CREATE, { app: 12, record: REC });
    await noGrant.h.call(CREATE, { app: 12, record: REC });
    expect(noGrant.h.writes).toHaveLength(2);
    expect(noGrant.f?.puts.filter((p) => p.key.startsWith("idem:"))).toEqual([]);
  });
});
