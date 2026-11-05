/**
 * tools/list のスナップショット。ツール名・description・引数(JSON Schema)・annotation の変更を、
 * `test/snapshots/tools-list.json` の差分としてレビューできるようにする(引数名の退行を防ぐ。ADR 0009)。
 * 意図した変更のときだけ `mise run test:update-snapshots` で更新し、差分を確認してからコミットする。
 */
import { describe, expect, it } from "vitest";
import { createMcpServer } from "../../src/mcp/server";
import { ALL_TOOLS } from "../../src/mcp/tools";
import { fakeKintoneClient } from "../helpers/fake-kintone-client";
import { rpcTo } from "../helpers/mcp";

const deps = { getClient: () => fakeKintoneClient(), onKintoneUnauthorized: () => {} };

async function listTools(config?: { toolsets: "all"; readOnly: boolean }) {
  const { result } = await rpcTo(
    () => createMcpServer({ ...deps, ...(config ? { config } : {}) }),
    "tools/list",
  );
  return result?.tools ?? [];
}

describe("tools/list", () => {
  it("スナップショットと一致する(引数名・description・annotation)", async () => {
    const tools = await listTools();
    await expect(`${JSON.stringify(tools, null, 2)}\n`).toMatchFileSnapshot(
      "../snapshots/tools-list.json",
    );
  });

  it("ALL_TOOLS の全ツールが公開され、名前は kintone_ で始まり重複しない", async () => {
    const names = (await listTools()).map((t) => t.name);
    expect(names).toEqual(ALL_TOOLS.map((t) => t.name));
    expect(new Set(names).size).toBe(names.length);
    for (const name of names) expect(name).toMatch(/^kintone_[a-z][a-z0-9]*(?:_[a-z0-9]+)+$/);
  });

  it("max_response_tokens は読み取り専用のツールだけが持つ(書き込み・破壊的は持たない)", async () => {
    for (const t of await listTools()) {
      const props = Object.keys((t.inputSchema as { properties: object }).properties);
      const readOnly = t.annotations?.readOnlyHint === true;
      expect(props.includes("max_response_tokens"), t.name).toBe(readOnly);
    }
  });

  it("全ツールが 4 区分のどれかの annotation と日本語の title を持つ", async () => {
    for (const t of await listTools()) {
      const a = t.annotations as Record<string, unknown> | undefined;
      expect(typeof a?.title, t.name).toBe("string");
      expect(String(a?.title)).toMatch(/[\u3040-\u30ff\u4e00-\u9fff]/);
      for (const key of ["readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"]) {
        expect(typeof a?.[key], `${t.name}.${key}`).toBe("boolean");
      }
    }
  });

  it("read_only=true では読み取り専用のツールだけになる", async () => {
    const tools = await listTools({ toolsets: "all", readOnly: true });
    expect(tools.length).toBeGreaterThan(0);
    for (const t of tools) expect(t.annotations?.readOnlyHint, t.name).toBe(true);
  });
});
