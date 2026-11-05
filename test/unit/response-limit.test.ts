import { describe, expect, it } from "vitest";
import { ArgumentError, ResponseTooLargeError, toErrorEnvelope } from "../../src/errors";
import {
  assertWithinResponseLimit,
  DEFAULT_MAX_RESPONSE_TOKENS,
  estimateTokens,
  maxResponseTokensShape,
  resolveResponseLimit,
} from "../../src/mcp/response-limit";

describe("estimateTokens", () => {
  it("ASCII は 4 文字 1 トークン(切り上げ)", () => {
    expect(estimateTokens("")).toBe(0);
    expect(estimateTokens("a")).toBe(1);
    expect(estimateTokens("abcd")).toBe(1);
    expect(estimateTokens("abcde")).toBe(2);
  });

  it("ASCII 以外は 1 文字 1 トークン", () => {
    expect(estimateTokens("顧客管理")).toBe(4);
    expect(estimateTokens("顧客abcd")).toBe(3);
    expect(estimateTokens("😀")).toBe(1);
  });
});

describe("resolveResponseLimit", () => {
  it("未指定は既定の 50000", () => {
    expect(DEFAULT_MAX_RESPONSE_TOKENS).toBe(50000);
    expect(resolveResponseLimit(undefined)).toBe(50000);
  });

  it("0 は無効(0 を返す)、正の値はそのまま", () => {
    expect(resolveResponseLimit(0)).toBe(0);
    expect(resolveResponseLimit(123)).toBe(123);
  });

  it("負の値は argument_error", () => {
    expect(() => resolveResponseLimit(-1)).toThrow(ArgumentError);
    expect(() => resolveResponseLimit(-1)).toThrow(/max_response_tokens/);
  });
});

describe("assertWithinResponseLimit", () => {
  it("上限ちょうどは通り、1 超えると response_too_large(切り詰めない)", () => {
    const text = "a".repeat(400); // 100 トークン
    expect(() => assertWithinResponseLimit(text, 100)).not.toThrow();
    const err = (() => {
      try {
        assertWithinResponseLimit(text, 99);
      } catch (e) {
        return e;
      }
    })();
    expect(err).toBeInstanceOf(ResponseTooLargeError);
    const e = toErrorEnvelope(err).error;
    expect(e.code).toBe("response_too_large");
    expect(e.retryable).toBe(false);
    // 絞り込みの案内: 推定トークン数・上限・対処(fields / limit / max_response_tokens)
    expect(e.message).toContain("100");
    expect(e.message).toContain("99");
    expect(e.message).toContain("max_response_tokens");
    expect(e.message).toMatch(/narrow|fields|limit/i);
  });

  it("0 は無制限", () => {
    expect(() => assertWithinResponseLimit("a".repeat(10_000_000), 0)).not.toThrow();
  });
});

describe("maxResponseTokensShape", () => {
  it("整数の任意引数。説明に既定値と 0 で無効を書く", () => {
    const schema = maxResponseTokensShape.max_response_tokens;
    expect(schema.safeParse(undefined).success).toBe(true);
    expect(schema.safeParse(0).success).toBe(true);
    expect(schema.safeParse(-5).success).toBe(true); // 負の検証は resolveResponseLimit(argument_error)
    expect(schema.safeParse(1.5).success).toBe(false);
    expect(schema.safeParse("10").success).toBe(false);
    expect(schema.description).toContain("50000");
    expect(schema.description).toContain("0");
  });
});
