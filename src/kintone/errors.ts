/**
 * kintone REST のエラー(仕様書 §6 の HTTP の行)。クライアントが応答を分類して投げる。
 * message・details にトークンを含めない(kintone の message は呼び出し側が伏せてから渡す)。
 */
import { type ErrorCode, type ErrorDetails, KintoneMcpError } from "../errors";

/** kintone のエラー本文 `{code, id, message}` から読み取れたもの。 */
export interface KintoneErrorDetail {
  kintoneCode?: string;
  kintoneId?: string;
  /** kintone が返した利用者向けの文言(クライアントがトークンを伏せたもの)。 */
  kintoneMessage?: string;
  /** 429 の `Retry-After`(秒)。 */
  retryAfterSec?: number;
}

/**
 * HTTP の応答が得られなかった・読めなかった理由。
 * unreachable / timeout は status 0。invalid_body は応答自体は成功(2xx)だが本文が JSON でない。
 */
export interface KintoneTransportDetail extends KintoneErrorDetail {
  transport?: "unreachable" | "timeout" | "invalid_body";
  timeoutMs?: number;
}

/** kintone のメッセージの長さの上限(文字)。 */
const MAX_KINTONE_MESSAGE_CHARS = 500;

function detailsOf(status: number, d: KintoneErrorDetail): ErrorDetails {
  return {
    ...(status > 0 ? { http_status: status } : {}),
    ...(d.kintoneCode === undefined ? {} : { kintone_code: d.kintoneCode }),
    ...(d.kintoneId === undefined ? {} : { kintone_id: d.kintoneId }),
    ...(d.retryAfterSec === undefined ? {} : { retry_after_sec: d.retryAfterSec }),
  };
}

/** 仕様書 §6 の表の順。401・403・404・429 は(CB_VA の接頭辞があっても)ステータスが先。 */
function classify(
  status: number,
  kintoneCode: string | undefined,
  transport: KintoneTransportDetail["transport"],
): { code: ErrorCode; retryable: boolean } {
  if (transport) return { code: "api_error", retryable: true };
  if (status === 401) return { code: "authentication_error", retryable: false };
  if (status === 403) return { code: "permission_error", retryable: false };
  if (status === 404) return { code: "not_found", retryable: false };
  if (status === 429) return { code: "rate_limited", retryable: true };
  if (status === 400 || status === 422 || kintoneCode?.startsWith("CB_VA")) {
    return { code: "argument_error", retryable: false };
  }
  if (status >= 500) return { code: "api_error", retryable: true };
  return { code: "api_error", retryable: false };
}

function messageOf(status: number, d: KintoneTransportDetail): string {
  if (d.transport === "unreachable") return "kintone is unreachable (network error).";
  if (d.transport === "timeout") return `kintone request timed out after ${d.timeoutMs ?? 0}ms.`;
  if (d.transport === "invalid_body") {
    return `kintone returned a response that is not valid JSON (HTTP ${status}).`;
  }
  const code = d.kintoneCode ? `, ${d.kintoneCode}` : "";
  const text = d.kintoneMessage ? `: ${d.kintoneMessage.slice(0, MAX_KINTONE_MESSAGE_CHARS)}` : "";
  return `kintone API request failed (HTTP ${status}${code})${text}`;
}

/** kintone が 401 を返した(アクセストークンが失効・取り消し)。クライアントにリフレッシュさせる合図。 */
export class KintoneUnauthorizedError extends KintoneMcpError {
  constructor(detail: KintoneErrorDetail = {}) {
    super(
      "authentication_error",
      "kintone rejected the access token. Refresh the MCP token and retry.",
      false,
      detailsOf(401, detail),
    );
  }
}

/** kintone API のエラー。status 0 は「HTTP 応答を得られなかった」(通信失敗・タイムアウト)。 */
export class KintoneApiError extends KintoneMcpError {
  constructor(
    readonly status: number,
    detail: KintoneTransportDetail = {},
  ) {
    const { code, retryable } = classify(status, detail.kintoneCode, detail.transport);
    super(code, messageOf(status, detail), retryable, detailsOf(status, detail));
  }
}

/** 1 回のツール呼び出しで許す kintone への HTTP リクエスト数(リトライ含む)を超えた。 */
export class KintoneCallLimitError extends KintoneMcpError {
  constructor(readonly maxApiCalls: number) {
    super(
      "api_error",
      `kintone API call limit (${maxApiCalls} requests per tool call) exceeded.`,
      false,
    );
  }
}
