import { describe, expect, it } from "vitest";
import { codeChallengeS256, generateCodeVerifier } from "../../src/auth/pkce";

describe("PKCE (RFC 7636)", () => {
  it("RFC 7636 付録 B のテストベクトルと一致する", async () => {
    expect(await codeChallengeS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")).toBe(
      "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
    );
  });

  it("verifier は 43〜128 文字の URL-safe 文字(unreserved)で、毎回違う", () => {
    const seen = new Set<string>();
    for (let i = 0; i < 20; i++) {
      const v = generateCodeVerifier();
      expect(v.length).toBeGreaterThanOrEqual(43);
      expect(v.length).toBeLessThanOrEqual(128);
      expect(v).toMatch(/^[A-Za-z0-9\-._~]+$/);
      seen.add(v);
    }
    expect(seen.size).toBe(20);
  });

  it("challenge は 43 文字の base64url(パディングなし)", async () => {
    expect(await codeChallengeS256(generateCodeVerifier())).toMatch(/^[A-Za-z0-9_-]{43}$/);
  });
});
