/**
 * 名前解決の KV キャッシュ(仕様書 §10、ADR 0007)。置き場は OAUTH_KV。
 *
 * - キーは **grant 単位**: `kcache:v1:<userId>:<grantId>:appcode:<h>` / `appname:<h>` / `fields:<appId>`。
 *   `<h>` は ref の SHA-256 の先頭 16 文字(16 進)。email 単位(userId だけ)にしない(ADR 0010)。
 * - 値は最小限(アプリ `{appId, code, name}`、フィールド `{code, label, type}`)。TTL 600 秒。
 * - **KV の失敗はツールを止めない**: 例外は握りつぶし(読みは「なし」)、ログには例外名だけを出す
 *   (キー・値・例外の message は出さない)。壊れた値・形が違う値は「なし」として扱う。
 * - KV または grant が分からないときは何もしない実装を返す。
 */

/** キャッシュの寿命(秒)。 */
export const CACHE_TTL_SECONDS = 600;

const KEY_PREFIX = "kcache:v1";

/** MCP トークン `{userId}:{grantId}:{secret}` から取り出した grant の識別子。 */
export interface GrantRef {
  userId: string;
  grantId: string;
}

/** アプリ検索の段階。`code` = コード完全一致の結果、`name` = 名前検索(部分一致)の全ページ。 */
export type AppStage = "code" | "name";

export interface AppEntry {
  appId: string;
  code: string;
  name: string;
}

export interface FieldEntry {
  code: string;
  label: string;
  type: string;
}

export interface ResolverCache {
  getApps(stage: AppStage, ref: string): Promise<AppEntry[] | undefined>;
  putApps(stage: AppStage, ref: string, apps: readonly AppEntry[]): Promise<void>;
  deleteApps(stage: AppStage, ref: string): Promise<void>;
  getFields(appId: number): Promise<FieldEntry[] | undefined>;
  putFields(appId: number, fields: readonly FieldEntry[]): Promise<void>;
  deleteFields(appId: number): Promise<void>;
}

/** ref の SHA-256 の先頭 16 文字(16 進)。 */
async function refHash(ref: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(ref));
  let hex = "";
  for (const b of new Uint8Array(digest).slice(0, 8)) hex += b.toString(16).padStart(2, "0");
  return hex;
}

const grantPrefix = (grant: GrantRef) => `${KEY_PREFIX}:${grant.userId}:${grant.grantId}`;

export async function appCacheKey(grant: GrantRef, stage: AppStage, ref: string): Promise<string> {
  return `${grantPrefix(grant)}:app${stage}:${await refHash(ref)}`;
}

export function fieldsCacheKey(grant: GrantRef, appId: number): string {
  return `${grantPrefix(grant)}:fields:${appId}`;
}

const isString = (v: unknown): v is string => typeof v === "string";
const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

function parseApps(raw: string): AppEntry[] | undefined {
  const parsed: unknown = JSON.parse(raw);
  if (!Array.isArray(parsed)) return undefined;
  const out: AppEntry[] = [];
  for (const item of parsed) {
    if (!isRecord(item) || !isString(item.appId) || !isString(item.code) || !isString(item.name)) {
      return undefined;
    }
    out.push({ appId: item.appId, code: item.code, name: item.name });
  }
  return out;
}

function parseFields(raw: string): FieldEntry[] | undefined {
  const parsed: unknown = JSON.parse(raw);
  if (!Array.isArray(parsed)) return undefined;
  const out: FieldEntry[] = [];
  for (const item of parsed) {
    if (!isRecord(item) || !isString(item.code) || !isString(item.label) || !isString(item.type)) {
      return undefined;
    }
    out.push({ code: item.code, label: item.label, type: item.type });
  }
  return out;
}

const NOOP_CACHE: ResolverCache = {
  getApps: async () => undefined,
  putApps: async () => {},
  deleteApps: async () => {},
  getFields: async () => undefined,
  putFields: async () => {},
  deleteFields: async () => {},
};

/** 例外名だけをログに出す(キー・値・message は出さない)。 */
function warn(op: string, e: unknown): void {
  console.warn(`resolver cache ${op} failed: ${e instanceof Error ? e.name : typeof e}`);
}

export function createResolverCache(
  kv: KVNamespace | undefined,
  grant: GrantRef | undefined,
): ResolverCache {
  if (!kv || !grant) return NOOP_CACHE;

  async function read<T>(
    key: () => Promise<string> | string,
    parse: (raw: string) => T | undefined,
  ): Promise<T | undefined> {
    try {
      const raw = await kv?.get(await key());
      if (raw === null || raw === undefined) return undefined;
      return parse(raw);
    } catch (e) {
      warn("get", e);
      return undefined;
    }
  }

  async function write(key: () => Promise<string> | string, value: unknown): Promise<void> {
    try {
      await kv?.put(await key(), JSON.stringify(value), { expirationTtl: CACHE_TTL_SECONDS });
    } catch (e) {
      warn("put", e);
    }
  }

  async function remove(key: () => Promise<string> | string): Promise<void> {
    try {
      await kv?.delete(await key());
    } catch (e) {
      warn("delete", e);
    }
  }

  return {
    getApps: (stage, ref) => read(() => appCacheKey(grant, stage, ref), parseApps),
    putApps: (stage, ref, apps) =>
      write(
        () => appCacheKey(grant, stage, ref),
        apps.map((a) => ({ appId: a.appId, code: a.code, name: a.name })),
      ),
    deleteApps: (stage, ref) => remove(() => appCacheKey(grant, stage, ref)),
    getFields: (appId) => read(() => fieldsCacheKey(grant, appId), parseFields),
    putFields: (appId, fields) =>
      write(
        () => fieldsCacheKey(grant, appId),
        fields.map((f) => ({ code: f.code, label: f.label, type: f.type })),
      ),
    deleteFields: (appId) => remove(() => fieldsCacheKey(grant, appId)),
  };
}
