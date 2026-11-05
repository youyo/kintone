/**
 * ツールのエラー(最小版。M02 でエラー表の全行・`details` を足す)。
 * 仕様書 §6 のエンベロープ `{"schema_version":"1","error":{"code","message","retryable"}}` に変換する。
 */

/** M01 で使うコード。M02 で `permission_error` `not_found` `rate_limited` `response_too_large` `resolver_*` などを足す。 */
export const ERROR_CODES = [
  "argument_error",
  "authentication_error",
  "api_error",
  "generic_error",
] as const;
export type ErrorCode = (typeof ERROR_CODES)[number];

/** ツールの応答に載せてよいエラー。message は利用者(AI)にそのまま見える。トークンを含めないこと。 */
export class KintoneMcpError extends Error {
  constructor(
    readonly code: ErrorCode,
    message: string,
    readonly retryable = false,
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

export interface ErrorEnvelope {
  schema_version: "1";
  error: { code: ErrorCode; message: string; retryable: boolean };
}

export function errorEnvelope(code: ErrorCode, message: string, retryable: boolean): ErrorEnvelope {
  return { schema_version: "1", error: { code, message, retryable } };
}

export function toErrorEnvelope(e: unknown): ErrorEnvelope {
  if (e instanceof KintoneMcpError) return errorEnvelope(e.code, e.message, e.retryable);
  // 想定外の例外は内部情報を漏らさない。
  return errorEnvelope("generic_error", "Unexpected error.", false);
}
