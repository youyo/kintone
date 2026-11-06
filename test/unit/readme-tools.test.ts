import { describe, expect, it } from "vitest";
import readmeEn from "../../README.md?raw";
import readmeJa from "../../README_ja.md?raw";
import { ALL_TOOLS } from "../../src/mcp/tools";

/** README(英語版 README.md と日本語版 README_ja.md)のツール一覧が、実際に登録されているツールと一致していること。 */
const READMES = [
  { file: "README.md", readme: readmeEn, total: `${ALL_TOOLS.length} tools` },
  { file: "README_ja.md", readme: readmeJa, total: `${ALL_TOOLS.length} 個のツール` },
];

describe.each(READMES)("$file のツール一覧", ({ file, readme, total }) => {
  const byToolset = new Map<string, string[]>();
  for (const tool of ALL_TOOLS) {
    const list = byToolset.get(tool.toolset) ?? [];
    list.push(tool.name);
    byToolset.set(tool.toolset, list);
  }
  const lines = readme.split("\n");
  const toolsetRows = lines.filter((l) => /^\| `[a-z_]+` \| \d+ \|/.test(l));

  it("総数が本文に書かれている", () => {
    expect(readme).toContain(total);
  });

  it.each([...byToolset])("toolset %s の行に、件数と全ツール名がある", (toolset, names) => {
    const row = lines.find((l) => l.startsWith(`| \`${toolset}\` |`));
    expect(row, `${file} に toolset ${toolset} の行がない`).toBeDefined();
    const cells = (row ?? "").split("|").map((c) => c.trim());
    expect(cells[2]).toBe(String(names.length));
    for (const name of names) expect(row).toContain(`\`${name}\``);
  });

  it("toolset の表に、実在しない toolset・ツール名がない", () => {
    const real = new Set(ALL_TOOLS.map((t) => t.name));
    expect(toolsetRows.length).toBe(byToolset.size);
    for (const row of toolsetRows) {
      const cells = row.split("|");
      expect(byToolset.has((cells[1] ?? "").trim().replaceAll("`", ""))).toBe(true);
      for (const m of (cells[3] ?? "").matchAll(/`([a-z_0-9]+)`/g)) {
        expect(real.has(m[1] as string), `${file} の ${m[1]} は登録されていない`).toBe(true);
      }
    }
  });

  it("ツールごとの表に、全ツールが区分(annotation の 4 区分)つきで 1 行ずつある", () => {
    for (const tool of ALL_TOOLS) {
      const rows = lines.filter((l) => l.startsWith(`| \`${tool.name}\` |`));
      expect(rows, `${file} にツール ${tool.name} の行が 1 つない`).toHaveLength(1);
      expect(rows[0]).toContain(tool.category);
    }
  });

  it("ツールごとの表の `kintone_` で始まるツール名は、実在するものだけ", () => {
    const real = new Set(ALL_TOOLS.map((t) => t.name));
    const rows = lines.filter((l) => /^\| `kintone_[a-z_0-9]+` \|/.test(l));
    expect(rows).toHaveLength(ALL_TOOLS.length);
    for (const row of rows) {
      const name = /^\| `([^`]+)`/.exec(row)?.[1] ?? "";
      expect(real.has(name), `${file} の ${name} は登録されていない`).toBe(true);
    }
  });
});
