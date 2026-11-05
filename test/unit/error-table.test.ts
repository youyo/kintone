/**
 * 仕様書 §6 のエラー表の全行に対応するテスト。
 * HTTP の行は実際の kintone クライアントにモックの fetch を渡し、`toErrorEnvelope` まで通す。
 * 最後に、`ERROR_CODES` の全コードがどれかの行に現れることを検査する(コードを足してテストを書き忘れると赤くなる)。
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ERROR_CODES,
  type ErrorCode,
  type ErrorEnvelope,
  ResolverError,
  toErrorEnvelope,
} from "../../src/errors";
import { createKintoneClient } from "../../src/kintone/client";
import { jsonResponse, mockOutboundFetch } from "../helpers/mock-fetch";

const ORIGIN = "https://example.cybozu.com";
const APPS = `GET ${ORIGIN}/k/v1/apps.json`;
const RECORDS_POST = `POST ${ORIGIN}/k/v1/records.json`;
const TOKEN = "kt-at-1";

type Handler = () => Response | Promise<Response>;

function client(options: { timeoutMs?: number; maxApiCalls?: number } = {}) {
  return createKintoneClient({
    host: "example.cybozu.com",
    accessToken: TOKEN,
    sleep: async () => {},
    ...options,
  });
}

async function envelopeOf(run: () => Promise<unknown>): Promise<ErrorEnvelope> {
  try {
    await run();
  } catch (e) {
    return toErrorEnvelope(e);
  }
  throw new Error("エラーにならなかった");
}

/** GET /k/v1/apps.json を handler で応答させたときのエンベロープ。 */
function getWith(handler: Handler, options?: Parameters<typeof client>[0]) {
  return async () => {
    mockOutboundFetch({ [APPS]: handler });
    return envelopeOf(() => client(options).get("/k/v1/apps.json"));
  };
}

function postWith(handler: Handler) {
  return async () => {
    mockOutboundFetch({ [RECORDS_POST]: handler });
    return envelopeOf(() => client().post("/k/v1/records.json", { app: 1 }));
  };
}

interface Row {
  /** 仕様書 §6 の「HTTP / 事象」。 */
  row: string;
  /** どの状況か(同じ行に複数のケースがあるとき)。 */
  when: string;
  run: () => Promise<ErrorEnvelope>;
  code: ErrorCode;
  retryable: boolean;
  details?: Record<string, unknown>;
  /** details にこのキーがないこと。 */
  noDetailsKeys?: string[];
}

const kintoneError = (status: number, code: string, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify({ code, id: "id-1", message: `message for ${code}` }), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });

const candidatesApp = [
  { id: "12", code: "CRM", name: "顧客管理" },
  { id: "13", name: "顧客リスト" },
];
const candidatesField = [
  { code: "customer_name", label: "顧客名" },
  { code: "customer_no", label: "顧客番号" },
];

const ROWS: Row[] = [
  {
    row: "401",
    when: "kintone が 401",
    run: getWith(() => kintoneError(401, "CB_AU01")),
    code: "authentication_error",
    retryable: false,
    details: { http_status: 401, kintone_code: "CB_AU01", kintone_id: "id-1" },
  },
  {
    row: "403",
    when: "kintone が 403",
    run: getWith(() => kintoneError(403, "CB_NO02")),
    code: "permission_error",
    retryable: false,
    details: { http_status: 403, kintone_code: "CB_NO02" },
  },
  {
    row: "404",
    when: "kintone が 404",
    run: getWith(() => kintoneError(404, "GAIA_AP01")),
    code: "not_found",
    retryable: false,
    details: { http_status: 404, kintone_code: "GAIA_AP01", kintone_id: "id-1" },
  },
  {
    row: "400, 422, kintone コード接頭辞 CB_VA",
    when: "400",
    run: getWith(() => kintoneError(400, "GAIA_IQ11")),
    code: "argument_error",
    retryable: false,
    details: { http_status: 400, kintone_code: "GAIA_IQ11" },
  },
  {
    row: "400, 422, kintone コード接頭辞 CB_VA",
    when: "422",
    run: getWith(() => kintoneError(422, "GAIA_X")),
    code: "argument_error",
    retryable: false,
    details: { http_status: 422 },
  },
  {
    row: "400, 422, kintone コード接頭辞 CB_VA",
    when: "CB_VA01 が 520 で返る(ステータスによらず)",
    run: getWith(() => kintoneError(520, "CB_VA01")),
    code: "argument_error",
    retryable: false,
    details: { http_status: 520, kintone_code: "CB_VA01" },
  },
  {
    row: "400, 422, kintone コード接頭辞 CB_VA",
    when: "CB_VA01 が 403 のとき(401・403・404・429 はステータスが先)",
    run: getWith(() => kintoneError(403, "CB_VA01")),
    code: "permission_error",
    retryable: false,
  },
  {
    row: "429(リトライ後)",
    when: "読み取りで 429 が続き、2 回リトライしたあとも 429",
    run: getWith(() => kintoneError(429, "GAIA_TM12", { "Retry-After": "2" })),
    code: "rate_limited",
    retryable: true,
    details: { http_status: 429, retry_after_sec: 2 },
  },
  {
    row: "429(リトライ後)",
    when: "書き込みの 429(リトライしないので 1 回目で)",
    run: postWith(() => kintoneError(429, "GAIA_TM12")),
    code: "rate_limited",
    retryable: true,
    details: { http_status: 429 },
    noDetailsKeys: ["retry_after_sec"],
  },
  {
    row: "その他の 4xx",
    when: "405",
    run: getWith(() => kintoneError(405, "CB_IL02")),
    code: "api_error",
    retryable: false,
    details: { http_status: 405 },
  },
  {
    row: "その他の 4xx",
    when: "409",
    run: getWith(() => kintoneError(409, "GAIA_CO02")),
    code: "api_error",
    retryable: false,
    details: { http_status: 409 },
  },
  {
    row: "その他の 4xx",
    when: "413",
    run: getWith(() => new Response("{}", { status: 413 })),
    code: "api_error",
    retryable: false,
    details: { http_status: 413 },
  },
  {
    row: "5xx",
    when: "500",
    run: getWith(() => kintoneError(500, "GAIA_IN01")),
    code: "api_error",
    retryable: true,
    details: { http_status: 500, kintone_code: "GAIA_IN01" },
  },
  {
    row: "5xx",
    when: "520",
    run: getWith(() => kintoneError(520, "GAIA_UN01")),
    code: "api_error",
    retryable: true,
    details: { http_status: 520 },
  },
  {
    row: "5xx",
    when: "読み取りの 503 が続き、2 回リトライしたあとも 503",
    run: getWith(() => kintoneError(503, "GAIA_SV01")),
    code: "api_error",
    retryable: true,
    details: { http_status: 503 },
  },
  {
    row: "5xx",
    when: "書き込みの 503(リトライしない)",
    run: postWith(() => kintoneError(503, "GAIA_SV01")),
    code: "api_error",
    retryable: true,
    details: { http_status: 503 },
  },
  {
    row: "通信失敗・タイムアウト",
    when: "fetch が例外",
    run: getWith(() => Promise.reject(new Error(`down ${TOKEN}`))),
    code: "api_error",
    retryable: true,
    noDetailsKeys: ["http_status"],
  },
  {
    row: "通信失敗・タイムアウト",
    when: "応答が来ずタイムアウト",
    run: getWith(() => new Promise<Response>(() => {}).then(() => jsonResponse({})), {
      timeoutMs: 20,
    }),
    code: "api_error",
    retryable: true,
    noDetailsKeys: ["http_status"],
  },
  {
    row: "通信失敗・タイムアウト",
    when: "200 なのに JSON でない(通信失敗と同じ扱い。http_status は実際の 200)",
    run: getWith(() => new Response("<html>", { status: 200 })),
    code: "api_error",
    retryable: true,
    details: { http_status: 200 },
  },
  {
    row: "呼び出し回数の上限超過",
    when: "maxApiCalls に達したあとの呼び出し",
    run: async () => {
      mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [] }) });
      const c = client({ maxApiCalls: 1 });
      await c.get("/k/v1/apps.json");
      return envelopeOf(() => c.get("/k/v1/apps.json"));
    },
    code: "api_error",
    retryable: false,
  },
  {
    row: "応答が上限を超えた",
    when: "kintone の応答本文が 10 MiB 超",
    run: getWith(() => new Response(new Uint8Array(10 * 1024 * 1024 + 1).fill(0x20))),
    code: "response_too_large",
    retryable: false,
  },
  {
    row: "アプリ参照が見つからない",
    when: "resolver_app_not_found",
    run: async () =>
      toErrorEnvelope(
        new ResolverError("resolver_app_not_found", "App not found.", {
          kind: "app",
          ref: "顧客",
          candidates: [],
        }),
      ),
    code: "resolver_app_not_found",
    retryable: false,
    details: { kind: "app", ref: "顧客", candidates: [] },
  },
  {
    row: "アプリ参照が複数に一致",
    when: "resolver_app_ambiguous(details.candidates)",
    run: async () =>
      toErrorEnvelope(
        new ResolverError("resolver_app_ambiguous", "Multiple apps.", {
          kind: "app",
          ref: "顧客",
          candidates: candidatesApp,
        }),
      ),
    code: "resolver_app_ambiguous",
    retryable: false,
    details: { kind: "app", ref: "顧客", candidates: candidatesApp },
  },
  {
    row: "update_key_field_ref がラベルの部分一致のみ",
    when: "resolver_field_exact_match_required(details.candidates に部分一致の候補)",
    run: async () =>
      toErrorEnvelope(
        new ResolverError("resolver_field_exact_match_required", "Exact match required.", {
          kind: "field",
          ref: "顧客",
          candidates: candidatesField,
        }),
      ),
    code: "resolver_field_exact_match_required",
    retryable: false,
    details: { kind: "field", ref: "顧客", candidates: candidatesField },
  },
  {
    row: "書き込みツールでアプリ参照が部分一致のみ",
    when: "resolver_app_exact_match_required(details.candidates に部分一致の候補)",
    run: async () =>
      toErrorEnvelope(
        new ResolverError("resolver_app_exact_match_required", "Exact match required.", {
          kind: "app",
          ref: "顧客",
          candidates: candidatesApp,
        }),
      ),
    code: "resolver_app_exact_match_required",
    retryable: false,
    details: { kind: "app", ref: "顧客", candidates: candidatesApp },
  },
  {
    row: "フィールド参照が見つからない",
    when: "resolver_field_not_found",
    run: async () =>
      toErrorEnvelope(
        new ResolverError("resolver_field_not_found", "Field not found.", {
          kind: "field",
          ref: "x",
          candidates: [],
        }),
      ),
    code: "resolver_field_not_found",
    retryable: false,
    details: { kind: "field", ref: "x", candidates: [] },
  },
  {
    row: "フィールド参照が複数に一致",
    when: "resolver_field_ambiguous(details.candidates)",
    run: async () =>
      toErrorEnvelope(
        new ResolverError("resolver_field_ambiguous", "Multiple fields.", {
          kind: "field",
          ref: "顧客",
          candidates: candidatesField,
        }),
      ),
    code: "resolver_field_ambiguous",
    retryable: false,
    details: { kind: "field", ref: "顧客", candidates: candidatesField },
  },
  {
    row: "名前検索の結果が上限(1,000 件)に達した",
    when: "resolver_app_list_too_large",
    run: async () =>
      toErrorEnvelope(
        new ResolverError("resolver_app_list_too_large", "Too many apps.", {
          kind: "app",
          ref: "顧客",
        }),
      ),
    code: "resolver_app_list_too_large",
    retryable: false,
    details: { kind: "app", ref: "顧客" },
  },
  {
    row: "想定外の例外",
    when: "generic_error(メッセージは固定文)",
    run: async () => toErrorEnvelope(new Error(`internal ${TOKEN}`)),
    code: "generic_error",
    retryable: false,
  },
];

afterEach(() => vi.restoreAllMocks());

describe("仕様書 §6 のエラー表", () => {
  it.each(ROWS.map((r) => [r.row, r.when, r] as const))("[%s] %s", async (_row, _when, r) => {
    const env = await r.run();
    expect(env.schema_version).toBe("1");
    expect(env.error.code).toBe(r.code);
    expect(env.error.retryable).toBe(r.retryable);
    if (r.details) expect(env.error.details).toEqual(r.details);
    for (const key of r.noDetailsKeys ?? []) {
      expect(env.error.details ?? {}).not.toHaveProperty(key);
    }
    // どの行でも、トークンはメッセージにも details にも出ない。
    expect(JSON.stringify(env)).not.toContain(TOKEN);
  });

  it("generic_error のメッセージは固定文", async () => {
    const env = await ROWS.find((r) => r.code === "generic_error")?.run();
    expect(env?.error.message).toBe("Unexpected error.");
  });

  it("ERROR_CODES の全コードがどれかの行のテストに現れる", () => {
    const covered = new Set(ROWS.map((r) => r.code));
    expect([...covered].sort()).toEqual([...ERROR_CODES].sort());
  });
});
