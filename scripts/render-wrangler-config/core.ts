// upstream の wrangler.jsonc と環境変数から、デプロイ用の wrangler 設定(フラット)を作る純粋なコア(ADR 0012)。
// 入出力(ファイル・process.env)は main.ts が担う。ここは文字列とオブジェクトだけを扱う(Web 標準 API だけ)。
// 検証エラーには項目名と理由だけを載せ、入力値は決して載せない。

export type ErrorReason = "missing" | "invalid" | "placeholder";
export interface RenderError {
  name: string;
  reason: ErrorReason;
}
export type RenderResult =
  | { ok: true; config: Record<string, unknown>; text: string }
  | { ok: false; errors: RenderError[] };

/** 生成の入力になる環境変数(仕様書 §9)。検証と報告は、この順に行う。 */
export const ENV_NAMES = {
  workerName: "KINTONE_MCP_WORKER_NAME",
  kvNamespaceId: "KINTONE_MCP_KV_NAMESPACE_ID",
  host: "KINTONE_MCP_HOST",
  publicBaseUrl: "KINTONE_MCP_PUBLIC_BASE_URL",
  accessTeamDomain: "KINTONE_MCP_ACCESS_TEAM_DOMAIN",
  accessAud: "KINTONE_MCP_ACCESS_AUD",
  customDomain: "KINTONE_MCP_CUSTOM_DOMAIN",
  oauthPkce: "KINTONE_MCP_OAUTH_PKCE",
} as const;

/** `deploy:check`(dry-run)用のダミー。実在しない値で、Cloudflare には届かない。 */
export const EXAMPLE_ENV: Record<string, string> = {
  KINTONE_MCP_WORKER_NAME: "kintone-mcp-example",
  KINTONE_MCP_KV_NAMESPACE_ID: "00000000000000000000000000000000",
  KINTONE_MCP_HOST: "example.cybozu.com",
  KINTONE_MCP_PUBLIC_BASE_URL: "https://kintone-mcp.example.com",
  KINTONE_MCP_ACCESS_TEAM_DOMAIN: "example-team.cloudflareaccess.com",
  KINTONE_MCP_ACCESS_AUD: "example-access-aud",
  KINTONE_MCP_CUSTOM_DOMAIN: "kintone-mcp.example.com",
};

const KV_BINDING = "OAUTH_KV";
const PLACEHOLDER = /<[^>]*>/;
const WORKER_NAME = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
const KV_ID = /^[0-9a-f]{32}$/i;
const HOSTNAME = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/i;
const PKCE_MODES = new Set(["s256", "off"]);

/** JSONC(コメント・末尾カンマ)を読む。文字列の中の `//` や `/*` は壊さない。 */
export function parseJsonc(text: string): unknown {
  return JSON.parse(removeTrailingCommas(stripComments(text)));
}

/** 位置 i(開き引用符)から、閉じ引用符の次の位置を返す。 */
function skipString(text: string, i: number): number {
  let j = i + 1;
  while (j < text.length && text[j] !== '"') j += text[j] === "\\" ? 2 : 1;
  return j + 1;
}

function stripComments(text: string): string {
  let out = "";
  let i = 0;
  while (i < text.length) {
    const c = text[i] as string;
    const next = text[i + 1];
    if (c === '"') {
      const end = skipString(text, i);
      out += text.slice(i, end);
      i = end;
    } else if (c === "/" && next === "/") {
      while (i < text.length && text[i] !== "\n") i++;
    } else if (c === "/" && next === "*") {
      const end = text.indexOf("*/", i + 2);
      i = end === -1 ? text.length : end + 2;
      out += " ";
    } else {
      out += c;
      i++;
    }
  }
  return out;
}

function removeTrailingCommas(text: string): string {
  let out = "";
  let i = 0;
  while (i < text.length) {
    const c = text[i] as string;
    if (c === '"') {
      const end = skipString(text, i);
      out += text.slice(i, end);
      i = end;
    } else if (c === ",") {
      let j = i + 1;
      while (j < text.length && /\s/.test(text[j] as string)) j++;
      if (text[j] !== "}" && text[j] !== "]") out += c;
      i++;
    } else {
      out += c;
      i++;
    }
  }
  return out;
}

/** https のオリジンだけ。末尾スラッシュ・パス・クエリ・フラグメント・大文字のホストは不可。 */
function isValidBaseUrl(value: string): boolean {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return false;
  }
  return url.protocol === "https:" && url.origin === value;
}

type Validator = (v: string) => boolean;

export function renderWranglerConfig(
  baseText: string,
  env: Record<string, string | undefined>,
): RenderResult {
  const errors: RenderError[] = [];

  /** 値を読む。空・空白のみは「未指定」(必須なら missing)。エラーには項目名と理由だけを積む。 */
  const read = (name: string, valid: Validator, required: boolean): string | undefined => {
    const raw = env[name]?.trim() ?? "";
    if (raw === "") {
      if (required) errors.push({ name, reason: "missing" });
      return undefined;
    }
    if (PLACEHOLDER.test(raw)) errors.push({ name, reason: "placeholder" });
    else if (!valid(raw)) errors.push({ name, reason: "invalid" });
    else return raw;
    return undefined;
  };

  const workerName = read(ENV_NAMES.workerName, (v) => WORKER_NAME.test(v), true);
  const kvId = read(ENV_NAMES.kvNamespaceId, (v) => KV_ID.test(v), true);
  const host = read(ENV_NAMES.host, (v) => HOSTNAME.test(v), true);
  const baseUrl = read(ENV_NAMES.publicBaseUrl, isValidBaseUrl, true);
  const teamDomain = read(ENV_NAMES.accessTeamDomain, (v) => HOSTNAME.test(v), true);
  // AUD の形式は checkConfig と同じく検査しない(空白を含まないことだけ)。
  const aud = read(ENV_NAMES.accessAud, (v) => !/\s/.test(v), true);
  const customDomain = read(ENV_NAMES.customDomain, (v) => HOSTNAME.test(v), false);
  const pkce = read(ENV_NAMES.oauthPkce, (v) => PKCE_MODES.has(v), false);

  let base: Record<string, unknown> | undefined;
  try {
    const parsed = parseJsonc(baseText);
    if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) {
      base = parsed as Record<string, unknown>;
    }
  } catch {
    // 下で報告する(パースエラーの文面に入力の一部が入りうるので、例外は載せない)
  }
  if (!base) errors.push({ name: "wrangler.jsonc", reason: "invalid" });

  if (errors.length > 0 || !base) return { ok: false, errors };

  // env ブロックと routes は持ち越さない(フラットな設定にする。routes は環境変数で決める)。
  const { env: _env, routes: _routes, ...rest } = base;
  const config: Record<string, unknown> = { ...rest, name: workerName };

  const kv = Array.isArray(rest.kv_namespaces)
    ? (rest.kv_namespaces as Record<string, unknown>[])
    : [];
  const hasBinding = kv.some((e) => e.binding === KV_BINDING);
  const replaced = kv.map((e) => (e.binding === KV_BINDING ? { ...e, id: kvId } : e));
  config.kv_namespaces = hasBinding ? replaced : [...replaced, { binding: KV_BINDING, id: kvId }];

  const vars = (typeof rest.vars === "object" && rest.vars !== null ? rest.vars : {}) as Record<
    string,
    unknown
  >;
  config.vars = {
    ...vars,
    KINTONE_HOST: host,
    PUBLIC_BASE_URL: baseUrl,
    ACCESS_TEAM_DOMAIN: teamDomain,
    ACCESS_AUD: aud,
    ...(pkce ? { KINTONE_OAUTH_PKCE: pkce } : {}),
  };

  if (customDomain) {
    config.routes = [{ pattern: customDomain, custom_domain: true }];
    config.workers_dev = false;
  } else {
    config.workers_dev = true;
  }

  return { ok: true, config, text: `${JSON.stringify(config, null, 2)}\n` };
}

/** 標準エラー用。項目名と理由だけ(値は含めない)。 */
export function formatErrors(errors: RenderError[]): string[] {
  const label: Record<ErrorReason, string> = {
    missing: "未設定",
    invalid: "形式が正しくない",
    placeholder: "プレースホルダ(<...>)のまま",
  };
  return errors.map((e) => `${e.name}: ${label[e.reason]}`);
}
