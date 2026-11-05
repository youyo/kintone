/**
 * 対象アプリの指定(`app` / `app_ref`)。どちらか 1 つが必須で排他(仕様書 §5)。
 * `app` は正の整数(そのまま使う)、`app_ref` はアプリ ID・コード・名前(解決順序は §7)。
 */
import { z } from "zod";
import { ArgumentError } from "../../errors";
import { KintoneApiError } from "../../kintone/client";
import type { ResolveMode } from "../../resolver/resolver";
import type { ToolContext } from "../registry";

export const appTargetShape = {
  app: z
    .number()
    .int()
    .positive()
    .optional()
    .describe(
      "kintone app ID (positive integer). Specify exactly one of app or app_ref (either one, not both).",
    ),
  app_ref: z
    .string()
    .min(1)
    .optional()
    .describe(
      "App reference: app ID, app code, or app name (exact match first, then partial match when it is unique). Specify exactly one of app or app_ref (either one, not both).",
    ),
};

/**
 * 書き込みツール(record_create / update / delete)の `app` / `app_ref`。`app_ref` は完全一致だけで解決する
 * (部分一致のみなら `resolver_app_exact_match_required`。ADR 0007)。
 */
export const writeAppTargetShape = {
  app: appTargetShape.app,
  app_ref: z
    .string()
    .min(1)
    .optional()
    .describe(
      "App reference: app ID, app code, or app name. Write operations accept an EXACT match only (a partial name match is never used: it fails with resolver_app_exact_match_required and details.candidates; pass a candidate ID as app). Specify exactly one of app or app_ref (either one, not both).",
    ),
};

export interface AppTargetArgs {
  app?: number | undefined;
  app_ref?: string | undefined;
}

export interface AppTargetOptions {
  mode: ResolveMode;
  /** false ならキャッシュを読まない(`kintone_app_describe`)。既定 true。 */
  useCache?: boolean;
}

/**
 * `app` / `app_ref` の排他を検査し、アプリ ID を解決して `run` に渡す。
 * キャッシュで解決したアプリへの呼び出し(`run`)が not_found(HTTP 404)なら、そのアプリのキャッシュを消してエラーをそのまま投げる(再解決はしない)。
 */
export async function withResolvedApp<T>(
  args: AppTargetArgs,
  ctx: ToolContext,
  options: AppTargetOptions,
  run: (appId: number) => Promise<T>,
): Promise<T> {
  const hasApp = args.app !== undefined;
  const hasRef = args.app_ref !== undefined;
  if (hasApp === hasRef) {
    throw new ArgumentError(
      hasApp
        ? "Specify exactly one of app or app_ref, not both."
        : "Specify exactly one of app or app_ref (either one is required).",
    );
  }
  if (args.app !== undefined) return run(args.app);

  const resolved = await ctx.resolver.resolveApp(args.app_ref as string, {
    mode: options.mode,
    ...(options.useCache === undefined ? {} : { useCache: options.useCache }),
  });
  try {
    return await run(resolved.appId);
  } catch (e) {
    if (resolved.fromCache && e instanceof KintoneApiError && e.status === 404) {
      await resolved.invalidate();
    }
    throw e;
  }
}
