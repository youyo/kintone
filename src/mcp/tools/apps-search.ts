import type { McpServer, StandardSchemaWithJSON } from "@modelcontextprotocol/server";
import { z } from "zod";
import { ArgumentError, toErrorEnvelope } from "../../errors";
import { type KintoneClient, KintoneUnauthorizedError } from "../../kintone/client";

export const APPS_SEARCH_TOOL = "kintone_apps_search";

export interface AppsSearchDeps {
  getClient: () => KintoneClient;
  /** kintone が 401 を返したとき(MCP の応答も HTTP 401 にするための合図)。 */
  onKintoneUnauthorized: () => void;
}

/** M01 の最小版: `limit` だけ受ける(M02 で ids / codes / name / space_ids / offset と max_response_tokens を足す)。 */
const inputSchema = z.strictObject({
  limit: z
    .number()
    .int()
    .min(1)
    .max(100)
    .optional()
    .describe("Number of apps to return (1-100). Default 100."),
});

/**
 * SDK には「公開用のスキーマ」だけを渡し、検証は自前で行う。SDK の検証エラーはプレーンテキストの
 * isError になり、仕様書 §6 のエンベロープにならないため。
 */
function advertiseOnly(schema: z.ZodObject): StandardSchemaWithJSON {
  const standard = schema["~standard"];
  return {
    "~standard": { ...standard, validate: (value: unknown) => ({ value }) },
  } as unknown as StandardSchemaWithJSON;
}

function describeIssues(error: z.ZodError): string {
  return error.issues
    .map((i) => `${i.path.length ? i.path.join(".") : "(input)"}: ${i.message}`)
    .join("; ");
}

export function registerAppsSearch(server: McpServer, deps: AppsSearchDeps): void {
  server.registerTool(
    APPS_SEARCH_TOOL,
    {
      title: "アプリ検索",
      description:
        "Search kintone apps the authenticated user can see (GET /k/v1/apps.json). Returns the raw kintone response: {apps:[{appId, code, name, ...}]}.",
      inputSchema: advertiseOnly(inputSchema),
      annotations: {
        title: "アプリ検索",
        readOnlyHint: true,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    async (args: unknown) => {
      try {
        const parsed = inputSchema.safeParse(args ?? {});
        if (!parsed.success) throw new ArgumentError(describeIssues(parsed.error));
        const body = await deps
          .getClient()
          .get(
            "/k/v1/apps.json",
            parsed.data.limit === undefined ? {} : { limit: parsed.data.limit },
          );
        return { content: [{ type: "text" as const, text: JSON.stringify(body) }] };
      } catch (e) {
        if (e instanceof KintoneUnauthorizedError) deps.onKintoneUnauthorized();
        return {
          isError: true,
          content: [{ type: "text" as const, text: JSON.stringify(toErrorEnvelope(e)) }],
        };
      }
    },
  );
}
