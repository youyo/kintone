import { env } from "cloudflare:workers";
import { describe, expect, it } from "vitest";
import { checkConfig } from "../../src/config-check";

const valid = {
  OAUTH_KV: env.OAUTH_KV,
  KINTONE_HOST: "example.cybozu.com",
  PUBLIC_BASE_URL: "https://kintone-mcp.example.com",
  KINTONE_CLIENT_ID: "client-id-value",
  KINTONE_CLIENT_SECRET: "client-secret-value",
  ACCESS_TEAM_DOMAIN: "example-team.cloudflareaccess.com",
  ACCESS_AUD: "access-aud-value",
};

describe("checkConfig", () => {
  it("すべてそろっていれば ok", () => {
    expect(checkConfig(valid)).toEqual({ ok: true, missing: [], invalid: [] });
  });

  it.each([
    "OAUTH_KV",
    "KINTONE_HOST",
    "PUBLIC_BASE_URL",
    "KINTONE_CLIENT_ID",
    "KINTONE_CLIENT_SECRET",
    "ACCESS_TEAM_DOMAIN",
    "ACCESS_AUD",
  ] as const)("%s が無ければ missing に項目名だけが入る", (name) => {
    const { [name]: _removed, ...rest } = valid;
    expect(checkConfig(rest)).toEqual({ ok: false, missing: [name], invalid: [] });
  });

  it("空文字・空白だけの値は未設定として扱う(ACCESS_* を含む)", () => {
    const result = checkConfig({
      ...valid,
      KINTONE_CLIENT_ID: "",
      KINTONE_CLIENT_SECRET: "  ",
      ACCESS_TEAM_DOMAIN: "",
      ACCESS_AUD: "   ",
    });
    expect(result.ok).toBe(false);
    expect(result.missing).toEqual([
      "KINTONE_CLIENT_ID",
      "KINTONE_CLIENT_SECRET",
      "ACCESS_TEAM_DOMAIN",
      "ACCESS_AUD",
    ]);
  });

  it("KV バインディングが KV でない値(文字列など)なら missing", () => {
    expect(checkConfig({ ...valid, OAUTH_KV: "not-a-kv" }).missing).toEqual(["OAUTH_KV"]);
  });

  it("文字列でない値(数値など)は未設定として扱う", () => {
    expect(checkConfig({ ...valid, ACCESS_AUD: 123 }).missing).toEqual(["ACCESS_AUD"]);
  });

  it("プレースホルダ(<...>)のまま残っている値は invalid", () => {
    const result = checkConfig({
      ...valid,
      PUBLIC_BASE_URL: "https://<domain>",
      KINTONE_HOST: "<subdomain>.cybozu.com",
      KINTONE_CLIENT_ID: "<client-id>",
      ACCESS_TEAM_DOMAIN: "<team>.cloudflareaccess.com",
      ACCESS_AUD: "<aud>",
    });
    expect(result.missing).toEqual([]);
    expect(result.invalid).toEqual([
      "KINTONE_HOST",
      "PUBLIC_BASE_URL",
      "KINTONE_CLIENT_ID",
      "ACCESS_TEAM_DOMAIN",
      "ACCESS_AUD",
    ]);
  });

  it.each([
    ["http://kintone-mcp.example.com", false],
    ["kintone-mcp.example.com", false],
    ["https://kintone-mcp.example.com/mcp", false],
    ["https://kintone-mcp.example.com?x=1", false],
    ["https://kintone-mcp.example.com#frag", false],
    ["https://kintone-mcp.example.com/", true],
    ["https://kintone-mcp.example.com", true],
    ["http://localhost:8787", true],
    ["http://127.0.0.1:8787", true],
  ])("PUBLIC_BASE_URL %s の妥当性は %s", (url, isValid) => {
    const result = checkConfig({ ...valid, PUBLIC_BASE_URL: url });
    expect(result.invalid.includes("PUBLIC_BASE_URL")).toBe(!isValid);
  });

  it.each([
    ["https://example.cybozu.com", false],
    ["example.cybozu.com/", false],
    ["example.cybozu.com/k/v1", false],
    ["example cybozu.com", false],
    ["localhost", false],
    ["example.cybozu.com", true],
    ["example.kintone.com", true],
  ])("KINTONE_HOST %s の妥当性は %s", (host, isValid) => {
    const result = checkConfig({ ...valid, KINTONE_HOST: host });
    expect(result.invalid.includes("KINTONE_HOST")).toBe(!isValid);
  });

  it.each([
    ["https://example-team.cloudflareaccess.com", false],
    ["example-team.cloudflareaccess.com/", false],
    ["example-team.cloudflareaccess.com/cdn-cgi/access/certs", false],
    ["example team.cloudflareaccess.com", false],
    ["example-team.cloudflareaccess.com", true],
  ])("ACCESS_TEAM_DOMAIN %s の妥当性は %s", (host, isValid) => {
    const result = checkConfig({ ...valid, ACCESS_TEAM_DOMAIN: host });
    expect(result.invalid.includes("ACCESS_TEAM_DOMAIN")).toBe(!isValid);
  });

  it("ACCESS_AUD は形式を検査しない(プレースホルダでなければ ok)", () => {
    expect(checkConfig({ ...valid, ACCESS_AUD: "any-aud-string-0123" }).ok).toBe(true);
  });

  describe("KINTONE_OAUTH_PKCE(任意)", () => {
    it("未設定・s256・off は ok", () => {
      expect(checkConfig(valid).ok).toBe(true);
      expect(checkConfig({ ...valid, KINTONE_OAUTH_PKCE: "s256" }).ok).toBe(true);
      expect(checkConfig({ ...valid, KINTONE_OAUTH_PKCE: "off" }).ok).toBe(true);
    });

    it.each(["S256x", "on", "plain", "false", ""])("不正値 %j は invalid", (value) => {
      const result = checkConfig({ ...valid, KINTONE_OAUTH_PKCE: value });
      expect(result).toEqual({ ok: false, missing: [], invalid: ["KINTONE_OAUTH_PKCE"] });
    });
  });

  it("結果に値(秘密を含む)を載せない", () => {
    const result = checkConfig({
      ...valid,
      PUBLIC_BASE_URL: "https://<domain>/secret-path",
      KINTONE_CLIENT_SECRET: "<super-secret>",
      ACCESS_AUD: "<aud-secret>",
      KINTONE_OAUTH_PKCE: "bogus-pkce",
    });
    const json = JSON.stringify(result);
    for (const leaked of ["super-secret", "secret-path", "aud-secret", "bogus-pkce"]) {
      expect(json).not.toContain(leaked);
    }
  });
});
