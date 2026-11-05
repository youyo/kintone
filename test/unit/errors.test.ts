import { describe, expect, it } from "vitest";
import {
  ArgumentError,
  ERROR_CODES,
  errorEnvelope,
  KintoneMcpError,
  ResolverError,
  ResponseTooLargeError,
  toErrorEnvelope,
} from "../../src/errors";

describe("ERROR_CODES(仕様書 §6 の code の全体)", () => {
  it("小文字 snake_case で、表のコードをすべて含む(重複なし)", () => {
    expect([...ERROR_CODES].sort()).toEqual(
      [
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
      ].sort(),
    );
    expect(new Set(ERROR_CODES).size).toBe(ERROR_CODES.length);
    for (const code of ERROR_CODES) expect(code).toMatch(/^[a-z]+(?:_[a-z]+)*$/);
  });
});

describe("エンベロープ", () => {
  it("schema_version と error.{code,message,retryable} を持つ。details がなければキーごと出さない", () => {
    const env = toErrorEnvelope(new ArgumentError("bad"));
    expect(env).toEqual({
      schema_version: "1",
      error: { code: "argument_error", message: "bad", retryable: false },
    });
    expect("details" in env.error).toBe(false);
  });

  it("空の details({} や undefined のキーだけ)はキーごと出さない", () => {
    const env = toErrorEnvelope(
      new KintoneMcpError("api_error", "x", true, { http_status: undefined }),
    );
    expect("details" in env.error).toBe(false);
    expect(errorEnvelope("api_error", "x", true, {}).error).not.toHaveProperty("details");
  });

  it("details は全キー(http_status / kintone_code / kintone_id / retry_after_sec / kind / ref / candidates)を載せられる", () => {
    const details = {
      http_status: 429,
      kintone_code: "GAIA_TM12",
      kintone_id: "abc",
      retry_after_sec: 3,
      kind: "app" as const,
      ref: "顧客",
      candidates: [{ id: "12", code: "CRM", name: "顧客管理" }],
    };
    const env = toErrorEnvelope(new KintoneMcpError("rate_limited", "slow down", true, details));
    expect(env.error.details).toEqual(details);
    expect(env.error.retryable).toBe(true);
  });

  it("想定外の例外は固定文の generic_error(例外のメッセージを出さない)", () => {
    const env = toErrorEnvelope(new Error("internal: token kt-at-1"));
    expect(env).toEqual({
      schema_version: "1",
      error: { code: "generic_error", message: "Unexpected error.", retryable: false },
    });
    expect(JSON.stringify(toErrorEnvelope("kt-at-1"))).not.toContain("kt-at-1");
  });

  it("ResponseTooLargeError は response_too_large(retryable でない)", () => {
    const env = toErrorEnvelope(new ResponseTooLargeError("too big"));
    expect(env.error).toEqual({
      code: "response_too_large",
      message: "too big",
      retryable: false,
    });
  });

  it("ResolverError は resolver_* のコードと details(kind / ref / candidates)をそのまま運ぶ", () => {
    const err = new ResolverError("resolver_app_ambiguous", "multiple", {
      kind: "app",
      ref: "顧客",
      candidates: [
        { id: "12", code: "CRM", name: "顧客管理" },
        { id: "13", name: "顧客リスト" },
      ],
    });
    expect(err).toBeInstanceOf(KintoneMcpError);
    expect(toErrorEnvelope(err).error).toEqual({
      code: "resolver_app_ambiguous",
      message: "multiple",
      retryable: false,
      details: {
        kind: "app",
        ref: "顧客",
        candidates: [
          { id: "12", code: "CRM", name: "顧客管理" },
          { id: "13", name: "顧客リスト" },
        ],
      },
    });
  });
});
