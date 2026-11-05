/**
 * kintone REST クライアント(最小版。M02 で本格化: リトライ・呼び出し回数上限・直列化・応答サイズ上限・エラー表)。
 * 認証は利用者の OAuth アクセストークンの `Authorization: Bearer`(API トークン経路は持たない。ADR 0001)。
 * トークンをエラーメッセージ・ログに出さない。
 */
import { KintoneMcpError } from "../errors";

/** 1 リクエストのタイムアウト(仕様書 §8)。 */
export const DEFAULT_TIMEOUT_MS = 30_000;

export type QueryValue = string | number | boolean | undefined;
export type Query = Record<string, QueryValue>;

/** kintone が 401 を返した(アクセストークンが失効・取り消し)。クライアントにリフレッシュさせる合図。 */
export class KintoneUnauthorizedError extends KintoneMcpError {
  constructor() {
    super(
      "authentication_error",
      "kintone rejected the access token. Refresh the MCP token and retry.",
      false,
    );
  }
}

/** kintone API のエラー。status 0 は「HTTP 応答を得られなかった」(通信失敗・タイムアウト)。 */
export class KintoneApiError extends KintoneMcpError {
  constructor(readonly status: number) {
    super(
      "api_error",
      status === 0
        ? "kintone API request failed (no response)."
        : `kintone API request failed (HTTP ${status}).`,
      status === 0 || status >= 500,
    );
  }
}

export interface KintoneClient {
  /** `GET https://<host><path>?<query>` を呼び、JSON を返す。path は `/k/v1/...`。 */
  get(path: string, query?: Query): Promise<unknown>;
}

export interface KintoneClientOptions {
  host: string;
  accessToken: string;
  timeoutMs?: number;
}

export function createKintoneClient(opts: KintoneClientOptions): KintoneClient {
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  return {
    async get(path: string, query: Query = {}): Promise<unknown> {
      const url = new URL(`https://${opts.host}${path}`);
      for (const [key, value] of Object.entries(query)) {
        if (value !== undefined) url.searchParams.set(key, String(value));
      }
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeoutMs);
      try {
        let res: Response;
        try {
          res = await fetch(url, {
            method: "GET",
            headers: { Authorization: `Bearer ${opts.accessToken}`, Accept: "application/json" },
            signal: controller.signal,
          });
        } catch {
          throw new KintoneApiError(0);
        }
        if (res.status === 401) throw new KintoneUnauthorizedError();
        if (!res.ok) throw new KintoneApiError(res.status);
        try {
          return await res.json();
        } catch {
          throw new KintoneApiError(502);
        }
      } finally {
        clearTimeout(timer);
      }
    },
  };
}
