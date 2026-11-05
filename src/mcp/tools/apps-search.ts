import { z } from "zod";
import { defineTool } from "../registry";

export const APPS_SEARCH_TOOL = "kintone_apps_search";

/** 引数は `limit` のまま(ids / codes / name / space_ids / offset は M03 で足す)。`max_response_tokens` は defineTool が足す。 */
const inputSchema = z.strictObject({
  limit: z
    .number()
    .int()
    .min(1)
    .max(100)
    .optional()
    .describe("Number of apps to return (1-100). Default 100."),
});

export const appsSearch = defineTool({
  name: APPS_SEARCH_TOOL,
  title: "アプリ検索",
  description:
    "Search kintone apps the authenticated user can see (GET /k/v1/apps.json). Returns the raw kintone response: {apps:[{appId, code, name, ...}]}.",
  category: "ReadOnly",
  toolset: "app",
  inputSchema,
  handler: (args, { client }) =>
    client.get("/k/v1/apps.json", args.limit === undefined ? {} : { limit: args.limit }),
});
