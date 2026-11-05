import { z } from "zod";
import type { Query } from "../../kintone/client";
import { readBudget } from "../../resolver/budget";
import { defineTool } from "../registry";

export const APPS_SEARCH_TOOL = "kintone_apps_search";

/** 論理呼び出しは apps.json 1 回だけ(リゾルバー・キャッシュは使わない)。 */
const LOGICAL_CALLS = 1;

/** 仕様書 §5.1。制約の出典: 公式 get-apps(配列は 100 件まで、codes は 64 文字まで、name は 64 文字まで、limit は 1〜100)。`max_response_tokens` は defineTool が足す。 */
const inputSchema = z.strictObject({
  ids: z
    .array(z.number().int().positive())
    .max(100)
    .optional()
    .describe("App IDs to match (up to 100)."),
  codes: z
    .array(z.string().min(1).max(64))
    .max(100)
    .optional()
    .describe("App codes to match exactly (case-sensitive; 1-64 characters each, up to 100)."),
  name: z
    .string()
    .max(64)
    .optional()
    .describe("App name partial match (case-insensitive, up to 64 characters)."),
  space_ids: z
    .array(z.number().int().positive())
    .max(100)
    .optional()
    .describe("Space IDs the apps belong to (up to 100)."),
  limit: z
    .number()
    .int()
    .min(1)
    .max(100)
    .optional()
    .describe("Number of apps to return (1-100). Default 100."),
  offset: z
    .number()
    .int()
    .min(0)
    .optional()
    .describe(
      "Number of apps to skip (0 or greater). Default 0. Call again with offset to get the next page.",
    ),
});

export const appsSearch = defineTool({
  name: APPS_SEARCH_TOOL,
  title: "アプリ検索",
  description:
    "Search kintone apps the authenticated user can see (GET /k/v1/apps.json). Filter by ids, codes, name (partial match) or space_ids; returns one page (use offset for the next). Returns the raw kintone response: {apps:[{appId, code, name, ...}]}. Always reads the latest data (no cache).",
  category: "ReadOnly",
  toolset: "app",
  inputSchema,
  maxApiCalls: readBudget(LOGICAL_CALLS),
  handler: (args, { client }) => {
    const query: Query = {};
    if (args.ids && args.ids.length > 0) query.ids = args.ids;
    if (args.codes && args.codes.length > 0) query.codes = args.codes;
    if (args.name) query.name = args.name;
    if (args.space_ids && args.space_ids.length > 0) query.spaceIds = args.space_ids;
    if (args.limit !== undefined) query.limit = args.limit;
    if (args.offset !== undefined) query.offset = args.offset;
    return client.get("/k/v1/apps.json", query);
  },
});
