import { z } from "zod";
import { APP_RESOLVE_CALLS, FIELD_RESOLVE_CALLS, writeBudget } from "../../resolver/budget";
import { defineTool } from "../registry";
import { withResolvedApp, writeAppTargetShape } from "./app-target";
import { recordShape, revisionShape, toKintoneInteger } from "./record-write";

export const RECORD_UPDATE_TOOL = "kintone_record_update";

/**
 * 最悪の試行数: アプリ解決 A とフィールド解決 F の GET は読み取りなのでリトライされうる((A + F) × 3 = 36)
 * + 書き込み 1(リトライなし)= 37。論理呼び出しは A + F + 1 = 13。
 */
const MAX_API_CALLS = writeBudget(APP_RESOLVE_CALLS + FIELD_RESOLVE_CALLS);

const inputSchema = z
  .strictObject({
    ...writeAppTargetShape,
    id: z
      .number()
      .int()
      .positive()
      .optional()
      .describe(
        "ID of the record to update. Specify either id or update_key_field(_ref) + update_key_value, not both.",
      ),
    update_key_field: z
      .string()
      .min(1)
      .optional()
      .describe(
        "Field code used as the update key (a text field or number field with 'prohibit duplicate values'). Exclusive with update_key_field_ref and id.",
      ),
    update_key_field_ref: z
      .string()
      .min(1)
      .optional()
      .describe(
        "Update key field given as field code or label. For writes only an EXACT match is accepted: a partial label match fails with resolver_field_exact_match_required and details.candidates (use the code). Exclusive with update_key_field and id.",
      ),
    update_key_value: z
      .string()
      .min(1)
      .optional()
      .describe("Value of the update key field that identifies the record."),
    revision: revisionShape
      .optional()
      .describe(
        "Expected revision of the record for optimistic locking (integer; -1 means no check).",
      ),
    record: recordShape
      .refine((r) => Object.keys(r).length > 0, "record must not be empty")
      .describe('Fields to update: {"<field code>":{"value":...}}. Required and not empty.'),
  })
  .superRefine((args, ctx) => {
    const hasId = args.id !== undefined;
    const hasField = args.update_key_field !== undefined || args.update_key_field_ref !== undefined;
    const hasValue = args.update_key_value !== undefined;
    const issue = (message: string) => ctx.addIssue({ code: "custom", message });
    if (args.update_key_field !== undefined && args.update_key_field_ref !== undefined) {
      issue("Specify exactly one of update_key_field or update_key_field_ref, not both.");
    }
    if (hasId && (hasField || hasValue)) {
      issue("Specify either id or update_key_field(_ref) + update_key_value, not both.");
    } else if (!hasId && !(hasField && hasValue)) {
      issue(
        "Specify the record with id, or with update_key_field (or update_key_field_ref) together with update_key_value.",
      );
    }
  });

export const recordUpdate = defineTool({
  name: RECORD_UPDATE_TOOL,
  title: "レコード更新",
  description:
    'Update one record of a kintone app (PUT /k/v1/record.json). Specify the app with exactly one of app (ID) or app_ref (ID, code or name). For writes app_ref must match the app ID, code or name EXACTLY: a partial name match is never used; when only partial matches exist the call fails with resolver_app_exact_match_required and details.candidates, so call again with app set to a candidate ID. Identify the record with id, or with update_key_field / update_key_field_ref + update_key_value (update_key_field_ref also accepts only an EXACT field code or label; a partial label match fails with resolver_field_exact_match_required and details.candidates). record (required, not empty) holds the fields to update as {"<field code>":{"value":...}}; revision enables optimistic locking. Returns {revision:number}. A failed write is not retried.',
  category: "WriteIdempotent",
  toolset: "record",
  inputSchema,
  maxApiCalls: MAX_API_CALLS,
  handler: (args, ctx) =>
    withResolvedApp(args, ctx, { mode: "write" }, async (appId) => {
      const body: Record<string, unknown> = { app: appId };
      if (args.id !== undefined) {
        body.id = args.id;
      } else {
        // 更新キーのフィールド参照は完全一致だけ(部分一致のみなら resolver_field_exact_match_required で PUT しない)。
        const field =
          args.update_key_field_ref !== undefined
            ? await ctx.resolver.resolveField(appId, args.update_key_field_ref, { mode: "write" })
            : (args.update_key_field as string);
        body.updateKey = { field, value: args.update_key_value };
      }
      if (args.revision !== undefined) body.revision = args.revision;
      body.record = args.record;
      const response = (await ctx.client.put("/k/v1/record.json", body)) as {
        revision?: unknown;
      } | null;
      return { revision: toKintoneInteger(response?.revision, "revision") };
    }),
});
