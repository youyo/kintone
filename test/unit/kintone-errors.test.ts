import { describe, expect, it } from "vitest";
import { toErrorEnvelope } from "../../src/errors";
import {
  KintoneApiError,
  KintoneCallLimitError,
  KintoneUnauthorizedError,
} from "../../src/kintone/errors";

const envelope = (e: unknown) => toErrorEnvelope(e).error;

describe("KintoneApiError の分類(仕様書 §6 の HTTP の行)", () => {
  it.each([
    [403, "permission_error", false],
    [404, "not_found", false],
    [400, "argument_error", false],
    [422, "argument_error", false],
    [429, "rate_limited", true],
    [405, "api_error", false],
    [409, "api_error", false],
    [413, "api_error", false],
    [302, "api_error", false],
    [500, "api_error", true],
    [502, "api_error", true],
    [503, "api_error", true],
    [520, "api_error", true],
  ])("HTTP %i → %s(retryable=%s)", (status, code, retryable) => {
    const e = envelope(new KintoneApiError(status));
    expect(e.code).toBe(code);
    expect(e.retryable).toBe(retryable);
    expect(e.details?.http_status).toBe(status);
  });

  it("kintone コードが CB_VA で始まれば、HTTP ステータスによらず argument_error", () => {
    for (const status of [400, 500, 520, 200]) {
      const e = envelope(new KintoneApiError(status, { kintoneCode: "CB_VA01" }));
      expect(e.code, `status ${status}`).toBe("argument_error");
      expect(e.retryable).toBe(false);
    }
  });

  it("401・403・404・429 はステータスが先(CB_VA 接頭辞があっても argument_error にしない)", () => {
    expect(envelope(new KintoneApiError(403, { kintoneCode: "CB_VA01" })).code).toBe(
      "permission_error",
    );
    expect(envelope(new KintoneApiError(404, { kintoneCode: "CB_VA01" })).code).toBe("not_found");
    expect(envelope(new KintoneApiError(429, { kintoneCode: "CB_VA01" })).code).toBe(
      "rate_limited",
    );
  });

  it("kintone の code / id / message と Retry-After を details と message に載せる", () => {
    const e = envelope(
      new KintoneApiError(404, {
        kintoneCode: "GAIA_AP01",
        kintoneId: "id-1",
        kintoneMessage: "指定したアプリ(id: 9)が見つかりません。",
      }),
    );
    expect(e.details).toEqual({ http_status: 404, kintone_code: "GAIA_AP01", kintone_id: "id-1" });
    expect(e.message).toContain("HTTP 404");
    expect(e.message).toContain("GAIA_AP01");
    expect(e.message).toContain("指定したアプリ(id: 9)が見つかりません。");

    const limited = envelope(new KintoneApiError(429, { retryAfterSec: 3 }));
    expect(limited.details).toEqual({ http_status: 429, retry_after_sec: 3 });
  });

  it("長い kintone message は 500 文字に切る", () => {
    const e = envelope(new KintoneApiError(400, { kintoneMessage: "あ".repeat(2000) }));
    expect(e.message.length).toBeLessThan(700);
  });

  it("status 0 は通信失敗: api_error(retryable)で http_status を持たない。timeout は message で区別", () => {
    const net = new KintoneApiError(0, { transport: "unreachable" });
    expect(net.status).toBe(0);
    expect(envelope(net)).toEqual({
      code: "api_error",
      message: "kintone is unreachable (network error).",
      retryable: true,
    });
    const timeout = envelope(new KintoneApiError(0, { transport: "timeout", timeoutMs: 30000 }));
    expect(timeout.code).toBe("api_error");
    expect(timeout.retryable).toBe(true);
    expect(timeout.message).toContain("timed out");
    expect(timeout.message).toContain("30000");
    expect(timeout.details).toBeUndefined();
  });

  it("成功ステータスなのに JSON でない応答: api_error(retryable)。http_status は実際の 200 で、message で区別する", () => {
    const err = new KintoneApiError(200, { transport: "invalid_body" });
    expect(err.status).toBe(200);
    const e = envelope(err);
    expect(e.code).toBe("api_error");
    expect(e.retryable).toBe(true);
    expect(e.details).toEqual({ http_status: 200 });
    expect(e.message).toContain("not valid JSON");
  });
});

describe("KintoneUnauthorizedError", () => {
  it("authentication_error(retryable でない)。details に http_status 401 と kintone のコード", () => {
    const err = new KintoneUnauthorizedError({ kintoneCode: "CB_AU01", kintoneId: "x" });
    const e = envelope(err);
    expect(e.code).toBe("authentication_error");
    expect(e.retryable).toBe(false);
    expect(e.details).toEqual({ http_status: 401, kintone_code: "CB_AU01", kintone_id: "x" });
  });

  it("引数なしでも作れる(M01 との互換)", () => {
    expect(envelope(new KintoneUnauthorizedError()).code).toBe("authentication_error");
  });
});

describe("KintoneCallLimitError", () => {
  it("api_error(retryable でない)。上限の数を message に含む", () => {
    const e = envelope(new KintoneCallLimitError(50));
    expect(e.code).toBe("api_error");
    expect(e.retryable).toBe(false);
    expect(e.message).toContain("50");
  });
});
