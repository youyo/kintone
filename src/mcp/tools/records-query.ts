import { z } from "zod";
import { KintoneMcpError } from "../../errors";
import type { Query } from "../../kintone/client";
import { APP_RESOLVE_CALLS, readBudget } from "../../resolver/budget";
import { defineTool } from "../registry";
import { appTargetShape, withResolvedApp } from "./app-target";

export const RECORDS_QUERY_TOOL = "kintone_records_query";

/** 論理呼び出しの最悪: アプリ解決 A(11)+ records.json 1 = 12(× 3 = 36)。 */
const LOGICAL_CALLS = APP_RESOLVE_CALLS + 1;

const inputSchema = z.strictObject({
  ...appTargetShape,
  query: z
    .string()
    .optional()
    .describe(
      'kintone query language, e.g. `status = "Open" order by $id desc limit 50 offset 0`. Put limit (max 500, default 100) and offset (max 10,000) in the query.',
    ),
  fields: z
    .array(z.string().min(1))
    .max(100)
    .optional()
    .describe("Field codes to return (up to 100). Omit to return all fields."),
  total_count: z
    .boolean()
    .optional()
    .describe("true to also return total_count (number of records matching the query)."),
});

export const recordsQuery = defineTool({
  name: RECORDS_QUERY_TOOL,
  title: "レコード検索",
  description:
    "Query records of a kintone app (GET /k/v1/records.json). Specify the app with exactly one of app (ID) or app_ref (ID, code or name). Returns {records:[...raw kintone records], total_count?:number}. One request per call, no automatic paging: put limit/offset in query to fetch more. Large results fail with response_too_large; narrow with fields, a smaller limit or a tighter query.",
  category: "ReadOnly",
  toolset: "record",
  inputSchema,
  maxApiCalls: readBudget(LOGICAL_CALLS),
  handler: (args, ctx) =>
    withResolvedApp(args, ctx, { mode: "read" }, async (appId) => {
      const query: Query = { app: appId };
      if (args.query) query.query = args.query;
      if (args.fields && args.fields.length > 0) query.fields = args.fields;
      if (args.total_count === true) query.totalCount = true;
      const response = (await ctx.client.get("/k/v1/records.json", query)) as {
        records?: unknown;
        totalCount?: unknown;
      };
      const out: { records: unknown[]; total_count?: number } = {
        records: Array.isArray(response.records) ? response.records : [],
      };
      if (response.totalCount !== undefined && response.totalCount !== null) {
        // kintone は totalCount を文字列で返す。数値に変換して返す。
        const n = Number(response.totalCount);
        if (response.totalCount === "" || !Number.isFinite(n)) {
          throw new KintoneMcpError(
            "api_error",
            "kintone returned a non-numeric totalCount.",
            true,
          );
        }
        out.total_count = n;
      }
      return out;
    }),
});
