import { McpServer } from "@modelcontextprotocol/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import { ResolverError } from "../../src/errors";
import { KintoneApiError, KintoneUnauthorizedError } from "../../src/kintone/client";
import {
  annotationsFor,
  defineTool,
  type RegisterDeps,
  registerTools,
  type ToolCategory,
  type ToolConfig,
  type ToolDefinition,
  type Toolset,
} from "../../src/mcp/registry";
import { estimateTokens } from "../../src/mcp/response-limit";
import { fakeKintoneClient } from "../helpers/fake-kintone-client";
import { rpcTo } from "../helpers/mcp";
import { firstText } from "../helpers/oauth-flow";

// 文字列を分けて書く(エディタ・ツールがトークンらしい文字列を伏せるため)。
const BEARER = ["Bear", "er"].join("");
const ALL: ToolConfig = { toolsets: "all", readOnly: false };

afterEach(() => vi.restoreAllMocks());

function simpleTool(
  name: string,
  category: ToolCategory,
  toolset: Toolset,
  handler: ToolDefinition["handler"] = async () => ({ ok: true }),
  extra: { maxApiCalls?: number; responseLimit?: boolean } = {},
) {
  return defineTool({
    name,
    title: "テスト",
    description: `desc of ${name}`,
    category,
    toolset,
    inputSchema: z.strictObject({ n: z.number().int().optional() }),
    handler: (args, ctx) => handler(args, ctx),
    ...extra,
  });
}

interface Setup {
  getClientCalls: ({ maxApiCalls?: number } | undefined)[];
  unauthorized: number;
  factory: () => McpServer;
}

function setup(
  tools: ToolDefinition[],
  config: ToolConfig = ALL,
  deps: Partial<RegisterDeps> = {},
) {
  const s: Setup = {
    getClientCalls: [],
    unauthorized: 0,
    factory: () => {
      const server = new McpServer({ name: "test", version: "0" });
      registerTools(server, tools, config, {
        getClient: (options) => {
          s.getClientCalls.push(options);
          return fakeKintoneClient({ redact: (t) => t.split("kt-secret").join("[REDACTED]") });
        },
        onKintoneUnauthorized: () => {
          s.unauthorized++;
        },
        ...deps,
      });
      return server;
    },
  };
  return s;
}

const call = (s: Setup, name: string, args: Record<string, unknown> = {}) =>
  rpcTo(s.factory, "tools/call", { name, arguments: args });

describe("annotationsFor(4 区分)", () => {
  it.each([
    [
      "ReadOnly",
      { readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: true },
    ],
    [
      "WriteIdempotent",
      { readOnlyHint: false, destructiveHint: false, idempotentHint: true, openWorldHint: true },
    ],
    [
      "WriteNonIdempotent",
      { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: true },
    ],
    [
      "Destructive",
      { readOnlyHint: false, destructiveHint: true, idempotentHint: true, openWorldHint: true },
    ],
  ] as const)("%s", (category, hints) => {
    expect(annotationsFor(category, "タイトル")).toEqual({ title: "タイトル", ...hints });
  });
});

describe("defineTool", () => {
  it.each([
    "apps_search",
    "kintone_search",
    "kintone_",
    "Kintone_apps_search",
    "kintone_Apps_search",
    "kintone_apps-search",
    "kintone__apps",
    "logbutler_issue_get",
  ])("名前が kintone_<resource>_<action> でなければ例外: %s", (name) => {
    expect(() => simpleTool(name, "ReadOnly", "app")).toThrow(/kintone_<resource>_<action>/);
  });

  it.each([
    "kintone_apps_search",
    "kintone_app_describe",
    "kintone_records_query",
    "kintone_record_create",
  ])("正しい名前は通る: %s", (name) => {
    expect(simpleTool(name, "ReadOnly", "app").name).toBe(name);
  });

  it("annotation は区分から決まり、title は日本語のまま", () => {
    const t = simpleTool("kintone_record_delete", "Destructive", "record");
    expect(t.annotations).toEqual(annotationsFor("Destructive", "テスト"));
  });

  it("ReadOnly には max_response_tokens が自動で付く。書き込み・破壊的には付かない", () => {
    const keys = (t: ToolDefinition) => Object.keys(t.inputSchema.shape);
    expect(keys(simpleTool("kintone_apps_list", "ReadOnly", "app"))).toEqual([
      "n",
      "max_response_tokens",
    ]);
    for (const category of ["WriteIdempotent", "WriteNonIdempotent", "Destructive"] as const) {
      expect(keys(simpleTool("kintone_record_write", category, "record"))).toEqual(["n"]);
    }
  });

  it("responseLimit: false で ReadOnly でも付けない。書き込みに responseLimit: true を渡しても付かない", () => {
    const off = simpleTool("kintone_apps_list", "ReadOnly", "app", undefined, {
      responseLimit: false,
    });
    expect(off.responseLimit).toBe(false);
    expect(Object.keys(off.inputSchema.shape)).toEqual(["n"]);
    const write = simpleTool("kintone_record_create", "WriteNonIdempotent", "record", undefined, {
      responseLimit: true,
    });
    expect(write.responseLimit).toBe(false);
  });

  it("スキーマは strict のまま(未知の引数を通さない)", () => {
    const t = simpleTool("kintone_apps_list", "ReadOnly", "app");
    expect(t.inputSchema.safeParse({ n: 1, max_response_tokens: 5 }).success).toBe(true);
    expect(t.inputSchema.safeParse({ other: 1 }).success).toBe(false);
  });
});

describe("registerTools: tools/list", () => {
  const TOOLS = [
    simpleTool("kintone_apps_list", "ReadOnly", "app"),
    simpleTool("kintone_record_create", "WriteNonIdempotent", "record"),
    simpleTool("kintone_record_update", "WriteIdempotent", "record"),
    simpleTool("kintone_record_delete", "Destructive", "record"),
  ];

  it("全ツールを annotation つきで公開し、max_response_tokens は ReadOnly だけが持つ", async () => {
    const { result } = await rpcTo(setup(TOOLS).factory, "tools/list");
    const tools = result?.tools ?? [];
    expect(tools.map((t) => t.name)).toEqual(TOOLS.map((t) => t.name));
    const byName = Object.fromEntries(tools.map((t) => [t.name, t]));
    expect(byName.kintone_apps_list?.annotations).toMatchObject({
      readOnlyHint: true,
      destructiveHint: false,
    });
    expect(byName.kintone_record_delete?.annotations).toMatchObject({
      readOnlyHint: false,
      destructiveHint: true,
    });
    const props = (name: string) =>
      Object.keys(
        (byName[name]?.inputSchema as { properties: object } | undefined)?.properties ?? {},
      );
    expect(props("kintone_apps_list")).toContain("max_response_tokens");
    expect(props("kintone_record_create")).not.toContain("max_response_tokens");
    expect(props("kintone_record_update")).not.toContain("max_response_tokens");
    expect(props("kintone_record_delete")).not.toContain("max_response_tokens");
  });

  it("config で toolset・read_only に合わせて隠す", async () => {
    const ro = await rpcTo(setup(TOOLS, { toolsets: "all", readOnly: true }).factory, "tools/list");
    expect((ro.result?.tools ?? []).map((t) => t.name)).toEqual(["kintone_apps_list"]);
    const rec = await rpcTo(
      setup(TOOLS, { toolsets: new Set<Toolset>(["record"]), readOnly: false }).factory,
      "tools/list",
    );
    expect((rec.result?.tools ?? []).map((t) => t.name)).toEqual([
      "kintone_record_create",
      "kintone_record_update",
      "kintone_record_delete",
    ]);
  });

  it("隠したツールは tools/call でも呼べない", async () => {
    const s = setup(TOOLS, { toolsets: "all", readOnly: true });
    const res = await rpcTo(s.factory, "tools/call", {
      name: "kintone_record_delete",
      arguments: {},
    });
    expect(res.error ?? res.result?.isError).toBeTruthy();
    expect(s.getClientCalls).toHaveLength(0);
  });

  it("名前が重複していたら登録時に例外", () => {
    const dup = [
      simpleTool("kintone_apps_list", "ReadOnly", "app"),
      simpleTool("kintone_apps_list", "ReadOnly", "app"),
    ];
    expect(() => setup(dup).factory()).toThrow(/kintone_apps_list/);
  });
});

describe("registerTools: tools/call", () => {
  it("handler の戻り値を JSON text 1 件で返す(structuredContent なし)。undefined は null", async () => {
    const s = setup([
      simpleTool("kintone_apps_list", "ReadOnly", "app", async () => ({ apps: [{ appId: "1" }] })),
      simpleTool("kintone_apps_none", "ReadOnly", "app", async () => undefined),
    ]);
    const a = await call(s, "kintone_apps_list");
    expect(a.result?.isError).toBeFalsy();
    expect(a.result?.content).toHaveLength(1);
    expect(JSON.parse(firstText(a))).toEqual({ apps: [{ appId: "1" }] });
    expect((a.result as { structuredContent?: unknown }).structuredContent).toBeUndefined();
    expect(JSON.parse(firstText(await call(s, "kintone_apps_none")))).toBeNull();
  });

  it("入力の検証エラーは argument_error で、クライアントも handler も呼ばない", async () => {
    const handler = vi.fn(async () => ({}));
    const s = setup([simpleTool("kintone_apps_list", "ReadOnly", "app", handler)]);
    for (const args of [{ n: "x" }, { n: 1.5 }, { unknown: 1 }]) {
      const { result } = await call(s, "kintone_apps_list", args);
      expect(result?.isError).toBe(true);
      const body = JSON.parse(firstText({ result }));
      expect(body.schema_version).toBe("1");
      expect(body.error).toMatchObject({ code: "argument_error", retryable: false });
    }
    expect(handler).not.toHaveBeenCalled();
    expect(s.getClientCalls).toHaveLength(0);
  });

  it("1 回の tools/call につきクライアントを 1 つ作り、maxApiCalls を宣言したツールはその値を渡す", async () => {
    const s = setup([
      simpleTool("kintone_apps_list", "ReadOnly", "app"),
      simpleTool("kintone_apps_many", "ReadOnly", "app", undefined, { maxApiCalls: 7 }),
    ]);
    await call(s, "kintone_apps_list");
    await call(s, "kintone_apps_many");
    expect(s.getClientCalls).toEqual([{}, { maxApiCalls: 7 }]);
  });

  it("handler にはそのツール呼び出し用のクライアントが ctx.client で渡る", async () => {
    let seen: unknown;
    const s = setup([
      simpleTool("kintone_apps_list", "ReadOnly", "app", async (_a, ctx) => {
        seen = ctx.client;
        return {};
      }),
    ]);
    await call(s, "kintone_apps_list");
    expect(typeof (seen as { get: unknown }).get).toBe("function");
  });

  it("kintone の 401 は authentication_error で、onKintoneUnauthorized を 1 回呼ぶ", async () => {
    const s = setup([
      simpleTool("kintone_apps_list", "ReadOnly", "app", async () => {
        throw new KintoneUnauthorizedError({ kintoneCode: "CB_AU01" });
      }),
    ]);
    const { result } = await call(s, "kintone_apps_list");
    expect(s.unauthorized).toBe(1);
    const body = JSON.parse(firstText({ result }));
    expect(body.error.code).toBe("authentication_error");
    expect(body.error.details).toEqual({ http_status: 401, kintone_code: "CB_AU01" });
  });

  it("KintoneMcpError は details ごとエンベロープに変換し、401 の合図は呼ばない", async () => {
    const s = setup([
      simpleTool("kintone_apps_list", "ReadOnly", "app", async () => {
        throw new ResolverError("resolver_app_ambiguous", "multiple", {
          kind: "app",
          ref: "顧客",
          candidates: [{ id: "1", code: "A", name: "顧客" }],
        });
      }),
      simpleTool("kintone_apps_down", "ReadOnly", "app", async () => {
        throw new KintoneApiError(503);
      }),
    ]);
    const a = JSON.parse(firstText(await call(s, "kintone_apps_list")));
    expect(a.error.code).toBe("resolver_app_ambiguous");
    expect(a.error.details.candidates).toEqual([{ id: "1", code: "A", name: "顧客" }]);
    const b = JSON.parse(firstText(await call(s, "kintone_apps_down")));
    expect(b.error).toMatchObject({ code: "api_error", retryable: true });
    expect(s.unauthorized).toBe(0);
  });

  it("想定外の例外は固定文の generic_error。ログには例外名と、トークンを伏せた message", async () => {
    const errorLog = vi.spyOn(console, "error").mockImplementation(() => {});
    const s = setup([
      simpleTool("kintone_apps_list", "ReadOnly", "app", async () => {
        throw new TypeError(`boom kt-secret and ${BEARER} abc.def-123`);
      }),
    ]);
    const { result } = await call(s, "kintone_apps_list");
    const text = firstText({ result });
    expect(JSON.parse(text).error).toEqual({
      code: "generic_error",
      message: "Unexpected error.",
      retryable: false,
    });
    expect(text).not.toContain("kt-secret");
    expect(errorLog).toHaveBeenCalledTimes(1);
    const logged = String(errorLog.mock.calls[0]?.join(" "));
    expect(logged).toContain("kintone_apps_list");
    expect(logged).toContain("TypeError");
    expect(logged).not.toContain("kt-secret");
    expect(logged).not.toContain("abc.def-123");
  });

  it("想定外でない KintoneMcpError はログに出さない", async () => {
    const errorLog = vi.spyOn(console, "error").mockImplementation(() => {});
    const s = setup([
      simpleTool("kintone_apps_list", "ReadOnly", "app", async () => {
        throw new KintoneApiError(500);
      }),
    ]);
    await call(s, "kintone_apps_list");
    expect(errorLog).not.toHaveBeenCalled();
  });
});

describe("registerTools: max_response_tokens", () => {
  const big = { text: "a".repeat(4 * 60_000) }; // 約 60000 トークン
  const bigTool = (category: ToolCategory = "ReadOnly") =>
    simpleTool(
      category === "ReadOnly" ? "kintone_records_big" : "kintone_record_big",
      category,
      "record",
      async () => big,
    );

  it("既定は 50000: 超えたら切り詰めず response_too_large(isError)", async () => {
    const s = setup([bigTool()]);
    const { result } = await call(s, "kintone_records_big");
    expect(result?.isError).toBe(true);
    const text = firstText({ result });
    const body = JSON.parse(text);
    expect(body.error).toMatchObject({ code: "response_too_large", retryable: false });
    expect(body.error.message).toContain("max_response_tokens");
    expect(text).not.toContain("aaaa");
  });

  it("max_response_tokens: 0 で無効、大きい値なら通る", async () => {
    const s = setup([bigTool()]);
    for (const limit of [0, 100_000]) {
      const { result } = await call(s, "kintone_records_big", { max_response_tokens: limit });
      expect(result?.isError, `limit ${limit}`).toBeFalsy();
      expect(JSON.parse(firstText({ result }))).toEqual(big);
    }
  });

  it("上限ちょうどは通り、1 少ないと response_too_large", async () => {
    const result = { items: ["顧客", "abcdefgh"] };
    const estimated = estimateTokens(JSON.stringify(result));
    const s = setup([
      simpleTool("kintone_records_small", "ReadOnly", "record", async () => result),
    ]);
    const ok = await call(s, "kintone_records_small", { max_response_tokens: estimated });
    expect(ok.result?.isError).toBeFalsy();
    const ng = await call(s, "kintone_records_small", { max_response_tokens: estimated - 1 });
    expect(JSON.parse(firstText(ng)).error.code).toBe("response_too_large");
  });

  it("負の値・非整数は argument_error で、kintone(クライアント・handler)を呼ばない", async () => {
    const handler = vi.fn(async () => ({}));
    const s = setup([simpleTool("kintone_records_small", "ReadOnly", "record", handler)]);
    for (const v of [-1, 1.5, "10"]) {
      const { result } = await call(s, "kintone_records_small", { max_response_tokens: v });
      expect(JSON.parse(firstText({ result })).error.code, `value ${String(v)}`).toBe(
        "argument_error",
      );
    }
    expect(handler).not.toHaveBeenCalled();
    expect(s.getClientCalls).toHaveLength(0);
  });

  it.each(["WriteIdempotent", "WriteNonIdempotent", "Destructive"] as const)(
    "%s には上限をかけない(大きな応答も成功のまま返す)。max_response_tokens を渡すと argument_error",
    async (category) => {
      const s = setup([bigTool(category)]);
      const ok = await call(s, "kintone_record_big");
      expect(ok.result?.isError).toBeFalsy();
      expect(JSON.parse(firstText(ok))).toEqual(big);
      const ng = await call(s, "kintone_record_big", { max_response_tokens: 10 });
      expect(JSON.parse(firstText(ng)).error.code).toBe("argument_error");
    },
  );

  it("responseLimit: false の ReadOnly には上限をかけない", async () => {
    const s = setup([
      simpleTool("kintone_records_raw", "ReadOnly", "record", async () => big, {
        responseLimit: false,
      }),
    ]);
    const { result } = await call(s, "kintone_records_raw");
    expect(result?.isError).toBeFalsy();
  });
});
