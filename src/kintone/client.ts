/**
 * kintone REST クライアント(ADR 0008、仕様書 §8)。
 *
 * - 認証は利用者の OAuth アクセストークンの `Authorization: Bearer`。GET はクエリ、POST / PUT / DELETE は JSON 本文。
 * - リクエストはクライアントごとに直列(同時に 1 本)。1 クライアント = 1 回のツール呼び出し。
 * - **読み取り(GET)の 429・503 は最大 2 回リトライ**する。`Retry-After`(整数秒)があれば従い、10 秒を超えるなら待たずに打ち切る。
 *   **書き込み(POST / PUT / DELETE)はリトライしない**(多重作成・多重更新を避ける)。
 * - 1 クライアントあたりの HTTP リクエスト数に上限がある(リトライ含む)。応答本文は 10 MiB まで。タイムアウトは 1 リクエストごと。
 * - トークンをエラー・ログに出さない。リダイレクトは辿らない(トークンを別の宛先へ送らない)。
 */
import { type KintoneMcpError, ResponseTooLargeError } from "../errors";
import {
  KintoneApiError,
  KintoneCallLimitError,
  type KintoneErrorDetail,
  KintoneUnauthorizedError,
} from "./errors";

export { KintoneApiError, KintoneCallLimitError, KintoneUnauthorizedError };

/** 1 リクエストのタイムアウト(仕様書 §8)。 */
export const DEFAULT_TIMEOUT_MS = 30_000;
/** 読み取りの 429・503 のリトライ回数の上限。 */
export const DEFAULT_MAX_RETRIES = 2;
/** これを超える Retry-After は待たずにエラーにする(MCP クライアントを待たせすぎない)。 */
export const DEFAULT_MAX_RETRY_WAIT_SECONDS = 10;
/** Retry-After がない・読めないときの待ち時間。 */
const FALLBACK_RETRY_WAIT_SECONDS = 1;
/** 1 回のツール呼び出しで許す HTTP リクエスト数(リトライ含む)。Workers 無料プランの subrequest 上限と同じ。 */
export const DEFAULT_MAX_API_CALLS = 50;
/** 応答本文の読み取り上限(10 MiB)。超えたら `response_too_large`。 */
export const MAX_RESPONSE_BYTES = 10 * 1024 * 1024;
/** エラー応答の本文は 64 KiB までしか読まない(超えたら本文なし扱い)。 */
const MAX_ERROR_BODY_BYTES = 64 * 1024;

export type QueryScalar = string | number | boolean;
export type QueryValue = QueryScalar | readonly QueryScalar[] | undefined;
/** 配列は kintone の形式(`key[0]=a&key[1]=b`)で送る。undefined は送らない。 */
export type Query = Record<string, QueryValue>;

type HttpMethod = "GET" | "POST" | "PUT" | "DELETE";

export interface KintoneClient {
  /** `GET https://<host><path>?<query>` を呼び、JSON を返す。path は `/k/v1/...`。読み取りなので 429・503 はリトライする。 */
  get(path: string, query?: Query): Promise<unknown>;
  /** JSON 本文つきの書き込み。リトライしない。 */
  post(path: string, body: unknown): Promise<unknown>;
  put(path: string, body: unknown): Promise<unknown>;
  delete(path: string, body: unknown): Promise<unknown>;
  /** 文字列に紛れたアクセストークン・Bearer を伏せる(ログ用)。 */
  redact(text: string): string;
}

export interface KintoneClientOptions {
  host: string;
  accessToken: string;
  timeoutMs?: number;
  maxRetries?: number;
  maxRetryWaitSeconds?: number;
  maxApiCalls?: number;
  /** 待機の差し替え口(テスト用)。既定は setTimeout。 */
  sleep?: (ms: number) => Promise<void>;
}

const defaultSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

const abortError = () => new DOMException("aborted", "AbortError");

/** 本文の読み取りが上限を超えた(内部用。呼び出し側で response_too_large か「本文なし」に変換する)。 */
class BodyTooLarge extends Error {}

function appendQuery(target: URLSearchParams, query: Query): void {
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined) continue;
    if (Array.isArray(value)) {
      value.forEach((v, i) => {
        target.append(`${key}[${i}]`, String(v));
      });
    } else {
      target.append(key, String(value));
    }
  }
}

/** Retry-After(整数秒)を読む。日付形式・負・小数・空は読めない扱い(undefined)。 */
function parseRetryAfterSeconds(header: string | null): number | undefined {
  if (header === null || !/^\d+$/.test(header.trim())) return undefined;
  return Number(header.trim());
}

/** p の結果を待つ。signal が中断されたら(p が応じなくても)すぐ AbortError で失敗する。 */
function raceAbort<T>(p: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    if (signal.aborted) {
      reject(abortError());
      return;
    }
    const onAbort = () => reject(abortError());
    signal.addEventListener("abort", onAbort, { once: true });
    p.then(
      (v) => {
        signal.removeEventListener("abort", onAbort);
        resolve(v);
      },
      (e) => {
        signal.removeEventListener("abort", onAbort);
        reject(e);
      },
    );
  });
}

/** 本文を読む。上限 + 1 バイトを読んだ時点でやめて(cancel)BodyTooLarge。中断されたら cancel して AbortError。 */
async function readBody(res: Response, signal: AbortSignal, maxBytes: number): Promise<Uint8Array> {
  if (!res.body) return new Uint8Array(0);
  const reader = res.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    for (;;) {
      const { done, value } = await raceAbort(reader.read(), signal);
      if (done) break;
      total += value.byteLength;
      if (total > maxBytes) throw new BodyTooLarge();
      chunks.push(value);
    }
  } catch (e) {
    await reader.cancel().catch(() => undefined);
    throw e;
  }
  const out = new Uint8Array(total);
  let offset = 0;
  for (const c of chunks) {
    out.set(c, offset);
    offset += c.byteLength;
  }
  return out;
}

type Outcome =
  | { ok: true; value: unknown }
  | { ok: false; status: number; retryAfterSec: number | undefined; error: KintoneMcpError };

export function createKintoneClient(opts: KintoneClientOptions): KintoneClient {
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const maxRetries = opts.maxRetries ?? DEFAULT_MAX_RETRIES;
  const maxRetryWaitSeconds = opts.maxRetryWaitSeconds ?? DEFAULT_MAX_RETRY_WAIT_SECONDS;
  const maxApiCalls = opts.maxApiCalls ?? DEFAULT_MAX_API_CALLS;
  const sleep = opts.sleep ?? defaultSleep;

  let apiCalls = 0;
  // 直列化: 前のリクエスト(リトライの待機を含む)が終わるまで次を始めない。失敗しても後続は続ける。
  let tail: Promise<unknown> = Promise.resolve();

  /** kintone の本文や例外に紛れたアクセストークンと Bearer を伏せる。 */
  const redact = (text: string): string => {
    const withoutToken = opts.accessToken ? text.split(opts.accessToken).join("[REDACTED]") : text;
    return withoutToken.replace(/Bearer\s+[A-Za-z0-9._~+/=-]+/g, "Bearer [REDACTED]");
  };

  /** エラー応答の本文から kintone の `{code, id, message}` を読む。読めなければ空。 */
  async function readErrorDetail(res: Response, signal: AbortSignal): Promise<KintoneErrorDetail> {
    try {
      const text = new TextDecoder().decode(await readBody(res, signal, MAX_ERROR_BODY_BYTES));
      const parsed: unknown = JSON.parse(text);
      if (typeof parsed !== "object" || parsed === null) return {};
      const { code, id, message } = parsed as Record<string, unknown>;
      return {
        ...(typeof code === "string" ? { kintoneCode: redact(code) } : {}),
        ...(typeof id === "string" ? { kintoneId: redact(id) } : {}),
        ...(typeof message === "string" ? { kintoneMessage: redact(message) } : {}),
      };
    } catch {
      // 巨大・JSON でない・読み取りの失敗(期限切れを含む)は無視して status だけで分類する。
      await res.body?.cancel().catch(() => undefined);
      return {};
    }
  }

  /** 1 回の HTTP リクエスト。本文を読み終えるまで同じ期限(timeoutMs)で守る。 */
  async function attempt(
    method: HttpMethod,
    path: string,
    query: Query | undefined,
    body: unknown,
  ): Promise<Outcome> {
    if (apiCalls >= maxApiCalls) throw new KintoneCallLimitError(maxApiCalls);
    apiCalls++;

    const url = new URL(`https://${opts.host}${path}`);
    if (query) appendQuery(url.searchParams, query);
    const headers: Record<string, string> = {
      Authorization: `Bearer ${opts.accessToken}`,
      Accept: "application/json",
    };
    let payload: string | undefined;
    if (body !== undefined) {
      payload = JSON.stringify(body);
      headers["Content-Type"] = "application/json";
    }

    const controller = new AbortController();
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, timeoutMs);
    // 例外の文面にトークンやヘッダーが混ざらないよう、通信の原因は捨てて固定文にする。
    const transportError = () =>
      new KintoneApiError(
        0,
        timedOut ? { transport: "timeout", timeoutMs } : { transport: "unreachable" },
      );
    try {
      let res: Response;
      try {
        res = await raceAbort(
          fetch(url, {
            method,
            headers,
            body: payload,
            redirect: "manual",
            signal: controller.signal,
          }),
          controller.signal,
        );
      } catch {
        throw transportError();
      }

      if (!res.ok) {
        const detail = await readErrorDetail(res, controller.signal);
        if (res.status === 401) {
          return {
            ok: false,
            status: 401,
            retryAfterSec: undefined,
            error: new KintoneUnauthorizedError(detail),
          };
        }
        const retryAfterSec =
          res.status === 429 || res.status === 503
            ? parseRetryAfterSeconds(res.headers.get("Retry-After"))
            : undefined;
        return {
          ok: false,
          status: res.status,
          retryAfterSec,
          error: new KintoneApiError(res.status, {
            ...detail,
            ...(retryAfterSec === undefined ? {} : { retryAfterSec }),
          }),
        };
      }

      const declared = Number(res.headers.get("Content-Length"));
      if (
        res.headers.get("Content-Length") !== null &&
        Number.isFinite(declared) &&
        declared > MAX_RESPONSE_BYTES
      ) {
        await res.body?.cancel().catch(() => undefined);
        throw tooLarge();
      }
      let bytes: Uint8Array;
      try {
        bytes = await readBody(res, controller.signal, MAX_RESPONSE_BYTES);
      } catch (e) {
        if (e instanceof BodyTooLarge) throw tooLarge();
        throw transportError();
      }
      try {
        return { ok: true, value: JSON.parse(new TextDecoder().decode(bytes)) };
      } catch {
        throw new KintoneApiError(res.status, { transport: "invalid_body" });
      }
    } finally {
      clearTimeout(timer);
    }
  }

  const tooLarge = () =>
    new ResponseTooLargeError(
      "kintone response body exceeds the 10 MiB limit. Narrow the request (fewer fields, a smaller limit, a tighter query) and retry.",
    );

  async function execute(
    method: HttpMethod,
    path: string,
    query: Query | undefined,
    body: unknown,
  ): Promise<unknown> {
    for (let n = 0; ; n++) {
      const outcome = await attempt(method, path, query, body);
      if (outcome.ok) return outcome.value;
      // リトライするのは読み取りの 429・503 だけ。書き込みは 1 回で打ち切る。
      const retriable = method === "GET" && (outcome.status === 429 || outcome.status === 503);
      if (!retriable || n >= maxRetries) throw outcome.error;
      const waitSeconds = outcome.retryAfterSec ?? FALLBACK_RETRY_WAIT_SECONDS;
      if (waitSeconds > maxRetryWaitSeconds) throw outcome.error;
      // 次の試行ができないなら、待たずに上限のエラーにする。
      if (apiCalls >= maxApiCalls) throw new KintoneCallLimitError(maxApiCalls);
      await sleep(waitSeconds * 1000);
    }
  }

  /** 前のリクエスト(リトライの待機を含む)が終わるまで次を始めない。失敗しても後続は続ける。 */
  function serialize<T>(run: () => Promise<T>): Promise<T> {
    const p = tail.then(run);
    tail = p.catch(() => undefined);
    return p;
  }

  return {
    get: (path, query) => serialize(() => execute("GET", path, query, undefined)),
    post: (path, body) => serialize(() => execute("POST", path, undefined, body)),
    put: (path, body) => serialize(() => execute("PUT", path, undefined, body)),
    delete: (path, body) => serialize(() => execute("DELETE", path, undefined, body)),
    redact,
  };
}
