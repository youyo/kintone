/**
 * 下流向けセットアップスキル(.claude/skills/kintone-mcp-setup)の存在とリンク、
 * および参照している mise タスク名・環境変数名が実在することの検査。ファイルを読むだけ。
 * Node 環境のプロジェクト(`node:fs` を使うため)。
 */
import { lstatSync, readFileSync, readlinkSync, realpathSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { ENV_NAMES } from "../../scripts/render-wrangler-config/core";

const ROOT = resolve(import.meta.dirname, "../..");
const read = (path: string) => readFileSync(join(ROOT, path), "utf8");

const SKILL_DIR = ".claude/skills/kintone-mcp-setup";
const SKILL_PATH = `${SKILL_DIR}/SKILL.md`;
const LINK_PATH = ".agents/skills/kintone-mcp-setup";

const skill = read(SKILL_PATH);
const mise = read("mise.toml");
const deployYml = read(".github/workflows/deploy.yml");

/** frontmatter(先頭の `---` で囲まれた範囲)の `key: value` を読む。 */
function frontmatter(text: string): Record<string, string> {
  const m = /^---\n([\s\S]*?)\n---\n/.exec(text);
  if (!m) throw new Error("frontmatter がない");
  const out: Record<string, string> = {};
  for (const line of (m[1] as string).split("\n")) {
    const kv = /^([a-z-]+):\s*(.*)$/.exec(line);
    if (kv) out[kv[1] as string] = kv[2] as string;
  }
  return out;
}

describe("kintone-mcp-setup スキル", () => {
  it("frontmatter に name と description がある", () => {
    const fm = frontmatter(skill);
    expect(fm.name).toBe("kintone-mcp-setup");
    expect(fm.description?.length ?? 0).toBeGreaterThan(50);
    expect(fm.description).toContain("TRIGGER when:");
    expect(fm.description).toContain("DO NOT TRIGGER when:");
  });

  it(".agents/skills/kintone-mcp-setup が .claude/skills/kintone-mcp-setup への相対シンボリックリンク", () => {
    expect(lstatSync(join(ROOT, LINK_PATH)).isSymbolicLink()).toBe(true);
    expect(readlinkSync(join(ROOT, LINK_PATH))).toBe("../../.claude/skills/kintone-mcp-setup");
    expect(realpathSync(join(ROOT, LINK_PATH))).toBe(realpathSync(join(ROOT, SKILL_DIR)));
  });

  it("SKILL.md に出てくる mise タスク名が mise.toml に実在する", () => {
    const tasks = [...skill.matchAll(/mise run ([a-z][a-z0-9:-]*)/g)].map((m) => m[1] as string);
    expect(tasks.length).toBeGreaterThan(0);
    for (const name of new Set(tasks)) {
      const header = new RegExp(`^\\[tasks\\.("?)${name.replace(":", "\\:")}\\1\\]\\s*$`, "m");
      expect(mise, `mise.toml に [tasks.${name}] がない`).toMatch(header);
    }
    for (const required of ["deploy:render", "deploy:check", "deploy", "verify:oauth"]) {
      expect(tasks, `${required} への言及がない`).toContain(required);
    }
  });

  it("SKILL.md の KINTONE_MCP_* は core.ts の ENV_NAMES と一致し、すべて載っている", () => {
    const known = new Set<string>(Object.values(ENV_NAMES));
    const used = new Set([...skill.matchAll(/KINTONE_MCP_[A-Z0-9_]+/g)].map((m) => m[0]));
    for (const name of used) {
      expect(known.has(name), `${name} は core.ts の ENV_NAMES にない`).toBe(true);
    }
    for (const name of known) {
      expect(used.has(name), `${name} が SKILL.md にない`).toBe(true);
    }
  });

  it("SKILL.md の Secrets 名が deploy.yml に実在する", () => {
    for (const name of [
      "KINTONE_CLIENT_ID",
      "KINTONE_CLIENT_SECRET",
      "CLOUDFLARE_API_TOKEN",
      "CLOUDFLARE_ACCOUNT_ID",
    ]) {
      expect(skill, `${name} が SKILL.md にない`).toContain(name);
      expect(deployYml, `${name} が deploy.yml にない`).toContain(name);
    }
  });

  it("CLAUDE.md / README.md / docs/runbook.md からスキルへ案内している", () => {
    for (const file of ["CLAUDE.md", "README.md", "README_ja.md", "docs/runbook.md"]) {
      expect(read(file), `${file} にスキルへの案内がない`).toContain("kintone-mcp-setup");
    }
  });
});

describe("kintone-mcp-setup スキル: Step 0-A 下流リポジトリの作成", () => {
  const section = (() => {
    const m = /^## Step 0-A[^\n]*\n([\s\S]*?)(?=^## )/m.exec(skill);
    return m ? (m[0] as string) : "";
  })();

  it("Step 0-A の節がある", () => {
    expect(section, "## Step 0-A の節がない").not.toBe("");
    expect(section).toContain("下流リポジトリの作成");
  });

  it("fork かミラーの選択、作成先の確認、承認、停止条件を持つ", () => {
    for (const s of [
      "fork",
      "ミラー",
      "OWNER/REPO",
      "private",
      "gh repo create",
      "承認",
      "停止",
      "upstream",
    ]) {
      expect(section, `Step 0-A に「${s}」がない`).toContain(s);
    }
  });

  it("手順の本体は runbook 手順 0 にあると案内している", () => {
    expect(section).toContain("runbook 手順 0");
  });

  it("新しいセッションで最初に送る依頼文の例がコードブロックである", () => {
    const blocks = [...section.matchAll(/```[^\n]*\n([\s\S]*?)```/g)].map((m) => m[1] as string);
    expect(blocks.some((b) => b.includes("kintone-mcp-setup") && b.includes("Step 0"))).toBe(true);
    expect(blocks.some((b) => b.includes(".claude/skills/kintone-mcp-setup/SKILL.md"))).toBe(true);
  });

  it("Step 0 の upstream 判定は停止ではなく Step 0-A へ進む", () => {
    expect(skill).toMatch(/Step 0-A[^\n]*へ進む/);
  });

  it("Step 0 の目的の行(欠けたら止まる)が、Step 0-A へ進む例外に触れている", () => {
    const purpose = skill.split("\n").find((l) => l.startsWith("目的: ここが下流で")) ?? "";
    expect(purpose, "Step 0 の目的の行がない").not.toBe("");
    expect(purpose).toContain("Step 0-A");
  });

  it("description に下流リポジトリ作成のトリガー例がある", () => {
    const d = frontmatter(skill).description ?? "";
    for (const s of ["下流リポジトリを作りたい", "ミラーしたい", "fork して始めたい"]) {
      expect(d, `description に「${s}」がない`).toContain(s);
    }
  });

  it("完了報告のフォーマットに Step 0-A の行がある", () => {
    expect(skill).toMatch(/^- Step 0-A 下流リポジトリの作成: <fork\/ミラー\/既存>/m);
  });

  it("runbook 手順 0 からスキルへの導線がある", () => {
    const runbook = read("docs/runbook.md");
    const m = /^## 0\. [^\n]*\n([\s\S]*?)(?=^## )/m.exec(runbook);
    expect(m, "runbook に手順 0 の節がない").not.toBeNull();
    expect(m?.[0]).toContain("kintone-mcp-setup");
  });
});
