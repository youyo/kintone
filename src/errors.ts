/**
 * ツールのエラー(仕様書 §6、ADR 0006)。
 * `{"schema_version":"1","error":{"code","message","retryable","details"?}}` のエンベロープに変換する。
 * message は利用者(AI)にそのまま見える。トークン・Authorization ヘッダーを含めないこと。
 */

/** エラー表(仕様書 §6)の `code` の全体。小文字 snake_case。 */
export const ERROR_CODES = [
  "argument_error",
  "authentication_error",
  "permission_error",
  "not_found",
  "rate_limited",
  "api_error",
  "response_too_large",
  "resolver_app_not_found",
  "resolver_app_ambiguous",
  "resolver_app_exact_match_required",
  "resolver_field_exact_match_required",
  "resolver_field_not_found",
  "resolver_field_ambiguous",
  "resolver_app_list_too_large",
  "generic_error",
] as const;
export type ErrorCode = (typeof ERROR_CODES)[number];

/** リゾルバー(M03)が投げるコード。 */
export type ResolverErrorCode = Extract<ErrorCode, `resolver_${string}`>;

/** アプリの候補。空の項目は省略する(仕様書 §7)。 */
export interface AppCandidate {
  id: string;
  code?: string;
  name?: string;
}

/** フィールドの候補(コード順)。 */
export interface FieldCandidate {
  code: string;
  label?: string;
}

/** logbutler 形式への拡張(任意)。キーは仕様書 §6 のとおり。 */
export interface ErrorDetails {
  http_status?: number;
  kintone_code?: string;
  kintone_id?: string;
  retry_after_sec?: number;
  kind?: "app" | "field";
  ref?: string;
  candidates?: (AppCandidate | FieldCandidate)[];
}

/** ツールの応答に載せてよいエラー。 */
export class KintoneMcpError extends Error {
  constructor(
    readonly code: ErrorCode,
    message: string,
    readonly retryable = false,
    readonly details?: ErrorDetails,
  ) {
    super(message);
    this.name = new.target.name;
  }
}

/** 入力が不正。 */
export class ArgumentError extends KintoneMcpError {
  constructor(message: string) {
    super("argument_error", message, false);
  }
}

/** 応答が上限(`max_response_tokens` または kintone の応答本文 10 MiB)を超えた。切り詰めずに失敗させる。 */
export class ResponseTooLargeError extends KintoneMcpError {
  constructor(message: string) {
    super("response_too_large", message, false);
  }
}

/** 名前解決の失敗(M03 のリゾルバーが投げる。`details.candidates` で AI の再試行を助ける)。 */
export class ResolverError extends KintoneMcpError {
  constructor(code: ResolverErrorCode, message: string, details?: ErrorDetails) {
    super(code, message, false, details);
  }
}

export interface ErrorEnvelope {
  schema_version: "1";
  error: {
    code: ErrorCode;
    message: string;
    retryable: boolean;
    details?: ErrorDetails;
  };
}

/** undefined の項目を落とし、空ならキーごと出さない。 */
function compactDetails(details: ErrorDetails | undefined): ErrorDetails | undefined {
  if (!details) return undefined;
  const entries = Object.entries(details).filter(([, v]) => v !== undefined);
  return entries.length === 0 ? undefined : (Object.fromEntries(entries) as ErrorDetails);
}

export function errorEnvelope(
  code: ErrorCode,
  message: string,
  retryable: boolean,
  details?: ErrorDetails,
): ErrorEnvelope {
  const compact = compactDetails(details);
  return {
    schema_version: "1",
    error: { code, message, retryable, ...(compact ? { details: compact } : {}) },
  };
}

export function toErrorEnvelope(e: unknown): ErrorEnvelope {
  if (e instanceof KintoneMcpError) return errorEnvelope(e.code, e.message, e.retryable, e.details);
  // 想定外の例外は内部情報を漏らさない。
  return errorEnvelope("generic_error", "Unexpected error.", false);
}
