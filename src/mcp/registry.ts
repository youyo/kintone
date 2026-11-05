/**
 * ツール登録の仕組み(ADR 0009)。1 ツール = 名前・説明・入力スキーマ(zod)・区分・toolset・ハンドラ。
 * M03 以降のツールは `defineTool` で定義し、`tools/index.ts` の `ALL_TOOLS` に足すだけで載る。
 */
import type { McpServer, StandardSchemaWithJSON } from "@modelcontextprotocol/server";
import type { z } from "zod";
import { ArgumentError, KintoneMcpError, toErrorEnvelope } from "../errors";
import { type KintoneClient, KintoneUnauthorizedError } from "../kintone/client";
import {
  assertWithinResponseLimit,
  maxResponseTokensShape,
  resolveResponseLimit,
} from "./response-limit";

/** 4 区分。annotation の値と `read_only`(ReadOnly 以外を隠す)の根拠。 */
export type ToolCategory = "ReadOnly" | "WriteIdempotent" | "WriteNonIdempotent" | "Destructive";

/** リソース単位のグループ。/mcp の URL クエリ `?toolsets=` で有効にするものを選ぶ。 */
export const TOOLSETS = ["app", "record"] as const;
export type Toolset = (typeof TOOLSETS)[number];

export interface ToolAnnotations {
  title: string;
  readOnlyHint: boolean;
  destructiveHint: boolean;
  idempotentHint: boolean;
  openWorldHint: boolean;
}

/** 4 区分の annotation(`title` は日本語)。 */
export function annotationsFor(category: ToolCategory, title: string): ToolAnnotations {
  switch (category) {
    case "ReadOnly":
      return {
        title,
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      };
    case "WriteIdempotent":
      return {
        title,
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      };
    case "WriteNonIdempotent":
      return {
        title,
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: false,
        openWorldHint: true,
      };
    case "Destructive":
      return {
        title,
        readOnlyHint: false,
        destructiveHint: true,
        idempotentHint: true,
        openWorldHint: true,
      };
  }
}

/** ツールのハンドラに渡すもの。M03 でリゾルバー・キャッシュを足す。 */
export interface ToolContext {
  /** このツール呼び出し専用の kintone クライアント(直列化・呼び出し回数上限はこの単位)。 */
  client: KintoneClient;
}

export interface ToolDefinitionInput<S extends z.ZodObject> {
  /** `kintone_<resource>_<action>` */
  name: string;
  /** annotation の title(日本語)。 */
  title: string;
  description: string;
  category: ToolCategory;
  toolset: Toolset;
  /** `z.strictObject(...)`。ReadOnly では `max_response_tokens` が自動で足される。 */
  inputSchema: S;
  /**
   * このツール 1 回の呼び出しで許す kintone API の回数(リトライ含む)。省略時はクライアントの既定(50)。
   * 既定のままにする(ADR 0008。最悪ケースは 13 回 × 3 = 39 で 50 に収まる)。
   */
  maxApiCalls?: number;
  /** 応答サイズの上限(引数 max_response_tokens)を付けるか。ReadOnly の既定は付ける。書き込み系には付けない。 */
  responseLimit?: boolean;
  /** 戻り値は JSON にして text content に入れる。構造化出力(structuredContent)は使わない。 */
  handler: (args: z.infer<S>, ctx: ToolContext) => Promise<unknown>;
}

export interface ToolDefinition {
  readonly name: string;
  readonly description: string;
  readonly category: ToolCategory;
  readonly toolset: Toolset;
  readonly inputSchema: z.ZodObject;
  readonly maxApiCalls?: number;
  /** 応答サイズの上限(max_response_tokens)を適用するか。 */
  readonly responseLimit: boolean;
  readonly annotations: ToolAnnotations;
  /** 引数は自前の検証(inputSchema)を通ったものだけが来る。 */
  readonly handler: (args: unknown, ctx: ToolContext) => Promise<unknown>;
}

const TOOL_NAME = /^kintone_[a-z][a-z0-9]*(?:_[a-z0-9]+)+$/;

export function defineTool<S extends z.ZodObject>(input: ToolDefinitionInput<S>): ToolDefinition {
  if (!TOOL_NAME.test(input.name)) {
    throw new Error(`ツール名は kintone_<resource>_<action> の形式にする: ${input.name}`);
  }
  const responseLimit = input.category === "ReadOnly" && input.responseLimit !== false;
  return {
    name: input.name,
    description: input.description,
    category: input.category,
    toolset: input.toolset,
    inputSchema: responseLimit
      ? input.inputSchema.extend(maxResponseTokensShape)
      : input.inputSchema,
    responseLimit,
    ...(input.maxApiCalls === undefined ? {} : { maxApiCalls: input.maxApiCalls }),
    annotations: annotationsFor(input.category, input.title),
    handler: (args, ctx) => input.handler(args as z.infer<S>, ctx),
  };
}

/** /mcp の URL クエリが不正(HTTP 400 `invalid_query` にする)。 */
export class QueryError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "QueryError";
  }
}

export interface ToolConfig {
  /** 有効にする toolset。"all" ですべて。 */
  toolsets: "all" | ReadonlySet<Toolset>;
  /** true なら ReadOnly 以外(書き込み・破壊的)のツールを公開しない。 */
  readOnly: boolean;
}

const CONFIG_KEYS = ["read_only", "toolsets"] as const;
/** 小文字にして `_` と `-` を除く(`readOnly` `READ_ONLY` `read-only` を同じ綴りとして扱う)。 */
const squash = (key: string) => key.toLowerCase().replace(/[_\-\s]/g, "");
/** 設定キーの綴り違いとみなす接頭辞(`toolset` `tools` `readonlly` `readOnlyMode` など)。 */
const CONFIG_KEY_PREFIXES = ["read", "tool"] as const;

/**
 * `readonly` `readOnly` `tool_sets` `toolset` のように、squash 後に `read` か `tool` で始まる未知のキーは
 * QueryError にする。無視すると、読み取り専用のつもりで書き込み可能なまま接続してしまう。
 * それ以外の未知のキー(Portal の `optimize_context` など)は無視する。
 */
function rejectMisspelledKeys(params: URLSearchParams): void {
  for (const key of new Set(params.keys())) {
    if ((CONFIG_KEYS as readonly string[]).includes(key)) continue;
    const squashed = squash(key);
    if (CONFIG_KEY_PREFIXES.some((p) => squashed.startsWith(p))) {
      const intended = CONFIG_KEYS.find((k) => squash(k) === squashed);
      throw new QueryError(
        intended
          ? `クエリのキー "${key}" は綴り違い。正しいキーは "${intended}"`
          : `クエリのキー "${key}" は綴り違いの疑い。正しいキーは "read_only" と "toolsets"`,
      );
    }
  }
}

/**
 * /mcp の URL クエリからツール設定を作る(`?toolsets=app,record&read_only=true`、仕様書 §8)。
 * 公開するツールは tools/list の時点で決まるため、ツール引数ではなく接続 URL で決める。
 * 未知の toolset 名・読めない真偽値・綴り違いのキー・公開ツールが 0 個になる組み合わせは QueryError
 * (typo で黙って全ツールが消えたり、読み取り専用のつもりで書き込めたりしないため)。関係のないクエリは無視する。
 * `tools` は公開候補(通常は ALL_TOOLS)。
 */
export function parseToolConfig(
  params: URLSearchParams,
  tools: readonly ToolDefinition[],
): ToolConfig {
  rejectMisspelledKeys(params);
  const names = params
    .getAll("toolsets")
    .flatMap((v) => v.split(","))
    .map((s) => s.trim().toLowerCase())
    .filter((s) => s !== "");
  let toolsets: ToolConfig["toolsets"] = "all";
  if (names.length > 0 && !names.includes("all")) {
    const known = new Set<string>(TOOLSETS);
    const unknown = names.filter((n) => !known.has(n));
    if (unknown.length > 0) {
      throw new QueryError(
        `toolsets に未知の toolset がある: ${unknown.join(", ")}(有効: all, ${TOOLSETS.join(", ")})`,
      );
    }
    toolsets = new Set(names as Toolset[]);
  }

  const readOnlyValues = params.getAll("read_only");
  if (readOnlyValues.length > 1) {
    throw new QueryError("read_only は 1 回だけ指定できる(true か false)");
  }
  const ro = (readOnlyValues[0] ?? "").trim().toLowerCase();
  if (ro !== "" && ro !== "true" && ro !== "false") {
    throw new QueryError(`read_only は true か false: ${readOnlyValues[0]}`);
  }
  const config: ToolConfig = { toolsets, readOnly: ro === "true" };
  // 公開ツールが 0 個だと MCP サーバーが tools capability を宣言しない。空の一覧を返すのではなく、組み合わせの誤りとして弾く。
  if (selectTools(tools, config).length === 0) {
    const sets = toolsets === "all" ? "all" : [...toolsets].sort().join(",");
    throw new QueryError(
      `toolsets=${sets} と read_only=${config.readOnly} の組み合わせでは公開されるツールがありません`,
    );
  }
  return config;
}

export function selectTools(
  tools: readonly ToolDefinition[],
  config: ToolConfig,
): ToolDefinition[] {
  return tools.filter(
    (t) =>
      (config.toolsets === "all" || config.toolsets.has(t.toolset)) &&
      !(config.readOnly && t.category !== "ReadOnly"),
  );
}

export interface RegisterDeps {
  /**
   * tools/call ごとに呼ぶ。1 回のツール呼び出しにつき 1 クライアント。
   * ツールが `maxApiCalls` を宣言していれば渡す(宣言がなければ空のオブジェクト)。
   */
  getClient: (options: { maxApiCalls?: number }) => KintoneClient;
  /** kintone が 401 を返したとき(MCP の応答も HTTP 401 にするための合図)。 */
  onKintoneUnauthorized: () => void;
}

function textResult(text: string, isError = false) {
  return { ...(isError ? { isError: true } : {}), content: [{ type: "text" as const, text }] };
}

/** zod の検証エラーを、どの引数が悪いか分かる 1 行にする。 */
function describeIssues(error: z.ZodError): string {
  return error.issues
    .map((i) => `${i.path.length ? i.path.join(".") : "(input)"}: ${i.message}`)
    .join("; ");
}

/**
 * SDK には「公開用のスキーマ」だけを渡し、検証は自前で行う。SDK の検証エラーはプレーンテキストの
 * isError になり、仕様書 §6 のエンベロープにならないため。JSON Schema は zod のものをそのまま使う。
 */
function advertiseOnly(schema: z.ZodObject): StandardSchemaWithJSON {
  const standard = schema["~standard"];
  return {
    "~standard": { ...standard, validate: (value: unknown) => ({ value }) },
  } as unknown as StandardSchemaWithJSON;
}

/**
 * 想定外の例外(generic_error にするもの)を調べられるよう、ツール名・例外名・message をログに残す。
 * 応答には出さない。message にはトークンが混ざりうるので、クライアントが知るトークンと Bearer を伏せる。
 */
function logUnexpected(toolName: string, e: unknown, client: KintoneClient | undefined): void {
  const name = e instanceof Error ? e.name : typeof e;
  const raw = e instanceof Error ? e.message : String(e);
  const redacted = (client ? client.redact(raw) : raw).replace(
    /Bearer\s+[A-Za-z0-9._~+/=-]+/g,
    "Bearer [REDACTED]",
  );
  console.error(`tool ${toolName} failed unexpectedly: ${name}: ${redacted}`);
}

export function registerTools(
  server: McpServer,
  tools: readonly ToolDefinition[],
  config: ToolConfig,
  deps: RegisterDeps,
): void {
  const seen = new Set<string>();
  for (const t of tools) {
    if (seen.has(t.name)) throw new Error(`ツール名が重複している: ${t.name}`);
    seen.add(t.name);
  }

  for (const tool of selectTools(tools, config)) {
    server.registerTool(
      tool.name,
      {
        title: tool.annotations.title,
        description: tool.description,
        inputSchema: advertiseOnly(tool.inputSchema),
        annotations: tool.annotations,
      },
      async (args: unknown) => {
        let client: KintoneClient | undefined;
        try {
          const parsed = tool.inputSchema.safeParse(args ?? {});
          if (!parsed.success) throw new ArgumentError(describeIssues(parsed.error));
          // 上限の引数は kintone を呼ぶ前に検証する(負の値で API を無駄に叩かない)。
          const responseLimit = tool.responseLimit
            ? resolveResponseLimit(
                (parsed.data as { max_response_tokens?: number }).max_response_tokens,
              )
            : undefined;
          client = deps.getClient(
            tool.maxApiCalls === undefined ? {} : { maxApiCalls: tool.maxApiCalls },
          );
          const result = await tool.handler(parsed.data, { client });
          const text = JSON.stringify(result ?? null);
          if (responseLimit !== undefined) assertWithinResponseLimit(text, responseLimit);
          return textResult(text);
        } catch (e) {
          if (e instanceof KintoneUnauthorizedError) deps.onKintoneUnauthorized();
          if (!(e instanceof KintoneMcpError)) logUnexpected(tool.name, e, client);
          return textResult(JSON.stringify(toErrorEnvelope(e)), true);
        }
      },
    );
  }
}
