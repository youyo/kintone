import { z } from "zod";
import { APP_RESOLVE_CALLS, readBudget } from "../../resolver/budget";
import { fieldEntriesOf } from "../../resolver/resolver";
import { defineTool } from "../registry";
import { appTargetShape, withResolvedApp } from "./app-target";

export const APP_DESCRIBE_TOOL = "kintone_app_describe";

/** 論理呼び出しの最悪: アプリ解決 A(11)+ app.json 1 + form/fields.json 1 = 13(× 3 = 39)。 */
const LOGICAL_CALLS = APP_RESOLVE_CALLS + 2;

const inputSchema = z.strictObject({
  ...appTargetShape,
  lang: z
    .enum(["ja", "en", "zh", "user", "default"])
    .optional()
    .describe("Display language of the field labels: ja, en, zh, user or default."),
});

/** kintone の値が空(未定義・null・空文字・空のオブジェクト)か。 */
function isEmpty(v: unknown): boolean {
  if (v === undefined || v === null || v === "") return true;
  return typeof v === "object" && !Array.isArray(v) && Object.keys(v).length === 0;
}

/** 空でない値だけを `key: value` として足す(Go の `omitempty`)。 */
function omitEmpty(entries: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(entries).filter(([, v]) => !isEmpty(v)));
}

const text = (v: unknown): string =>
  typeof v === "string" ? v : v === undefined || v === null ? "" : String(v);

export const appDescribe = defineTool({
  name: APP_DESCRIBE_TOOL,
  title: "アプリ定義の取得",
  description:
    "Describe a kintone app: its basic info and its field definitions (GET /k/v1/app.json + GET /k/v1/app/form/fields.json). Specify the app with exactly one of app (ID) or app_ref (ID, code or name). Returns {app:{app_id, code, name, description, space_id?, ...}, fields:{<field code>:{...raw kintone definition}}, revision?}. Always reads the latest data (never uses the cache) and refreshes the field cache used by name resolution.",
  category: "ReadOnly",
  toolset: "app",
  inputSchema,
  maxApiCalls: readBudget(LOGICAL_CALLS),
  handler: (args, ctx) =>
    // 常に最新を見る: app_ref の解決でもキャッシュを読まない(結果は書く)。
    withResolvedApp(args, ctx, { mode: "read", useCache: false }, async (appId) => {
      const { client } = ctx;
      const app = (await client.get("/k/v1/app.json", { id: appId })) as Record<string, unknown>;
      const fieldsResponse = (await client.get("/k/v1/app/form/fields.json", {
        app: appId,
        ...(args.lang === undefined ? {} : { lang: args.lang }),
      })) as { properties?: Record<string, unknown>; revision?: unknown };
      const properties = fieldsResponse.properties ?? {};
      // フィールドのキャッシュを更新する(名前解決の次回以降が速くなる)。失敗しても describe は成功させる(cache は例外を出さない)。
      await ctx.cache.putFields(appId, fieldEntriesOf(properties));
      return {
        app: {
          app_id: text(app.appId),
          code: text(app.code),
          name: text(app.name),
          description: text(app.description),
          ...omitEmpty({
            space_id: app.spaceId,
            thread_id: app.threadId,
            created_at: app.createdAt,
            creator: app.creator,
            modified_at: app.modifiedAt,
            modifier: app.modifier,
          }),
        },
        fields: properties,
        ...omitEmpty({ revision: fieldsResponse.revision }),
      };
    }),
});
