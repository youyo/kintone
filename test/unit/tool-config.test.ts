import { describe, expect, it } from "vitest";
import { z } from "zod";
import {
  defineTool,
  parseToolConfig,
  QueryError,
  selectTools,
  type ToolCategory,
  type Toolset,
} from "../../src/mcp/registry";

const tool = (name: string, category: ToolCategory, toolset: Toolset) =>
  defineTool({
    name,
    title: "テスト",
    description: "test",
    category,
    toolset,
    inputSchema: z.strictObject({}),
    handler: async () => ({}),
  });

/** 4 区分 × 2 toolset を一通り持つ合成の一覧(実際の ALL_TOOLS には依存しない)。 */
const TOOLS = [
  tool("kintone_apps_list", "ReadOnly", "app"),
  tool("kintone_records_list", "ReadOnly", "record"),
  tool("kintone_record_create", "WriteNonIdempotent", "record"),
  tool("kintone_record_update", "WriteIdempotent", "record"),
  tool("kintone_record_delete", "Destructive", "record"),
];

const parse = (query: string, tools = TOOLS) => parseToolConfig(new URLSearchParams(query), tools);
const names = (query: string, tools = TOOLS) =>
  selectTools(tools, parse(query, tools)).map((t) => t.name);

describe("parseToolConfig: read_only", () => {
  it("指定なしは全ツール(toolsets=all、read_only=false)", () => {
    expect(parse("")).toEqual({ toolsets: "all", readOnly: false });
    expect(names("")).toHaveLength(5);
  });

  it("read_only=true で ReadOnly 以外(書き込み・破壊的)を隠す", () => {
    expect(parse("read_only=true").readOnly).toBe(true);
    expect(names("read_only=true")).toEqual(["kintone_apps_list", "kintone_records_list"]);
  });

  it("read_only=false・空・大文字・前後の空白を受け付ける", () => {
    expect(parse("read_only=false").readOnly).toBe(false);
    expect(parse("read_only=").readOnly).toBe(false);
    expect(parse("read_only=TRUE").readOnly).toBe(true);
    expect(parse("read_only=%20true%20").readOnly).toBe(true);
  });

  it.each(["maybe", "1", "yes", "on", "0"])("read_only=%s は QueryError", (value) => {
    expect(() => parse(`read_only=${value}`)).toThrow(QueryError);
    expect(() => parse(`read_only=${value}`)).toThrow(/read_only/);
  });

  it("read_only を複数回指定すると QueryError", () => {
    expect(() => parse("read_only=true&read_only=false")).toThrow(QueryError);
  });
});

describe("parseToolConfig: toolsets", () => {
  it("toolsets=app / record で絞る", () => {
    expect(names("toolsets=app")).toEqual(["kintone_apps_list"]);
    expect(names("toolsets=record")).toEqual([
      "kintone_records_list",
      "kintone_record_create",
      "kintone_record_update",
      "kintone_record_delete",
    ]);
  });

  it("カンマ区切り・複数回指定・大文字小文字・空白を受け付ける", () => {
    expect(names("toolsets=app,record")).toHaveLength(5);
    expect(names("toolsets=app&toolsets=record")).toHaveLength(5);
    expect(names("toolsets=%20APP%20,%20Record")).toHaveLength(5);
  });

  it("all を含む・空なら全部", () => {
    expect(parse("toolsets=all").toolsets).toBe("all");
    expect(parse("toolsets=app,all").toolsets).toBe("all");
    expect(parse("toolsets=").toolsets).toBe("all");
  });

  it("未知の toolset は QueryError(有効な名前を案内する)", () => {
    expect(() => parse("toolsets=nope")).toThrow(QueryError);
    expect(() => parse("toolsets=app,nope")).toThrow(/nope/);
    expect(() => parse("toolsets=issue")).toThrow(/app.*record|record.*app/);
  });

  it("toolsets と read_only の組み合わせ", () => {
    expect(names("toolsets=record&read_only=true")).toEqual(["kintone_records_list"]);
  });
});

describe("parseToolConfig: 綴り違いのキーと無関係なキー", () => {
  it.each([
    "readonly",
    "readOnly",
    "read-only",
    "READ_ONLY",
    "readonlly",
    "readOnlyMode",
    "toolset",
    "tool_sets",
    "toolSets",
    "tools",
  ])("綴り違い %s は QueryError(読み取り専用のつもりで書き込めてしまうのを防ぐ)", (key) => {
    expect(() => parse(`${key}=true`)).toThrow(QueryError);
  });

  it("正しいキーを案内する", () => {
    expect(() => parse("readonly=true")).toThrow(/read_only/);
    expect(() => parse("toolset=app")).toThrow(/toolsets/);
  });

  it.each(["x", "optimize_context", "mode", "ro", "foo"])("無関係なキー %s は無視する", (key) => {
    expect(parse(`${key}=1`)).toEqual({ toolsets: "all", readOnly: false });
  });
});

describe("parseToolConfig: 公開ツールが 0 個になる組み合わせ", () => {
  it("書き込みしかない toolset を read_only=true にすると QueryError", () => {
    const tools = [
      tool("kintone_apps_list", "ReadOnly", "app"),
      tool("kintone_record_create", "WriteNonIdempotent", "record"),
      tool("kintone_record_delete", "Destructive", "record"),
    ];
    expect(() => parse("toolsets=record&read_only=true", tools)).toThrow(QueryError);
    expect(() => parse("toolsets=record&read_only=true", tools)).toThrow(
      /公開されるツールがありません/,
    );
  });

  it("toolset にツールがない(M02 の実ツールのように record が空)と QueryError", () => {
    const tools = [tool("kintone_apps_list", "ReadOnly", "app")];
    expect(() => parse("toolsets=record", tools)).toThrow(QueryError);
    expect(names("toolsets=app", tools)).toEqual(["kintone_apps_list"]);
  });
});

describe("selectTools", () => {
  it("toolset × read_only の全組み合わせ", () => {
    const sel = (toolsets: "all" | Toolset[], readOnly: boolean) =>
      selectTools(TOOLS, {
        toolsets: toolsets === "all" ? "all" : new Set(toolsets),
        readOnly,
      }).map((t) => t.category);
    expect(sel("all", false)).toEqual([
      "ReadOnly",
      "ReadOnly",
      "WriteNonIdempotent",
      "WriteIdempotent",
      "Destructive",
    ]);
    expect(sel("all", true)).toEqual(["ReadOnly", "ReadOnly"]);
    expect(sel(["app"], false)).toEqual(["ReadOnly"]);
    expect(sel(["app"], true)).toEqual(["ReadOnly"]);
    expect(sel(["record"], true)).toEqual(["ReadOnly"]);
    expect(sel(["record"], false)).toHaveLength(4);
  });
});
