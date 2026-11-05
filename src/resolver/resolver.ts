/**
 * アプリ・フィールドの名前解決(仕様書 §7、ADR 0007)。
 *
 * アプリ(`resolveApp`): 空文字は argument_error → 正の整数の文字列は ID(API を呼ばない)→ コード完全一致
 * → 名前完全一致 → 名前部分一致。各段階でヒットしたら即採用し、次の段階へフォールバックしない。
 * フィールド(`resolveField`): コード完全一致 → label 完全一致 → label 部分一致(大文字小文字を区別)。
 *
 * mode:
 * - `read`: 部分一致の 1 件は採用、2 件以上は ambiguous
 * - `write`: 完全一致(ID・コード・名前 / フィールドコード・label)だけで解決する。部分一致しか当たらなければ
 *   1 件でも複数件でも採用せず `resolver_*_exact_match_required`(候補つき)。API の呼び出し回数・順序は read と同じ
 *
 * キャッシュ(`cache`): 検索の段階ごとに結果を KV に置く(TTL 600 秒)。キャッシュ由来の解決が not_found になったら、
 * **キャッシュから読んだ段階だけ**を 1 回だけ取り直す。1 回の解決で同じ段階の API を 2 度は呼ばない(メモ化)ので、
 * 取り直しを含めても最悪の呼び出し回数は A(コード 1 + 名前 10 = 11)を超えない。
 */
import { type AppCandidate, ArgumentError, type FieldCandidate, ResolverError } from "../errors";
import type { KintoneClient } from "../kintone/client";
import { APPS_PAGE_SIZE, MAX_APP_NAME_PAGES } from "./budget";
import {
  type AppEntry,
  type AppStage,
  createResolverCache,
  type FieldEntry,
  type ResolverCache,
} from "./cache";

export type ResolveMode = "read" | "write";

export interface ResolveOptions {
  mode: ResolveMode;
  /** false ならキャッシュを読まない(結果は書く)。`kintone_app_describe` が使う(常に最新を見る)。既定 true。 */
  useCache?: boolean;
}

export interface ResolvedApp {
  appId: number;
  /** この解決にキャッシュの値を使ったか。 */
  fromCache: boolean;
  /**
   * キャッシュで解決したアプリへの kintone 呼び出しが not_found だったとき、読んだ段階のキーと
   * そのアプリのフィールドのキャッシュを消す。API で取った結果・ID 直接の場合は何もしない。KV の失敗は握りつぶす。
   */
  invalidate(): Promise<void>;
}

export interface Resolver {
  resolveApp(ref: string, options: ResolveOptions): Promise<ResolvedApp>;
  /** フィールドコードを返す。 */
  resolveField(appId: number, ref: string, options: ResolveOptions): Promise<string>;
}

export interface ResolverDeps {
  client: KintoneClient;
  /** 省略時はキャッシュなし。 */
  cache?: ResolverCache;
}

const isPositiveIntegerRef = (ref: string): boolean =>
  /^[0-9]+$/.test(ref) && Number.isSafeInteger(Number(ref)) && Number(ref) > 0;

const appCandidate = (a: AppEntry): AppCandidate => ({
  id: a.appId,
  ...(a.code === "" ? {} : { code: a.code }),
  ...(a.name === "" ? {} : { name: a.name }),
});

const byCode = (a: FieldEntry, b: FieldEntry) => (a.code < b.code ? -1 : a.code > b.code ? 1 : 0);

const fieldCandidate = (f: FieldEntry): FieldCandidate => ({
  code: f.code,
  ...(f.label === "" ? {} : { label: f.label }),
});

/** `GET /k/v1/app/form/fields.json` の `properties`(キー = フィールドコード)から、トップレベルのフィールドを取り出す。 */
export function fieldEntriesOf(properties: unknown): FieldEntry[] {
  if (typeof properties !== "object" || properties === null) return [];
  const out: FieldEntry[] = [];
  for (const [code, prop] of Object.entries(properties)) {
    const p = (typeof prop === "object" && prop !== null ? prop : {}) as Record<string, unknown>;
    out.push({
      code,
      label: typeof p.label === "string" ? p.label : "",
      type: typeof p.type === "string" ? p.type : "",
    });
  }
  return out;
}

function toAppEntry(raw: unknown): AppEntry | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const r = raw as Record<string, unknown>;
  const appId = typeof r.appId === "number" ? String(r.appId) : r.appId;
  if (typeof appId !== "string") return undefined;
  return {
    appId,
    code: typeof r.code === "string" ? r.code : "",
    name: typeof r.name === "string" ? r.name : "",
  };
}

function appsOf(response: unknown): AppEntry[] {
  const apps = (response as { apps?: unknown } | null)?.apps;
  if (!Array.isArray(apps)) return [];
  return apps.flatMap((a) => {
    const e = toAppEntry(a);
    return e ? [e] : [];
  });
}

export function createResolver(deps: ResolverDeps): Resolver {
  const { client } = deps;
  const cache = deps.cache ?? createResolverCache(undefined, undefined);

  async function resolveApp(ref: string, options: ResolveOptions): Promise<ResolvedApp> {
    if (ref === "") throw new ArgumentError("app_ref must not be empty.");
    if (isPositiveIntegerRef(ref)) {
      return { appId: Number(ref), fromCache: false, invalidate: async () => {} };
    }
    const useCache = options.useCache !== false;

    // この 1 回の解決で API から取った段階の結果(同じ段階を 2 度取らない)。
    const fresh = new Map<AppStage, AppEntry[]>();
    // 今回の周回でキャッシュから読んだ段階。
    let used = new Set<AppStage>();

    async function loadCode(bypass: boolean): Promise<AppEntry[]> {
      const memo = fresh.get("code");
      if (memo) return memo;
      if (useCache && !bypass) {
        const cached = await cache.getApps("code", ref);
        if (cached) {
          used.add("code");
          return cached;
        }
      }
      const apps = appsOf(
        await client.get("/k/v1/apps.json", { codes: [ref], limit: APPS_PAGE_SIZE }),
      );
      fresh.set("code", apps);
      await cache.putApps("code", ref, apps);
      return apps;
    }

    async function loadName(bypass: boolean): Promise<AppEntry[]> {
      const memo = fresh.get("name");
      if (memo) return memo;
      if (useCache && !bypass) {
        const cached = await cache.getApps("name", ref);
        if (cached) {
          used.add("name");
          return cached;
        }
      }
      const all: AppEntry[] = [];
      for (let page = 0; page < MAX_APP_NAME_PAGES; page++) {
        const apps = appsOf(
          await client.get("/k/v1/apps.json", {
            name: ref,
            limit: APPS_PAGE_SIZE,
            offset: page * APPS_PAGE_SIZE,
          }),
        );
        all.push(...apps);
        if (apps.length < APPS_PAGE_SIZE) {
          fresh.set("name", all);
          await cache.putApps("name", ref, all);
          return all;
        }
      }
      throw new ResolverError(
        "resolver_app_list_too_large",
        `Too many apps match "${ref}" (more than ${MAX_APP_NAME_PAGES * APPS_PAGE_SIZE}). Use the app ID (app) or the app code instead, or a more specific name.`,
        { kind: "app", ref },
      );
    }

    const adopt = (entry: AppEntry): number => {
      const id = Number(entry.appId);
      if (!Number.isSafeInteger(id) || id <= 0) {
        throw new ResolverError("resolver_app_not_found", `App "${ref}" has an invalid ID.`, {
          kind: "app",
          ref,
          candidates: [],
        });
      }
      return id;
    };

    const ambiguous = (apps: AppEntry[]) =>
      new ResolverError(
        "resolver_app_ambiguous",
        `"${ref}" matches ${apps.length} apps. Pass the app ID as app (see details.candidates), or use the exact app code or name.`,
        { kind: "app", ref, candidates: apps.map(appCandidate) },
      );

    async function attempt(bypass: ReadonlySet<AppStage>): Promise<number> {
      // 1. コード完全一致
      const codeMatches = await loadCode(bypass.has("code"));
      if (codeMatches.length === 1) return adopt(codeMatches[0] as AppEntry);
      if (codeMatches.length > 1) throw ambiguous(codeMatches);

      // 2-3. 名前(kintone の name= は部分一致。完全一致は大文字小文字を区別して自前で振り分ける)
      const nameMatches = await loadName(bypass.has("name"));
      const exact = nameMatches.filter((a) => a.name === ref);
      const partial = nameMatches.filter((a) => a.name !== ref);
      if (exact.length === 1) return adopt(exact[0] as AppEntry);
      if (exact.length > 1) throw ambiguous(exact);
      if (options.mode === "write" && partial.length > 0) {
        throw new ResolverError(
          "resolver_app_exact_match_required",
          `"${ref}" only partially matches ${partial.length} app(s). Write operations need the exact app ID, code or name; pass one of details.candidates as app.`,
          { kind: "app", ref, candidates: partial.map(appCandidate) },
        );
      }
      if (partial.length === 1) return adopt(partial[0] as AppEntry);
      if (partial.length > 1) throw ambiguous(partial);
      throw new ResolverError(
        "resolver_app_not_found",
        `No app matches "${ref}". Use kintone_apps_search to find the app, then pass its ID as app.`,
        { kind: "app", ref, candidates: [] },
      );
    }

    let appId: number;
    try {
      appId = await attempt(new Set());
    } catch (e) {
      // キャッシュ由来の not_found だけ、読んだ段階を 1 回だけ取り直す。
      if (!(e instanceof ResolverError && e.code === "resolver_app_not_found") || used.size === 0) {
        throw e;
      }
      const bypass = used;
      used = new Set();
      appId = await attempt(bypass);
    }

    const stages = [...used];
    return {
      appId,
      fromCache: stages.length > 0,
      invalidate: async () => {
        if (stages.length === 0) return;
        for (const stage of stages) await cache.deleteApps(stage, ref);
        await cache.deleteFields(appId);
      },
    };
  }

  async function resolveField(
    appId: number,
    ref: string,
    options: ResolveOptions,
  ): Promise<string> {
    if (ref === "") throw new ArgumentError("field reference must not be empty.");
    const useCache = options.useCache !== false;
    let fresh: FieldEntry[] | undefined;
    let fromCache = false;

    async function load(bypass: boolean): Promise<FieldEntry[]> {
      if (fresh) return fresh;
      if (useCache && !bypass) {
        const cached = await cache.getFields(appId);
        if (cached) {
          fromCache = true;
          return cached;
        }
      }
      const response = (await client.get("/k/v1/app/form/fields.json", { app: appId })) as {
        properties?: unknown;
      } | null;
      fresh = fieldEntriesOf(response?.properties);
      await cache.putFields(appId, fresh);
      return fresh;
    }

    function attempt(fields: FieldEntry[]): string {
      // 1. コード完全一致
      if (fields.some((f) => f.code === ref)) return ref;
      // 2-3. label(コード順に並べる)
      const sorted = [...fields].filter((f) => f.label !== "").sort(byCode);
      const exact = sorted.filter((f) => f.label === ref);
      const partial = sorted.filter((f) => f.label !== ref && f.label.includes(ref));
      if (exact.length === 1) return (exact[0] as FieldEntry).code;
      if (exact.length > 1) {
        throw new ResolverError(
          "resolver_field_ambiguous",
          `Field "${ref}" matches ${exact.length} fields by label. Use the field code (see details.candidates).`,
          { kind: "field", ref, candidates: exact.map(fieldCandidate) },
        );
      }
      if (options.mode === "write" && partial.length > 0) {
        throw new ResolverError(
          "resolver_field_exact_match_required",
          `Field "${ref}" only partially matches ${partial.length} label(s). Write operations need the exact field code or label; use one of details.candidates.`,
          { kind: "field", ref, candidates: partial.map(fieldCandidate) },
        );
      }
      if (partial.length === 1) return (partial[0] as FieldEntry).code;
      if (partial.length > 1) {
        throw new ResolverError(
          "resolver_field_ambiguous",
          `Field "${ref}" partially matches ${partial.length} fields. Use the field code (see details.candidates).`,
          { kind: "field", ref, candidates: partial.map(fieldCandidate) },
        );
      }
      throw new ResolverError(
        "resolver_field_not_found",
        `No field matches "${ref}" in app ${appId}. Use kintone_app_describe to see the field codes and labels.`,
        { kind: "field", ref, candidates: [] },
      );
    }

    try {
      return attempt(await load(false));
    } catch (e) {
      if (!(e instanceof ResolverError && e.code === "resolver_field_not_found") || !fromCache) {
        throw e;
      }
      return attempt(await load(true));
    }
  }

  return { resolveApp, resolveField };
}
