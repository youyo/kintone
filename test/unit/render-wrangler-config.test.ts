import { describe, expect, it } from "vitest";
import {
  ENV_NAMES,
  EXAMPLE_ENV,
  formatErrors,
  parseJsonc,
  type RenderError,
  renderWranglerConfig,
} from "../../scripts/render-wrangler-config/core";

/** upstream の wrangler.jsonc を模した共有設定(コメント・末尾カンマ・env ブロックつき)。 */
const BASE = `{
  // コメント
  "name": "kintone-mcp",
  "main": "src/index.ts",
  "compatibility_date": "2026-10-13",
  "compatibility_flags": [],
  "observability": { "enabled": true },
  "kv_namespaces": [
    { "binding": "OAUTH_KV", "id": "00000000000000000000000000000000" },
    { "binding": "OTHER_KV", "id": "11111111111111111111111111111111" },
  ],
  "vars": {
    "KINTONE_HOST": "example.cybozu.com", // ダミー
    "PUBLIC_BASE_URL": "https://kintone-mcp.example.com",
    "ACCESS_TEAM_DOMAIN": "example-team.cloudflareaccess.com",
    "ACCESS_AUD": "dummy",
    "EXTRA": "kept /* not a comment */ https://x.example/a",
  },
  "env": { "production": { "name": "must-not-leak" } },
  "routes": [{ "pattern": "old.example.com", "custom_domain": true }],
}
`;

const VALID: Record<string, string> = {
  KINTONE_MCP_WORKER_NAME: "my-kintone-mcp",
  KINTONE_MCP_KV_NAMESPACE_ID: "0123456789abcdef0123456789abcdef",
  KINTONE_MCP_HOST: "acme.cybozu.com",
  KINTONE_MCP_PUBLIC_BASE_URL: "https://kintone-mcp.acme.example.org",
  KINTONE_MCP_ACCESS_TEAM_DOMAIN: "acme.cloudflareaccess.com",
  KINTONE_MCP_ACCESS_AUD: "abc123def456",
};

function ok(env: Record<string, string | undefined>, base = BASE) {
  const r = renderWranglerConfig(base, env);
  if (!r.ok) throw new Error(`expected ok, got ${JSON.stringify(r.errors)}`);
  return r;
}

function errors(env: Record<string, string | undefined>, base = BASE): RenderError[] {
  const r = renderWranglerConfig(base, env);
  if (r.ok) throw new Error("expected errors");
  return r.errors;
}

describe("環境変数の名前", () => {
  it("仕様書 §9 の項目に合わせた KINTONE_MCP_* で、任意は 2 つだけ", () => {
    expect(ENV_NAMES).toEqual({
      workerName: "KINTONE_MCP_WORKER_NAME",
      kvNamespaceId: "KINTONE_MCP_KV_NAMESPACE_ID",
      host: "KINTONE_MCP_HOST",
      publicBaseUrl: "KINTONE_MCP_PUBLIC_BASE_URL",
      accessTeamDomain: "KINTONE_MCP_ACCESS_TEAM_DOMAIN",
      accessAud: "KINTONE_MCP_ACCESS_AUD",
      customDomain: "KINTONE_MCP_CUSTOM_DOMAIN",
      oauthPkce: "KINTONE_MCP_OAUTH_PKCE",
    });
  });
});

describe("生成", () => {
  it("name・KV の id・vars を差し替え、それ以外は wrangler.jsonc のまま引き継ぐ", () => {
    const { config } = ok(VALID);
    expect(config.name).toBe("my-kintone-mcp");
    expect(config.main).toBe("src/index.ts");
    expect(config.compatibility_date).toBe("2026-10-13");
    expect(config.compatibility_flags).toEqual([]);
    expect(config.observability).toEqual({ enabled: true });
    expect(config.kv_namespaces).toEqual([
      { binding: "OAUTH_KV", id: "0123456789abcdef0123456789abcdef" },
      { binding: "OTHER_KV", id: "11111111111111111111111111111111" },
    ]);
    expect(config.vars).toEqual({
      KINTONE_HOST: "acme.cybozu.com",
      PUBLIC_BASE_URL: "https://kintone-mcp.acme.example.org",
      ACCESS_TEAM_DOMAIN: "acme.cloudflareaccess.com",
      ACCESS_AUD: "abc123def456",
      EXTRA: "kept /* not a comment */ https://x.example/a",
    });
  });

  it("env ブロックは持ち越さない(フラット)。routes は環境変数で決まる", () => {
    const { config } = ok(VALID);
    expect(config.env).toBeUndefined();
    expect(config.routes).toBeUndefined();
    expect(config.workers_dev).toBe(true);
  });

  it("カスタムドメインがあれば custom_domain の route にして workers.dev を無効にする", () => {
    const { config } = ok({ ...VALID, KINTONE_MCP_CUSTOM_DOMAIN: "kintone-mcp.acme.example.org" });
    expect(config.routes).toEqual([
      { pattern: "kintone-mcp.acme.example.org", custom_domain: true },
    ]);
    expect(config.workers_dev).toBe(false);
  });

  it("KINTONE_MCP_OAUTH_PKCE があれば vars.KINTONE_OAUTH_PKCE に入る。なければ vars に足さない", () => {
    expect(ok(VALID).config.vars).not.toHaveProperty("KINTONE_OAUTH_PKCE");
    for (const mode of ["s256", "off"]) {
      const { config } = ok({ ...VALID, KINTONE_MCP_OAUTH_PKCE: mode });
      expect((config.vars as Record<string, string>).KINTONE_OAUTH_PKCE).toBe(mode);
    }
  });

  it("前後の空白は取り除く", () => {
    const { config } = ok({
      ...VALID,
      KINTONE_MCP_WORKER_NAME: "  my-kintone-mcp\n",
      KINTONE_MCP_ACCESS_AUD: " abc123def456 ",
    });
    expect(config.name).toBe("my-kintone-mcp");
    expect((config.vars as Record<string, string>).ACCESS_AUD).toBe("abc123def456");
  });

  it("OAUTH_KV の束縛が wrangler.jsonc になければ足す", () => {
    const base = `{ "name": "x", "main": "src/index.ts", "vars": {} }`;
    const { config } = ok(VALID, base);
    expect(config.kv_namespaces).toEqual([
      { binding: "OAUTH_KV", id: "0123456789abcdef0123456789abcdef" },
    ]);
  });

  it("text は末尾が改行 1 つの JSON で、config と一致する", () => {
    const { text, config } = ok(VALID);
    expect(text.endsWith("}\n")).toBe(true);
    expect(JSON.parse(text)).toEqual(config);
  });

  it("入力のオブジェクトを書き換えない(同じ入力なら同じ出力)", () => {
    const env = { ...VALID };
    const a = ok(env).text;
    const b = ok(env).text;
    expect(a).toBe(b);
    expect(env).toEqual(VALID);
  });
});

describe("検証: 欠け・形式違い・プレースホルダ", () => {
  const REQUIRED = [
    "KINTONE_MCP_WORKER_NAME",
    "KINTONE_MCP_KV_NAMESPACE_ID",
    "KINTONE_MCP_HOST",
    "KINTONE_MCP_PUBLIC_BASE_URL",
    "KINTONE_MCP_ACCESS_TEAM_DOMAIN",
    "KINTONE_MCP_ACCESS_AUD",
  ];

  it.each(REQUIRED)("%s が未定義・空・空白のみなら missing", (name) => {
    const { [name]: _omit, ...undefinedCase } = VALID;
    for (const env of [undefinedCase, { ...VALID, [name]: "" }, { ...VALID, [name]: "  \n " }]) {
      expect(errors(env)).toEqual([{ name, reason: "missing" }]);
    }
  });

  const INVALID: [string, string][] = [
    ["KINTONE_MCP_WORKER_NAME", "My_Worker"],
    ["KINTONE_MCP_WORKER_NAME", "-leading-hyphen"],
    ["KINTONE_MCP_WORKER_NAME", "a".repeat(64)],
    ["KINTONE_MCP_KV_NAMESPACE_ID", "0123456789abcdef"],
    ["KINTONE_MCP_KV_NAMESPACE_ID", "g123456789abcdef0123456789abcdef"],
    ["KINTONE_MCP_HOST", "https://acme.cybozu.com"],
    ["KINTONE_MCP_HOST", "acme.cybozu.com/k"],
    ["KINTONE_MCP_HOST", "acme.cybozu.com:443"],
    ["KINTONE_MCP_HOST", "acme"],
    ["KINTONE_MCP_PUBLIC_BASE_URL", "http://kintone-mcp.acme.example.org"],
    ["KINTONE_MCP_PUBLIC_BASE_URL", "https://kintone-mcp.acme.example.org/"],
    ["KINTONE_MCP_PUBLIC_BASE_URL", "https://kintone-mcp.acme.example.org/mcp"],
    ["KINTONE_MCP_PUBLIC_BASE_URL", "https://kintone-mcp.acme.example.org?x=1"],
    ["KINTONE_MCP_PUBLIC_BASE_URL", "https://Kintone-MCP.acme.example.org"],
    ["KINTONE_MCP_PUBLIC_BASE_URL", "kintone-mcp.acme.example.org"],
    ["KINTONE_MCP_ACCESS_TEAM_DOMAIN", "https://acme.cloudflareaccess.com"],
    ["KINTONE_MCP_ACCESS_TEAM_DOMAIN", "acme.cloudflareaccess.com/cdn-cgi"],
    ["KINTONE_MCP_ACCESS_AUD", "has space"],
    ["KINTONE_MCP_CUSTOM_DOMAIN", "https://kintone-mcp.acme.example.org"],
    ["KINTONE_MCP_OAUTH_PKCE", "on"],
    ["KINTONE_MCP_OAUTH_PKCE", "S256"],
  ];

  it.each(INVALID)("%s に形式違いの値を入れると invalid", (name, value) => {
    expect(errors({ ...VALID, [name]: value })).toEqual([{ name, reason: "invalid" }]);
  });

  const PLACEHOLDERS: [string, string][] = [
    ["KINTONE_MCP_WORKER_NAME", "<worker-name>"],
    ["KINTONE_MCP_KV_NAMESPACE_ID", "<kv-namespace-id>"],
    ["KINTONE_MCP_HOST", "<subdomain>.cybozu.com"],
    ["KINTONE_MCP_PUBLIC_BASE_URL", "https://<domain>"],
    ["KINTONE_MCP_ACCESS_TEAM_DOMAIN", "<team>.cloudflareaccess.com"],
    ["KINTONE_MCP_ACCESS_AUD", "<aud-tag>"],
    ["KINTONE_MCP_CUSTOM_DOMAIN", "<domain>"],
    ["KINTONE_MCP_OAUTH_PKCE", "<s256-or-off>"],
  ];

  it.each(PLACEHOLDERS)("%s にプレースホルダ(<...>)が残っていれば placeholder", (name, value) => {
    expect(errors({ ...VALID, [name]: value })).toEqual([{ name, reason: "placeholder" }]);
  });

  it("任意の値は、空・空白のみなら未指定として扱い、エラーにしない", () => {
    const { config } = ok({
      ...VALID,
      KINTONE_MCP_CUSTOM_DOMAIN: "  ",
      KINTONE_MCP_OAUTH_PKCE: "",
    });
    expect(config.workers_dev).toBe(true);
    expect(config.vars).not.toHaveProperty("KINTONE_OAUTH_PKCE");
  });

  it("複数の誤りは 1 回ですべて報告する(順序は ENV_NAMES の順)", () => {
    const got = errors({
      KINTONE_MCP_WORKER_NAME: "Bad Name",
      KINTONE_MCP_HOST: "<sub>.cybozu.com",
      KINTONE_MCP_ACCESS_TEAM_DOMAIN: "acme.cloudflareaccess.com",
    });
    expect(got).toEqual([
      { name: "KINTONE_MCP_WORKER_NAME", reason: "invalid" },
      { name: "KINTONE_MCP_KV_NAMESPACE_ID", reason: "missing" },
      { name: "KINTONE_MCP_HOST", reason: "placeholder" },
      { name: "KINTONE_MCP_PUBLIC_BASE_URL", reason: "missing" },
      { name: "KINTONE_MCP_ACCESS_AUD", reason: "missing" },
    ]);
  });

  it("wrangler.jsonc が読めない・オブジェクトでなければ wrangler.jsonc の invalid", () => {
    for (const base of ["{ not json", "[]", "42", ""]) {
      expect(errors(VALID, base)).toEqual([{ name: "wrangler.jsonc", reason: "invalid" }]);
    }
  });
});

describe("値を出さない", () => {
  const SECRET = "SECRET-LOOKING-VALUE";

  it("エラーにも整形結果にも、入力値が現れない", () => {
    const env = {
      KINTONE_MCP_WORKER_NAME: `Bad ${SECRET}`,
      KINTONE_MCP_KV_NAMESPACE_ID: `zz-${SECRET}`,
      KINTONE_MCP_HOST: `https://${SECRET}.example.com/path`,
      KINTONE_MCP_PUBLIC_BASE_URL: `http://<${SECRET}>`,
      KINTONE_MCP_ACCESS_TEAM_DOMAIN: `${SECRET}/x`,
      KINTONE_MCP_ACCESS_AUD: `has space ${SECRET}`,
      KINTONE_MCP_CUSTOM_DOMAIN: `<${SECRET}>`,
      KINTONE_MCP_OAUTH_PKCE: SECRET,
    };
    const got = errors(env);
    expect(got).toHaveLength(8);
    expect(JSON.stringify(got)).not.toContain(SECRET);
    const lines = formatErrors(got);
    expect(lines).toHaveLength(8);
    expect(lines.join("\n")).not.toContain(SECRET);
  });
});

describe("formatErrors", () => {
  it("項目名と理由だけの行にする", () => {
    expect(
      formatErrors([
        { name: "KINTONE_MCP_HOST", reason: "missing" },
        { name: "KINTONE_MCP_PUBLIC_BASE_URL", reason: "invalid" },
        { name: "KINTONE_MCP_ACCESS_AUD", reason: "placeholder" },
      ]),
    ).toEqual([
      "KINTONE_MCP_HOST: 未設定",
      "KINTONE_MCP_PUBLIC_BASE_URL: 形式が正しくない",
      "KINTONE_MCP_ACCESS_AUD: プレースホルダ(<...>)のまま",
    ]);
  });
});

describe("EXAMPLE_ENV(deploy:check 用のダミー)", () => {
  it("検証を通り、カスタムドメインのある設定になる", () => {
    const { config } = ok(EXAMPLE_ENV);
    expect(config.name).toBe("kintone-mcp-example");
    expect(config.workers_dev).toBe(false);
  });

  it("実在しないドメイン(example.com / example.org などの予約名)と全ゼロの KV id だけ", () => {
    expect(EXAMPLE_ENV.KINTONE_MCP_KV_NAMESPACE_ID).toBe("00000000000000000000000000000000");
    expect(EXAMPLE_ENV.KINTONE_MCP_HOST).toBe("example.cybozu.com");
    for (const name of [
      "KINTONE_MCP_PUBLIC_BASE_URL",
      "KINTONE_MCP_CUSTOM_DOMAIN",
      "KINTONE_MCP_ACCESS_TEAM_DOMAIN",
    ]) {
      expect(EXAMPLE_ENV[name]).toMatch(/example/);
    }
  });
});

describe("parseJsonc", () => {
  it("コメントと末尾カンマを読む", () => {
    expect(parseJsonc(`{ /* a */ "a": 1, // b\n "b": [1, 2,], }`)).toEqual({ a: 1, b: [1, 2] });
  });

  it("文字列の中の // や /* や , は壊さない(エスケープされた引用符を含む)", () => {
    expect(parseJsonc(`{ "u": "https://x.example/a//b", "c": "/* x */", "q": "a\\"//b,}" }`)).toEqual(
      { u: "https://x.example/a//b", c: "/* x */", q: 'a"//b,}' },
    );
  });

  it("壊れた JSON は例外", () => {
    expect(() => parseJsonc("{")).toThrow();
  });
});
