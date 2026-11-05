import { z } from "zod";
import { APP_RESOLVE_CALLS, writeBudget } from "../../resolver/budget";
import { defineTool } from "../registry";
import { withResolvedApp, writeAppTargetShape } from "./app-target";
import { MAX_WRITE_ITEMS, recordShape, toKintoneIntegers } from "./record-write";

export const RECORD_CREATE_TOOL = "kintone_record_create";

/**
 * 最悪の試行数: アプリ解決の GET は読み取りなのでリトライされうる(A × 3 = 33)+ 書き込み 1(リトライなし)= 34。
 * 論理呼び出しは A + 1 = 12。
 */
const MAX_API_CALLS = writeBudget(APP_RESOLVE_CALLS);

const inputSchema = z
  .strictObject({
    ...writeAppTargetShape,
    record: recordShape
      .optional()
      .describe(
        'One record to create: {"<field code>":{"value":...}}. Specify exactly one of record or records.',
      ),
    records: z
      .array(recordShape)
      .min(1)
      .max(MAX_WRITE_ITEMS)
      .optional()
      .describe(
        'Records to create (1-100), each {"<field code>":{"value":...}}. Specify exactly one of record or records.',
      ),
  })
  .superRefine((args, ctx) => {
    if ((args.record === undefined) === (args.records === undefined)) {
      ctx.addIssue({
        code: "custom",
        message:
          args.record === undefined
            ? "Specify exactly one of record or records (either one is required)."
            : "Specify exactly one of record or records, not both.",
      });
    }
  });

export const recordCreate = defineTool({
  name: RECORD_CREATE_TOOL,
  title: "レコード作成",
  description:
    'Create records in a kintone app (POST /k/v1/records.json). Specify the app with exactly one of app (ID) or app_ref (ID, code or name). For writes app_ref must match the app ID, code or name EXACTLY: a partial name match is never used; when only partial matches exist the call fails with resolver_app_exact_match_required and details.candidates, so call again with app set to a candidate ID. Specify exactly one of record (1 record) or records (1-100 records), each shaped {"<field code>":{"value":...}}; all records are created or none. Returns {ids:[number], revisions:[number]}. A failed write is not retried, and after a timeout or network failure the result is unknown (check before creating again). Without an explicit idempotency key, a call with the same arguments within 5 minutes is treated as a re-send of the earlier call and returns the previous result: the second record is NOT created. To create identical records on purpose, pass a different idempotency_key argument (or _meta.idempotency_key) on each call.',
  category: "WriteNonIdempotent",
  toolset: "record",
  inputSchema,
  maxApiCalls: MAX_API_CALLS,
  handler: (args, ctx) =>
    withResolvedApp(args, ctx, { mode: "write" }, async (appId) => {
      const records = args.records ?? [args.record as Record<string, unknown>];
      const response = (await ctx.client.post("/k/v1/records.json", { app: appId, records })) as {
        ids?: unknown;
        revisions?: unknown;
      } | null;
      return {
        ids: toKintoneIntegers(response?.ids, "ids"),
        revisions: toKintoneIntegers(response?.revisions, "revisions"),
      };
    }),
});
