/**
 * 冪等キャッシュ(仕様書 §10、ADR 0009 / 0011)。WriteNonIdempotent のツール(`kintone_record_create`)の重複実行を防ぐ。
 *
 * MCP クライアントは接続断のあとに同じ呼び出しを再送しうる。同じキーの呼び出しは、成功した最初の結果を
 * 5 分間 KV(OAUTH_KV)に置き、2 回目以降は kintone を一度も呼ばずにそれを返す。best-effort: KV は結果整合で
 * 排他制御もないので、ほぼ同時の 2 回の呼び出しは両方実行されうる(Durable Objects を使わない方針のため受け入れる)。
 *
 * - キー: `idem:<userId>:<grantId>:<toolName>:<explicit|hash>:<sha256 16 進>`(**grant 単位**。ADR 0010)。
 *   `userId` は Access の email のハッシュで、生の email・MCP トークンの secret 部分はキーに入らない。
 *   明示キー(`_meta.idempotency_key` → 引数 `idempotency_key`)も生では入れず、SHA-256 にする。
 * - 保存するのは `{args: <引数ハッシュ>, result: <成功した結果の JSON>}` だけ。エラーは保存しない。
 *   同じ明示キーで別の引数が来たら、前回の結果を返さず `argument_error`(別の書き込みを黙って捨てない)。
 * - KV の失敗は呼び出しを止めない(読めなければ実行、書けなくても結果は返す)。ログには例外名だけ。
 */
import { ArgumentError } from "../errors";
import type { GrantRef } from "../resolver/cache";

/** 結果を置いておく時間(秒)。KV の最小 TTL(60 秒)以上。 */
export const IDEMPOTENCY_TTL_SECONDS = 300;
/** 明示キーの最大長(UTF-8 のバイト数)。 */
export const MAX_EXPLICIT_KEY_BYTES = 200;

/** キーをソートした JSON(オブジェクトのキー順に依存しないハッシュのため)。 */
export function canonicalJson(value: unknown): string {
  const sort = (v: unknown): unknown => {
    if (Array.isArray(v)) return v.map(sort);
    if (v && typeof v === "object") {
      return Object.fromEntries(
        Object.entries(v as Record<string, unknown>)
          .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
          .map(([k, x]) => [k, sort(x)]),
      );
    }
    return v;
  };
  return JSON.stringify(sort(value));
}

async function sha256Hex(text: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * 明示キー: `params._meta.idempotency_key` → 引数の `idempotency_key` の順。
 * 空文字・文字列でない値は無いものとして扱う。UTF-8 で 200 バイトを超えたら argument_error。
 */
export function explicitKeyFrom(
  meta: Record<string, unknown> | undefined,
  rawArgs: unknown,
): string | undefined {
  const fromArgs =
    rawArgs && typeof rawArgs === "object"
      ? (rawArgs as Record<string, unknown>).idempotency_key
      : undefined;
  for (const candidate of [meta?.idempotency_key, fromArgs]) {
    if (typeof candidate !== "string" || candidate === "") continue;
    const bytes = new TextEncoder().encode(candidate).length;
    if (bytes > MAX_EXPLICIT_KEY_BYTES) {
      throw new ArgumentError(
        `idempotency_key must be at most ${MAX_EXPLICIT_KEY_BYTES} bytes in UTF-8, got ${bytes}.`,
      );
    }
    return candidate;
  }
  return undefined;
}

/**
 * KV のキーと、引数のハッシュ(検証済みの引数の canonical JSON の SHA-256)。
 * 明示キーがなければキーの値に引数ハッシュを使う(同じ引数の再送だけが当たる)。
 */
export async function idempotencyCacheKey(
  grant: GrantRef,
  toolName: string,
  explicit: string | undefined,
  args: unknown,
): Promise<{ key: string; argsHash: string }> {
  const argsHash = await sha256Hex(canonicalJson(args));
  const prefix = `idem:${grant.userId}:${grant.grantId}:${toolName}`;
  const key =
    explicit !== undefined
      ? `${prefix}:explicit:${await sha256Hex(explicit)}`
      : `${prefix}:hash:${argsHash}`;
  return { key, argsHash };
}

export interface IdempotencyDeps {
  kv: KVNamespace;
  grant: GrantRef;
}

export interface IdempotencyInput {
  toolName: string;
  /** 明示キー(なければ引数ハッシュで判定)。 */
  explicit: string | undefined;
  /** 検証済みの引数。 */
  args: unknown;
}

interface Stored {
  args: string;
  result: string;
}

function parseStored(raw: string): Stored | undefined {
  const parsed: unknown = JSON.parse(raw);
  if (typeof parsed !== "object" || parsed === null) return undefined;
  const { args, result } = parsed as Record<string, unknown>;
  return typeof args === "string" && typeof result === "string" ? { args, result } : undefined;
}

/** 例外名だけをログに出す(キー・値・message は出さない)。 */
function warn(op: string, e: unknown): void {
  console.warn(`idempotency cache ${op} failed: ${e instanceof Error ? e.name : typeof e}`);
}

/**
 * 保存済みの結果(JSON 文字列)があればそれを返し、なければ `run` を実行して成功した結果だけ保存する。
 * `run` が投げたエラーは保存しない(そのまま投げる)。KV の失敗は止めない。
 */
export async function withIdempotency(
  deps: IdempotencyDeps,
  input: IdempotencyInput,
  run: () => Promise<string>,
): Promise<string> {
  const { key, argsHash } = await idempotencyCacheKey(
    deps.grant,
    input.toolName,
    input.explicit,
    input.args,
  );

  let stored: Stored | undefined;
  try {
    const raw = await deps.kv.get(key);
    if (raw !== null && raw !== undefined) stored = parseStored(raw);
  } catch (e) {
    warn("get", e);
  }
  if (stored) {
    if (stored.args !== argsHash) {
      throw new ArgumentError(
        "idempotency_key was already used with different arguments. Use a new idempotency_key for a different request.",
      );
    }
    return stored.result;
  }

  const result = await run();
  try {
    await deps.kv.put(key, JSON.stringify({ args: argsHash, result } satisfies Stored), {
      expirationTtl: IDEMPOTENCY_TTL_SECONDS,
    });
  } catch (e) {
    warn("put", e);
  }
  return result;
}
