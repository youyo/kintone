import { z } from "zod";
import { APP_RESOLVE_CALLS, writeBudget } from "../../resolver/budget";
import { defineTool } from "../registry";
import { withResolvedApp, writeAppTargetShape } from "./app-target";
import { MAX_WRITE_ITEMS, revisionShape } from "./record-write";

export const RECORD_DELETE_TOOL = "kintone_record_delete";

/**
 * 最悪の試行数: アプリ解決の GET は読み取りなのでリトライされうる(A × 3 = 33)+ 書き込み 1(リトライなし)= 34。
 * 論理呼び出しは A + 1 = 12。
 */
const MAX_API_CALLS = writeBudget(APP_RESOLVE_CALLS);

const inputSchema = z
  .strictObject({
    ...writeAppTargetShape,
    ids: z
      .array(z.number().int().positive())
      .min(1)
      .max(MAX_WRITE_ITEMS)
      .describe("IDs of the records to delete (1-100, positive integers). Required."),
    revisions: z
      .array(revisionShape)
      .max(MAX_WRITE_ITEMS)
      .optional()
      .describe(
        "Expected revisions for optimistic locking, one per id in the same order (-1 means no check). Must have the same length as ids.",
      ),
  })
  .superRefine((args, ctx) => {
    if (
      Array.isArray(args.ids) &&
      Array.isArray(args.revisions) &&
      args.revisions.length !== args.ids.length
    ) {
      ctx.addIssue({
        code: "custom",
        path: ["revisions"],
        message: `revisions must have the same length as ids (${args.ids.length}), got ${args.revisions.length}.`,
      });
    }
  });

export const recordDelete = defineTool({
  name: RECORD_DELETE_TOOL,
  title: "レコード削除",
  description:
    "Delete records of a kintone app (DELETE /k/v1/records.json). This cannot be undone. Specify the app with exactly one of app (ID) or app_ref (ID, code or name). For writes app_ref must match the app ID, code or name EXACTLY: a partial name match is never used; when only partial matches exist the call fails with resolver_app_exact_match_required and details.candidates, so call again with app set to a candidate ID. ids (1-100) are the record IDs to delete; revisions (optional, same length as ids) enable optimistic locking. All records are deleted or none. Returns {deleted:number}. A failed write is not retried.",
  category: "Destructive",
  toolset: "record",
  inputSchema,
  maxApiCalls: MAX_API_CALLS,
  handler: (args, ctx) =>
    withResolvedApp(args, ctx, { mode: "write" }, async (appId) => {
      await ctx.client.delete("/k/v1/records.json", {
        app: appId,
        ids: args.ids,
        ...(args.revisions === undefined ? {} : { revisions: args.revisions }),
      });
      return { deleted: args.ids.length };
    }),
});
