/**
 * tools/list のスナップショット。ツール名・description・引数(JSON Schema)・annotation の変更を、
 * `test/snapshots/tools-list.json` の差分としてレビューできるようにする(引数名の退行を防ぐ。ADR 0009)。
 * 意図した変更のときだけ `mise run test:update-snapshots` で更新し、差分を確認してからコミットする。
 */
import { describe, expect, it } from "vitest";
import type { Toolset } from "../../src/mcp/registry";
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

  it("M03 の 3 つの読み取りツールが公開される(app_describe と records_query は app / app_ref を持つ)", async () => {
    const tools = await listTools();
    expect(tools.map((t) => t.name)).toEqual([
      "kintone_apps_search",
      "kintone_app_describe",
      "kintone_records_query",
    ]);
    const props = (name: string) =>
      (tools.find((t) => t.name === name)?.inputSchema as { properties: Record<string, any> })
        .properties;
    expect(Object.keys(props("kintone_app_describe"))).toEqual([
      "app",
      "app_ref",
      "lang",
      "max_response_tokens",
    ]);
    expect(Object.keys(props("kintone_records_query"))).toEqual([
      "app",
      "app_ref",
      "query",
      "fields",
      "total_count",
      "max_response_tokens",
    ]);
    // app / app_ref の排他(どちらか 1 つ)が説明から読める。
    for (const name of ["kintone_app_describe", "kintone_records_query"]) {
      expect(props(name).app?.description, name).toMatch(/exactly one|either/i);
      expect(props(name).app_ref?.description, name).toMatch(/exactly one|either/i);
    }
  });

  it("toolset: app = apps_search + app_describe、record = records_query", async () => {
    const names = async (toolset: Toolset) => {
      const { result } = await rpcTo(
        () =>
          createMcpServer({
            ...deps,
            config: { toolsets: new Set<Toolset>([toolset]), readOnly: false },
          }),
        "tools/list",
      );
      return (result?.tools ?? []).map((t) => t.name);
    };
    expect(await names("app")).toEqual(["kintone_apps_search", "kintone_app_describe"]);
    expect(await names("record")).toEqual(["kintone_records_query"]);
  });

  it("max_response_tokens の公開スキーマに minimum: 0 が出る(全ての読み取りツール)", async () => {
    for (const t of await listTools()) {
      const props = (t.inputSchema as { properties: Record<string, { minimum?: number }> })
        .properties;
      expect(props.max_response_tokens?.minimum, t.name).toBe(0);
    }
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
