import { afterEach, describe, expect, it, vi } from "vitest";
import { ResponseTooLargeError, toErrorEnvelope } from "../../src/errors";
import {
  createKintoneClient,
  DEFAULT_MAX_API_CALLS,
  KintoneApiError,
  KintoneCallLimitError,
  type KintoneClientOptions,
  KintoneUnauthorizedError,
  MAX_RESPONSE_BYTES,
} from "../../src/kintone/client";
import { jsonResponse, mockOutboundFetch } from "../helpers/mock-fetch";

const ORIGIN = "https://example.cybozu.com";
const APPS = `GET ${ORIGIN}/k/v1/apps.json`;
const APP = `GET ${ORIGIN}/k/v1/app.json`;
const TOKEN = "kt-at-1";
// 文字列を分けて書く(エディタ・ツールがトークンらしい文字列を伏せるため)。
const BEARER = ["Bear", "er"].join("");

/** 待機を実際には待たず、秒数だけ記録する。 */
function setup(options: Partial<KintoneClientOptions> = {}) {
  const sleeps: number[] = [];
  const client = createKintoneClient({
    host: "example.cybozu.com",
    accessToken: TOKEN,
    sleep: async (ms) => {
      sleeps.push(ms);
    },
    ...options,
  });
  return { client, sleeps };
}

const MIB = 1024 * 1024;

/** pull されたチャンク数と cancel を記録するストリーム応答。 */
function streamBody(chunks: number, chunkBytes: number, init: ResponseInit = {}) {
  const state = { pulled: 0, cancelled: false };
  const chunk = new Uint8Array(chunkBytes).fill(0x61); // "a"
  const body = new ReadableStream<Uint8Array>(
    {
      pull(controller) {
        if (state.pulled >= chunks) {
          controller.close();
          return;
        }
        state.pulled++;
        controller.enqueue(chunk);
      },
      cancel() {
        state.cancelled = true;
      },
    },
    { highWaterMark: 0 },
  );
  return { state, res: new Response(body, init) };
}

const errorOf = (p: Promise<unknown>) =>
  p.then(
    () => undefined,
    (e: unknown) => e,
  );

afterEach(() => vi.restoreAllMocks());

describe("KintoneClient.get", () => {
  it("Authorization: Bearer でトークンを送り、JSON を返す。クエリは付けた分だけ", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [{ appId: "1" }] }) });
    const body = await setup().client.get("/k/v1/apps.json", { limit: 5, name: "顧客" });
    expect(body).toEqual({ apps: [{ appId: "1" }] });
    const call = m.calls[0];
    expect(call?.headers.get("authorization")).toBe(`Bearer ${TOKEN}`);
    expect(call?.headers.get("accept")).toBe("application/json");
    expect(call?.body).toBe("");
    const url = new URL(call?.url ?? "");
    expect(url.origin + url.pathname).toBe("https://example.cybozu.com/k/v1/apps.json");
    expect(url.searchParams.get("limit")).toBe("5");
    expect(url.searchParams.get("name")).toBe("顧客");
  });

  it("undefined のクエリは付けない", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [] }) });
    await setup().client.get("/k/v1/apps.json", { limit: undefined });
    expect(new URL(m.calls[0]?.url ?? "").search).toBe("");
  });

  it("配列のクエリは kintone の形式(key[0]=v)で送る", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [] }) });
    await setup().client.get("/k/v1/apps.json", { ids: [1, 22], codes: ["CRM"], limit: 3 });
    const params = new URL(m.calls[0]?.url ?? "").searchParams;
    expect(params.get("ids[0]")).toBe("1");
    expect(params.get("ids[1]")).toBe("22");
    expect(params.get("codes[0]")).toBe("CRM");
    expect(params.get("limit")).toBe("3");
    expect([...params.keys()].sort()).toEqual(["codes[0]", "ids[0]", "ids[1]", "limit"]);
  });

  it("401 は KintoneUnauthorizedError(クライアントにリフレッシュさせる合図)。kintone の code を details に持つ", async () => {
    mockOutboundFetch({
      [APPS]: () => jsonResponse({ code: "CB_AU01", id: "x", message: "unauthorized" }, 401),
    });
    const err = await errorOf(setup().client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(KintoneUnauthorizedError);
    expect(toErrorEnvelope(err).error.details).toEqual({
      http_status: 401,
      kintone_code: "CB_AU01",
      kintone_id: "x",
    });
  });

  it("エラー本文の {code,id,message} を details と message に載せる。JSON でないエラー本文は無視して status だけで分類する", async () => {
    mockOutboundFetch({
      [APPS]: () =>
        jsonResponse({ code: "GAIA_AP01", id: "id-9", message: "アプリが見つかりません。" }, 404),
      [APP]: () => new Response("<html>bad gateway</html>", { status: 502 }),
    });
    const { client } = setup();
    const notFound = toErrorEnvelope(await errorOf(client.get("/k/v1/apps.json"))).error;
    expect(notFound.code).toBe("not_found");
    expect(notFound.details).toEqual({
      http_status: 404,
      kintone_code: "GAIA_AP01",
      kintone_id: "id-9",
    });
    expect(notFound.message).toContain("アプリが見つかりません。");

    const bad = toErrorEnvelope(await errorOf(client.get("/k/v1/app.json"))).error;
    expect(bad.code).toBe("api_error");
    expect(bad.details).toEqual({ http_status: 502 });
    expect(bad.message).not.toContain("<html>");
  });

  it("通信失敗は status 0 の KintoneApiError(retryable。トークンを含めない)。GET でもリトライしない", async () => {
    const m = mockOutboundFetch({ [APPS]: () => Promise.reject(new Error(`boom ${TOKEN}`)) });
    const { client, sleeps } = setup();
    const err = await errorOf(client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(KintoneApiError);
    expect((err as KintoneApiError).status).toBe(0);
    expect(toErrorEnvelope(err).error.retryable).toBe(true);
    expect(String((err as Error).message)).not.toContain(TOKEN);
    expect(m.calls).toHaveLength(1);
    expect(sleeps).toEqual([]);
  });

  it("リダイレクトは辿らない(トークンを別の宛先へ送らない)。3xx は api_error(retryable でない)", async () => {
    const m = mockOutboundFetch({
      [APPS]: () =>
        new Response(null, { status: 302, headers: { Location: "https://evil.example/steal" } }),
    });
    const err = await errorOf(setup().client.get("/k/v1/apps.json"));
    expect(m.calls).toHaveLength(1);
    expect(m.calls[0]?.redirect).toBe("manual");
    const e = toErrorEnvelope(err).error;
    expect(e.code).toBe("api_error");
    expect(e.retryable).toBe(false);
    expect(e.details?.http_status).toBe(302);
  });
});

describe("書き込み(POST / PUT / DELETE)の送り方", () => {
  it.each([
    ["post", "POST"],
    ["put", "PUT"],
    ["delete", "DELETE"],
  ] as const)(
    "%s は JSON 本文と Content-Type を付け、Bearer で呼ぶ",
    async (method, httpMethod) => {
      const m = mockOutboundFetch({
        [`${httpMethod} ${ORIGIN}/k/v1/records.json`]: () => jsonResponse({ ids: ["1"] }),
      });
      const body = { app: 1, records: [{ a: { value: "x" } }] };
      const res = await setup().client[method]("/k/v1/records.json", body);
      expect(res).toEqual({ ids: ["1"] });
      const call = m.calls[0];
      expect(call?.method).toBe(httpMethod);
      expect(call?.headers.get("authorization")).toBe(`Bearer ${TOKEN}`);
      expect(call?.headers.get("content-type")).toContain("application/json");
      expect(JSON.parse(call?.body ?? "")).toEqual(body);
      expect(new URL(call?.url ?? "").search).toBe("");
    },
  );
});

describe("直列化", () => {
  it("並行に呼んでも、前のリクエストが終わるまで次の fetch を始めない", async () => {
    let release: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    let active = 0;
    let maxActive = 0;
    const started: string[] = [];
    mockOutboundFetch({
      [APPS]: async () => {
        started.push("apps");
        active++;
        maxActive = Math.max(maxActive, active);
        await gate;
        active--;
        return jsonResponse({ n: 1 });
      },
      [APP]: async () => {
        started.push("app");
        active++;
        maxActive = Math.max(maxActive, active);
        active--;
        return jsonResponse({ n: 2 });
      },
    });
    const { client } = setup();
    const first = client.get("/k/v1/apps.json");
    const second = client.get("/k/v1/app.json");
    await new Promise((r) => setTimeout(r, 20));
    expect(started).toEqual(["apps"]);
    release?.();
    expect(await first).toEqual({ n: 1 });
    expect(await second).toEqual({ n: 2 });
    expect(started).toEqual(["apps", "app"]);
    expect(maxActive).toBe(1);
  });

  it("リトライの待機中も次のリクエストを始めない。前が失敗しても後続は動く", async () => {
    const order: string[] = [];
    let appsCalls = 0;
    mockOutboundFetch({
      [APPS]: () => {
        order.push("apps");
        appsCalls++;
        return appsCalls === 1
          ? jsonResponse({}, 429)
          : jsonResponse({ code: "GAIA_X", message: "x" }, 400);
      },
      [APP]: () => {
        order.push("app");
        return jsonResponse({ ok: true });
      },
    });
    const { client } = setup({ sleep: () => new Promise((r) => setTimeout(r, 10)) });
    const first = errorOf(client.get("/k/v1/apps.json"));
    const second = client.get("/k/v1/app.json");
    expect(toErrorEnvelope(await first).error.code).toBe("argument_error");
    expect(await second).toEqual({ ok: true });
    expect(order).toEqual(["apps", "apps", "app"]);
  });
});

describe("読み取りのリトライ(429・503 を最大 2 回)", () => {
  it("429 のあと 200 なら成功する。Retry-After がなければ 1 秒待つ", async () => {
    let n = 0;
    const m = mockOutboundFetch({
      [APPS]: () => (++n === 1 ? jsonResponse({}, 429) : jsonResponse({ apps: [] })),
    });
    const { client, sleeps } = setup();
    expect(await client.get("/k/v1/apps.json")).toEqual({ apps: [] });
    expect(m.calls).toHaveLength(2);
    expect(sleeps).toEqual([1000]);
  });

  it("503 が 2 回続いても 3 回目で成功すれば成功する(待機は 1 秒ずつ)", async () => {
    let n = 0;
    const m = mockOutboundFetch({
      [APPS]: () => (++n <= 2 ? jsonResponse({}, 503) : jsonResponse({ apps: [] })),
    });
    const { client, sleeps } = setup();
    expect(await client.get("/k/v1/apps.json")).toEqual({ apps: [] });
    expect(m.calls).toHaveLength(3);
    expect(sleeps).toEqual([1000, 1000]);
  });

  it("Retry-After の秒数に従う", async () => {
    let n = 0;
    mockOutboundFetch({
      [APPS]: () =>
        ++n === 1
          ? new Response("{}", { status: 429, headers: { "Retry-After": "3" } })
          : jsonResponse({ apps: [] }),
    });
    const { client, sleeps } = setup();
    await client.get("/k/v1/apps.json");
    expect(sleeps).toEqual([3000]);
  });

  it("429 が続くと、合計 3 回(リトライ 2 回)で打ち切り、rate_limited(retryable)+ retry_after_sec", async () => {
    const m = mockOutboundFetch({
      [APPS]: () => new Response("{}", { status: 429, headers: { "Retry-After": "2" } }),
    });
    const { client, sleeps } = setup();
    const err = await errorOf(client.get("/k/v1/apps.json"));
    expect(m.calls).toHaveLength(3);
    expect(sleeps).toEqual([2000, 2000]);
    expect(toErrorEnvelope(err).error).toMatchObject({
      code: "rate_limited",
      retryable: true,
      details: { http_status: 429, retry_after_sec: 2 },
    });
  });

  it("503 が続くと 3 回で打ち切り、api_error(retryable)", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({}, 503) });
    const err = await errorOf(setup().client.get("/k/v1/apps.json"));
    expect(m.calls).toHaveLength(3);
    expect(toErrorEnvelope(err).error).toMatchObject({ code: "api_error", retryable: true });
  });

  it("Retry-After が 10 秒を超えるなら待たずに打ち切る(10 秒ちょうどは待つ)", async () => {
    const m1 = mockOutboundFetch({
      [APPS]: () => new Response("{}", { status: 429, headers: { "Retry-After": "11" } }),
    });
    const a = setup();
    const err = await errorOf(a.client.get("/k/v1/apps.json"));
    expect(m1.calls).toHaveLength(1);
    expect(a.sleeps).toEqual([]);
    expect(toErrorEnvelope(err).error.details?.retry_after_sec).toBe(11);
    vi.restoreAllMocks();

    let n = 0;
    mockOutboundFetch({
      [APPS]: () =>
        ++n === 1
          ? new Response("{}", { status: 503, headers: { "Retry-After": "10" } })
          : jsonResponse({ ok: 1 }),
    });
    const b = setup();
    expect(await b.client.get("/k/v1/apps.json")).toEqual({ ok: 1 });
    expect(b.sleeps).toEqual([10_000]);
  });

  it("Retry-After: 0 は待たずに(0 ms)リトライする", async () => {
    let n = 0;
    mockOutboundFetch({
      [APPS]: () =>
        ++n === 1
          ? new Response("{}", { status: 429, headers: { "Retry-After": "0" } })
          : jsonResponse({ ok: 1 }),
    });
    const { client, sleeps } = setup();
    await client.get("/k/v1/apps.json");
    expect(sleeps).toEqual([0]);
  });

  it.each([
    ["日付形式", "Wed, 21 Oct 2026 07:28:00 GMT"],
    ["負", "-1"],
    ["小数", "1.5"],
    ["空", ""],
    ["文字列", "soon"],
  ])("Retry-After が読めない(%s)なら 1 秒待つ", async (_name, value) => {
    let n = 0;
    mockOutboundFetch({
      [APPS]: () =>
        ++n === 1
          ? new Response("{}", { status: 429, headers: { "Retry-After": value } })
          : jsonResponse({ ok: 1 }),
    });
    const { client, sleeps } = setup();
    await client.get("/k/v1/apps.json");
    expect(sleeps).toEqual([1000]);
  });

  it.each([400, 401, 403, 404, 500, 502, 520])("%i はリトライしない", async (status) => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({}, status) });
    const { client, sleeps } = setup();
    await errorOf(client.get("/k/v1/apps.json"));
    expect(m.calls).toHaveLength(1);
    expect(sleeps).toEqual([]);
  });
});

describe("書き込みはリトライしない", () => {
  const WRITES = [
    ["post", "POST"],
    ["put", "PUT"],
    ["delete", "DELETE"],
  ] as const;

  it.each(
    WRITES.flatMap(([m, h]) => [
      [m, h, 429, "rate_limited"],
      [m, h, 503, "api_error"],
    ]),
  )("%s(%s)は %i でも 1 回で打ち切る(%s)", async (method, httpMethod, status, code) => {
    const m = mockOutboundFetch({
      [`${httpMethod} ${ORIGIN}/k/v1/records.json`]: () =>
        new Response("{}", { status, headers: { "Retry-After": "1" } }),
    });
    const { client, sleeps } = setup();
    const err = await errorOf(client[method]("/k/v1/records.json", { app: 1 }));
    expect(m.calls).toHaveLength(1);
    expect(sleeps).toEqual([]);
    const e = toErrorEnvelope(err).error;
    expect(e.code).toBe(code);
    expect(e.retryable).toBe(true);
  });

  it.each(WRITES)("%s は通信失敗・タイムアウトでもリトライしない", async (method, httpMethod) => {
    const m = mockOutboundFetch({
      [`${httpMethod} ${ORIGIN}/k/v1/records.json`]: () => Promise.reject(new Error("down")),
    });
    const { client, sleeps } = setup();
    await errorOf(client[method]("/k/v1/records.json", { app: 1 }));
    expect(m.calls).toHaveLength(1);
    expect(sleeps).toEqual([]);
  });

  it("書き込みの 200 でも JSON でなければ api_error(retryable)で、リトライしない", async () => {
    const m = mockOutboundFetch({
      [`POST ${ORIGIN}/k/v1/records.json`]: () => new Response("<html>", { status: 200 }),
    });
    const err = await errorOf(setup().client.post("/k/v1/records.json", { app: 1 }));
    expect(m.calls).toHaveLength(1);
    expect(toErrorEnvelope(err).error.code).toBe("api_error");
  });
});

describe("呼び出し回数の上限(maxApiCalls)", () => {
  it("既定は 50 回。51 回目は fetch せずに api_error(retryable でない)", async () => {
    expect(DEFAULT_MAX_API_CALLS).toBe(50);
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ ok: 1 }) });
    const { client } = setup();
    for (let i = 0; i < 50; i++) await client.get("/k/v1/apps.json");
    const err = await errorOf(client.get("/k/v1/apps.json"));
    expect(m.calls).toHaveLength(50);
    expect(err).toBeInstanceOf(KintoneCallLimitError);
    expect(toErrorEnvelope(err).error).toMatchObject({ code: "api_error", retryable: false });
  });

  it("maxApiCalls を指定できる。失敗した呼び出しも数える", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({}, 404) });
    const { client } = setup({ maxApiCalls: 2 });
    await errorOf(client.get("/k/v1/apps.json"));
    await errorOf(client.get("/k/v1/apps.json"));
    const err = await errorOf(client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(KintoneCallLimitError);
    expect(m.calls).toHaveLength(2);
  });

  it("リトライも 1 回として数える(上限に達したらリトライせずに api_error)", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({}, 503) });
    const { client } = setup({ maxApiCalls: 2 });
    const err = await errorOf(client.get("/k/v1/apps.json"));
    expect(m.calls).toHaveLength(2);
    expect(err).toBeInstanceOf(KintoneCallLimitError);
  });

  it("書き込みも同じカウンターを使う", async () => {
    const m = mockOutboundFetch({
      [APPS]: () => jsonResponse({ ok: 1 }),
      [`POST ${ORIGIN}/k/v1/records.json`]: () => jsonResponse({ ids: [] }),
    });
    const { client } = setup({ maxApiCalls: 2 });
    await client.get("/k/v1/apps.json");
    await client.post("/k/v1/records.json", {});
    const err = await errorOf(client.post("/k/v1/records.json", {}));
    expect(err).toBeInstanceOf(KintoneCallLimitError);
    expect(m.calls).toHaveLength(2);
  });
});

describe("応答本文の上限(10 MiB)", () => {
  it("MAX_RESPONSE_BYTES は 10 MiB", () => {
    expect(MAX_RESPONSE_BYTES).toBe(10 * MIB);
  });

  /** ちょうど `bytes` バイトの JSON(ASCII)。 */
  const jsonOfBytes = (bytes: number) => `{"a":"${"x".repeat(bytes - 8)}"}`;

  it("ちょうど 10 MiB の応答は読める", async () => {
    const text = jsonOfBytes(10 * MIB);
    expect(new TextEncoder().encode(text).byteLength).toBe(10 * MIB);
    mockOutboundFetch({ [APPS]: () => new Response(text) });
    const body = (await setup().client.get("/k/v1/apps.json")) as { a: string };
    expect(body.a.length).toBe(10 * MIB - 8);
  });

  it("10 MiB + 1 バイトは response_too_large。超えた時点で読むのをやめて(cancel)、全部は読まない", async () => {
    const { state, res } = streamBody(100, MIB);
    const m = mockOutboundFetch({ [APPS]: () => res });
    const { client, sleeps } = setup();
    const err = await errorOf(client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(ResponseTooLargeError);
    const e = toErrorEnvelope(err).error;
    expect(e.code).toBe("response_too_large");
    expect(e.retryable).toBe(false);
    expect(e.message).toContain("10 MiB");
    expect(state.cancelled).toBe(true);
    expect(state.pulled).toBeLessThanOrEqual(11);
    expect(m.calls).toHaveLength(1);
    expect(sleeps).toEqual([]);
  });

  it("ちょうど 10 MiB + 1 バイトでも超過(境界)", async () => {
    mockOutboundFetch({ [APPS]: () => new Response(jsonOfBytes(10 * MIB + 1)) });
    const err = await errorOf(setup().client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(ResponseTooLargeError);
  });

  it("Content-Length が上限超過を宣言していれば、本文を読まずに response_too_large", async () => {
    const { state, res } = streamBody(100, MIB, {
      headers: { "Content-Length": String(10 * MIB + 1) },
    });
    mockOutboundFetch({ [APPS]: () => res });
    const err = await errorOf(setup().client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(ResponseTooLargeError);
    expect(state.pulled).toBe(0);
    expect(state.cancelled).toBe(true);
  });

  it("巨大なエラー応答の本文は 64 KiB までしか読まず、status だけで分類する", async () => {
    const { state, res } = streamBody(100, MIB, { status: 500 });
    mockOutboundFetch({ [APPS]: () => res });
    const err = await errorOf(setup({ maxRetries: 0 }).client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(KintoneApiError);
    expect((err as KintoneApiError).status).toBe(500);
    expect(state.pulled).toBeLessThanOrEqual(2);
    expect(state.cancelled).toBe(true);
  });
});

describe("タイムアウト", () => {
  it("応答が返らなければ中断し、api_error(retryable)。message に時間が入る。リトライしない", async () => {
    const m = mockOutboundFetch({
      [APPS]: (call) =>
        new Promise<Response>((_, reject) => {
          call.signal.addEventListener("abort", () =>
            reject(new DOMException("aborted", "AbortError")),
          );
        }),
    });
    const { client, sleeps } = setup({ timeoutMs: 20 });
    const err = await errorOf(client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(KintoneApiError);
    expect((err as KintoneApiError).status).toBe(0);
    const e = toErrorEnvelope(err).error;
    expect(e.code).toBe("api_error");
    expect(e.retryable).toBe(true);
    expect(e.message).toContain("timed out after 20ms");
    expect(m.calls).toHaveLength(1);
    expect(sleeps).toEqual([]);
  });

  it("既定は 30 秒", async () => {
    const { DEFAULT_TIMEOUT_MS } = await import("../../src/kintone/client");
    expect(DEFAULT_TIMEOUT_MS).toBe(30_000);
  });

  it("ヘッダーは来たが本文が止まった場合も、同じ期限で中断する", async () => {
    let cancelled = false;
    mockOutboundFetch({
      [APPS]: () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(controller) {
              controller.enqueue(new TextEncoder().encode('{"apps":['));
            },
            cancel() {
              cancelled = true;
            },
          }),
        ),
    });
    const err = await errorOf(setup({ timeoutMs: 30 }).client.get("/k/v1/apps.json"));
    const e = toErrorEnvelope(err).error;
    expect(e.code).toBe("api_error");
    expect(e.retryable).toBe(true);
    expect(e.message).toContain("timed out");
    expect(cancelled).toBe(true);
  });
});

describe("JSON でない・空の 200 応答", () => {
  it.each([
    ["HTML", "<html>"],
    ["空", ""],
    ["途中で切れた JSON", '{"apps":['],
  ])(
    "%s は api_error(retryable)。http_status は実際の 200 で、502 を偽装しない",
    async (_n, text) => {
      mockOutboundFetch({ [APPS]: () => new Response(text, { status: 200 }) });
      const err = await errorOf(setup().client.get("/k/v1/apps.json"));
      expect(err).toBeInstanceOf(KintoneApiError);
      expect((err as KintoneApiError).status).toBe(200);
      const e = toErrorEnvelope(err).error;
      expect(e.code).toBe("api_error");
      expect(e.retryable).toBe(true);
      expect(e.details).toEqual({ http_status: 200 });
      expect(e.message).toContain("not valid JSON");
      expect(e.message).not.toContain("<html>");
    },
  );

  it("JSON でない 200 は(読み取りでも)クライアント内ではリトライしない", async () => {
    const m = mockOutboundFetch({ [APPS]: () => new Response("<html>", { status: 200 }) });
    await errorOf(setup().client.get("/k/v1/apps.json"));
    expect(m.calls).toHaveLength(1);
  });
});

describe("トークンを出さない", () => {
  it("kintone の message にトークン・Bearer が混ざっていても、エラーの message / details から伏せる", async () => {
    mockOutboundFetch({
      [APPS]: () =>
        jsonResponse(
          {
            code: "CB_VA01",
            id: `id-${TOKEN}`,
            message: `bad ${TOKEN} ${BEARER} ${TOKEN} ${BEARER} other.tok-en`,
          },
          400,
        ),
    });
    const err = await errorOf(setup().client.get("/k/v1/apps.json"));
    const text = JSON.stringify(toErrorEnvelope(err));
    expect(text).not.toContain(TOKEN);
    expect(text).not.toContain("other.tok-en");
    expect(text).toContain("[REDACTED]");
  });

  it("redact() はトークンと Bearer を伏せる", () => {
    const { client } = setup();
    expect(client.redact(`x ${TOKEN} y ${BEARER} abc.DEF-123 z`)).toBe(
      `x [REDACTED] y ${BEARER} [REDACTED] z`,
    );
    expect(client.redact("no secrets")).toBe("no secrets");
  });

  it("クライアントは console に何も出さない", async () => {
    const spies = (["log", "info", "warn", "error", "debug"] as const).map((k) =>
      vi.spyOn(console, k).mockImplementation(() => {}),
    );
    mockOutboundFetch({
      [APPS]: () => jsonResponse({ message: `echo ${TOKEN}` }, 500),
    });
    await errorOf(setup().client.get("/k/v1/apps.json"));
    for (const s of spies) expect(s).not.toHaveBeenCalled();
  });

  it("その他のエラーは status を持つ KintoneApiError(トークンを含めない)", async () => {
    mockOutboundFetch({
      [APPS]: () => jsonResponse({ message: `echo ${TOKEN} Bearer ${TOKEN}` }, 503),
    });
    const err = await errorOf(setup().client.get("/k/v1/apps.json"));
    expect(err).toBeInstanceOf(KintoneApiError);
    expect((err as KintoneApiError).status).toBe(503);
    expect((err as KintoneApiError).message).not.toContain(TOKEN);
  });
});
