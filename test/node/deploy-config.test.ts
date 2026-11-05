/**
 * デプロイ関連の設定ファイル・文書の不変条件(M05)。ファイルを読むだけで、何も実行しない。
 * Node 環境のプロジェクト(`node:fs` を使うため)。
 */
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { ENV_NAMES, EXAMPLE_ENV, renderWranglerConfig } from "../../scripts/render-wrangler-config/core";

const ROOT = resolve(import.meta.dirname, "../..");
const read = (path: string) => readFileSync(join(ROOT, path), "utf8");

const ci = read(".github/workflows/ci.yml");
const mise = read("mise.toml");

/** mise.toml の `[tasks.<name>]` の本文(次の `[` の行まで)。 */
function taskBlock(name: string): string {
  const header = new RegExp(`^\\[tasks\\.("?)${name.replace(":", "\\:")}\\1\\]\\s*$`, "m");
  const m = header.exec(mise);
  if (!m) throw new Error(`mise.toml に [tasks.${name}] がない`);
  const rest = mise.slice(m.index + m[0].length);
  const next = rest.search(/^\[/m);
  return next === -1 ? rest : rest.slice(0, next);
}

describe("mise.toml", () => {
  it("deploy:render / deploy:check / deploy が定義されている", () => {
    expect(taskBlock("deploy:render")).toContain("node scripts/render-wrangler-config/main.ts");
    const check = taskBlock("deploy:check");
    expect(check).toContain("node scripts/render-wrangler-config/main.ts --example");
    expect(check).toContain(
      "wrangler deploy --dry-run -c wrangler.deploy.example.jsonc --outdir dist",
    );
    const deploy = taskBlock("deploy");
    expect(deploy).toContain("node scripts/render-wrangler-config/main.ts");
    expect(deploy).toContain("wrangler deploy -c wrangler.deploy.jsonc");
    expect(deploy).not.toContain("--dry-run");
  });

  it("deploy:check は実際のデプロイ(--dry-run なしの wrangler deploy)も secret の登録もしない", () => {
    const check = taskBlock("deploy:check");
    expect(check).not.toMatch(/wrangler secret/);
    for (const line of check.split("\n").filter((l) => l.includes("wrangler deploy"))) {
      expect(line).toContain("--dry-run");
    }
  });

  it("test:e2e は node-e2e プロジェクトを含む", () => {
    const e2e = taskBlock("test:e2e");
    expect(e2e).toContain("--project e2e");
    expect(e2e).toContain("--project node-e2e");
  });
});

describe(".gitignore", () => {
  const ignore = read(".gitignore").split("\n");

  it("実用の生成物(wrangler.deploy*.jsonc)は追跡しない。ダミー値の example だけ追跡する", () => {
    expect(ignore).toContain("wrangler.deploy*.jsonc");
    expect(ignore).toContain("!wrangler.deploy.example.jsonc");
    expect(ignore.indexOf("!wrangler.deploy.example.jsonc")).toBeGreaterThan(
      ignore.indexOf("wrangler.deploy*.jsonc"),
    );
  });

  it("mise.local.toml(手元の値)と .dev.vars は追跡しない", () => {
    expect(ignore).toContain("mise.local.toml");
    expect(ignore).toContain(".dev.vars");
  });
});

describe("wrangler.deploy.example.jsonc", () => {
  it("--example の生成物(wrangler.jsonc + EXAMPLE_ENV)と一致する(ダミー値だけ)", () => {
    const rendered = renderWranglerConfig(read("wrangler.jsonc"), EXAMPLE_ENV);
    if (!rendered.ok) throw new Error("EXAMPLE_ENV が検証を通らない");
    expect(read("wrangler.deploy.example.jsonc")).toBe(rendered.text);
  });
});

describe("GitHub Actions", () => {
  const workflows = {
    "ci.yml": ci,
    "deploy.yml": read(".github/workflows/deploy.yml"),
  };

  /** `uses: <action>@<ref> # <tag>` の行。 */
  function usesOf(text: string): { action: string; ref: string; line: string }[] {
    return text
      .split("\n")
      .filter((l) => /^\s*-?\s*uses:/.test(l))
      .map((line) => {
        const m = /uses:\s*(\S+)@(\S+)/.exec(line);
        return { action: m?.[1] ?? "", ref: m?.[2] ?? "", line };
      });
  }

  it.each(Object.entries(workflows))("%s: すべてのアクションがコミット SHA で固定されている", (_n, text) => {
    const uses = usesOf(text);
    expect(uses.length).toBeGreaterThan(0);
    for (const u of uses) {
      expect(u.ref, `${u.action} が SHA 固定でない`).toMatch(/^[0-9a-f]{40}$/);
      expect(u.line, `${u.action} のタグのコメントがない`).toMatch(/# v\d/);
    }
  });

  it("deploy.yml は ci.yml と同じアクション・同じ SHA だけを使う(新しいアクションを足さない)", () => {
    const ciUses = new Map(usesOf(ci).map((u) => [u.action, u.ref]));
    for (const u of usesOf(workflows["deploy.yml"])) {
      expect(ciUses.get(u.action), `${u.action} が ci.yml にない / SHA が違う`).toBe(u.ref);
    }
  });

  it("ci.yml が deploy:check を実行する", () => {
    expect(ci).toMatch(/^\s*- run: mise run deploy:check\s*$/m);
  });

  const deploy = workflows["deploy.yml"];

  it("deploy.yml は Worker 名の Variable が空のとき(upstream)ジョブを飛ばす", () => {
    expect(deploy).toContain("if: ${{ vars.KINTONE_MCP_WORKER_NAME != '' }}");
  });

  it("deploy.yml は main への push と手動実行で動き、権限は contents: read だけ", () => {
    expect(deploy).toMatch(/^on:\n {2}push:\n {4}branches: \[main\]\n {2}workflow_dispatch:/m);
    expect(deploy).toMatch(/^permissions:\n {2}contents: read\n/m);
  });

  it("deploy.yml は secrets をジョブ全体の env に置かない(ステップの env だけ)", () => {
    // ワークフロー直下・ジョブ直下の env: は禁止。
    expect(deploy).not.toMatch(/^env:/m);
    expect(deploy).not.toMatch(/^ {4}env:/m);
    // secrets の参照はステップの env(インデント 10 以上)の中だけ。
    const refs = deploy.split("\n").filter((l) => l.includes("secrets."));
    expect(refs.length).toBeGreaterThan(0);
    for (const line of refs) expect(line).toMatch(/^ {10,}[A-Z_]+: \$\{\{ secrets\.[A-Z_]+ \}\}$/);
  });

  it("deploy.yml の流れ: lint → test → deploy:render(Variables)→ --secrets-file でデプロイ", () => {
    const order = [
      "mise run lint",
      "mise run test",
      "mise run deploy:render",
      "wrangler deploy -c wrangler.deploy.jsonc --secrets-file",
    ].map((s) => deploy.indexOf(s));
    for (const i of order) expect(i).toBeGreaterThan(-1);
    expect([...order].sort((a, b) => a - b)).toEqual(order);
    for (const name of Object.values(ENV_NAMES)) {
      expect(deploy).toContain(`${name}: \${{ vars.${name} }}`);
    }
    for (const name of ["KINTONE_CLIENT_ID", "KINTONE_CLIENT_SECRET"]) {
      expect(deploy).toContain(`${name}: \${{ secrets.${name} }}`);
    }
    for (const name of ["CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID"]) {
      expect(deploy).toContain(`${name}: \${{ secrets.${name} }}`);
    }
  });

  it("deploy.yml は secrets-file を自分だけが読める権限で作り、終了時に消す。wrangler secret put は使わない", () => {
    expect(deploy).toContain("umask 077");
    expect(deploy).toMatch(/trap 'rm -f "\$secrets_file"' EXIT/);
    expect(deploy).not.toContain("secret put");
    expect(deploy).not.toMatch(/echo .*secrets\./);
  });
});

describe("docs/runbook.md", () => {
  const runbook = read("docs/runbook.md");
  const headings = runbook.split("\n").filter((l) => /^#{1,3} /.test(l));

  it.each([
    ["設定値の対応表", /設定値の対応表/],
    ["下流リポジトリの作り方", /下流リポジトリ/],
    ["OAuth クライアントの登録", /OAuth クライアント/],
    ["Access の self-hosted アプリ", /Access.*self-hosted|self-hosted.*Access/],
    ["Access から外した利用者の grant の失効", /失効/],
    ["KV の作成", /KV/],
    ["Variables / Secrets", /Variables.*Secrets/],
    ["初回デプロイと動作確認", /動作確認/],
    ["WAF / Rate Limiting", /WAF.*Rate Limiting/],
    ["IP 制限", /IP 制限/],
    ["障害の切り分け", /切り分け/],
  ])("見出しに「%s」の節がある", (_label, re) => {
    expect(headings.some((h) => re.test(h)), `見出しがない: ${re}`).toBe(true);
  });

  it("生成する環境変数(KINTONE_MCP_*)と Secrets の名前がすべて載っている", () => {
    for (const name of Object.values(ENV_NAMES)) expect(runbook).toContain(name);
    for (const name of [
      "KINTONE_CLIENT_ID",
      "KINTONE_CLIENT_SECRET",
      "CLOUDFLARE_API_TOKEN",
      "CLOUDFLARE_ACCOUNT_ID",
    ]) {
      expect(runbook).toContain(name);
    }
  });

  it("OAuth・Access・動作確認の要点が載っている", () => {
    for (const s of [
      "<PUBLIC_BASE_URL>/callback",
      "k:app_record:read",
      "k:app_record:write",
      "k:app_settings:read",
      "k:app_settings:write",
      "k:file:read",
      "k:file:write",
      "STEP2",
      "/authorize",
      "/callback",
      "AUD",
      "cloudflareaccess.com",
      "Managed OAuth",
      "/healthz",
      "/.well-known/oauth-protected-resource/mcp",
      "mise run verify:oauth",
      "server_misconfigured",
      "redirect_uri",
      "kv namespace create",
      "wrangler kv key",
    ]) {
      expect(runbook, `runbook に「${s}」がない`).toContain(s);
    }
    // Access で保護しないパスを明記する。
    for (const path of ["/mcp", "/token", "/register", "/.well-known/*", "/healthz"]) {
      expect(runbook).toContain(path);
    }
  });

  it("実機で確かめていない手順は【未検証】と付け、manual-verification.md の実在する V / P 番号に結ぶ", () => {
    const manual = read("docs/manual-verification.md");
    const defined = new Set([...manual.matchAll(/^### ([VP]\d+)\b/gm)].map((m) => m[1]));
    const flagged = runbook.split("\n").filter((l) => l.includes("【未検証】"));
    expect(flagged.length).toBeGreaterThan(5);
    for (const line of flagged) {
      const ids = [...line.matchAll(/\b([VP]\d{2})\b/g)].map((m) => m[1] as string);
      expect(ids.length, `【未検証】の行に V / P 番号がない: ${line}`).toBeGreaterThan(0);
      for (const id of ids) expect(defined.has(id), `${id} が manual-verification.md にない`).toBe(true);
    }
  });

  it("アカウント固有らしい値(32 桁の 16 進、実在しそうなドメイン)を書いていない", () => {
    expect(runbook).not.toMatch(/\b[0-9a-f]{32}\b/);
    for (const host of runbook.matchAll(
      /[a-z0-9-]+\.(?:cybozu|cloudflareaccess|workers)\.(?:com|dev)/g,
    )) {
      expect(host[0], `${host[0]} は例示用の名前(example で始まる)でない`).toMatch(/^example/);
    }
  });
});

describe("README.md", () => {
  const readme = read("README.md");

  it("商標の注記がある(kintone はサイボウズ株式会社の商標で、本プロジェクトは無関係)", () => {
    expect(readme).toMatch(/kintone[^\n]*サイボウズ株式会社の(登録)?商標/);
    expect(readme).toMatch(/無関係/);
  });

  it("Go 版の利用者への移行案内: 廃止・保存先・破壊的変更の一覧", () => {
    for (const s of [
      "Go 版",
      "CLI",
      "archive/go",
      "v0.5.6",
      "Homebrew",
      "ghcr",
      "破壊的変更",
      "kintone_",
      "isError",
      "小文字",
      "API トークン",
      "Cloudflare Access",
      "KINTONE_HOST",
    ]) {
      expect(readme, `README に「${s}」がない`).toContain(s);
    }
  });

  it("runbook・仕様書・ADR への導線がある", () => {
    for (const s of ["docs/runbook.md", "docs/specs/kintone-mcp-spec.md", "docs/adr/"]) {
      expect(readme).toContain(s);
    }
  });
});

describe("docs/manual-verification.md", () => {
  const manual = read("docs/manual-verification.md");

  it("DELETE /k/v1/records.json を JSON 本文で送る形を実機で確かめる独立の項目がある(M04 の指摘)", () => {
    const sections = manual.split(/^(?=### )/m);
    const item = sections.find(
      (s) => /^### V\d+/.test(s) && /DELETE \/k\/v1\/records\.json/.test(s.split("\n")[0] ?? ""),
    );
    expect(item, "見出しに DELETE /k/v1/records.json を持つ V 項目がない").toBeDefined();
    expect(item).toMatch(/JSON 本文/);
    expect(item).toMatch(/Content-Type: application\/json/);
    expect(item).toMatch(/クエリ文字列/);
  });
});
