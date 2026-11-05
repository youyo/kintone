/**
 * 起動時の設定チェック(仕様書 §9)。必須の vars / secrets / KV が欠けている、
 * またはプレースホルダ(`<...>`)のまま・形式が違う場合に、項目名だけを返す。値は結果にも例外にも載せない。
 * `ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` も必須で、欠けたまま認可を通すフォールバックは作らない(ADR 0010)。
 */
export interface ConfigCheckResult {
  ok: boolean;
  /** 未設定(未定義・空・KV でない)の項目名。 */
  missing: string[];
  /** 設定されているが使えない(プレースホルダのまま・形式違い)項目名。 */
  invalid: string[];
}

const PLACEHOLDER = /<[^>]*>/;
/** スキーム・パス・空白・ポートを含まないホスト名(ドットを 1 つ以上含む)。 */
const HOST_NAME = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/i;
const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);
const PKCE_MODES = new Set(["s256", "off"]);

function isBlank(value: unknown): boolean {
  return typeof value !== "string" || value.trim() === "";
}

function isKvNamespace(value: unknown): boolean {
  return (
    typeof value === "object" &&
    value !== null &&
    typeof (value as { get?: unknown }).get === "function" &&
    typeof (value as { put?: unknown }).put === "function"
  );
}

/** PUBLIC_BASE_URL は https のオリジンだけ(ローカル開発の loopback は http も可)。パス・クエリ・フラグメントは不可。 */
function isValidBaseUrl(value: string): boolean {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return false;
  }
  const schemeOk =
    url.protocol === "https:" || (url.protocol === "http:" && LOOPBACK_HOSTS.has(url.hostname));
  return schemeOk && url.pathname === "/" && url.search === "" && url.hash === "";
}

export function checkConfig(env: Record<string, unknown>): ConfigCheckResult {
  const missing: string[] = [];
  const invalid: string[] = [];

  if (!isKvNamespace(env.OAUTH_KV)) missing.push("OAUTH_KV");

  const text = (name: string, valid?: (v: string) => boolean) => {
    const value = env[name];
    if (isBlank(value)) {
      missing.push(name);
      return;
    }
    const v = (value as string).trim();
    if (PLACEHOLDER.test(v) || (valid && !valid(v))) invalid.push(name);
  };
  text("KINTONE_HOST", (v) => HOST_NAME.test(v));
  text("PUBLIC_BASE_URL", isValidBaseUrl);
  text("KINTONE_CLIENT_ID");
  text("KINTONE_CLIENT_SECRET");
  text("ACCESS_TEAM_DOMAIN", (v) => HOST_NAME.test(v));
  text("ACCESS_AUD");

  // 任意。設定されているなら s256 / off のどちらか(空文字も不正)。
  const pkce = env.KINTONE_OAUTH_PKCE;
  if (pkce !== undefined && (typeof pkce !== "string" || !PKCE_MODES.has(pkce.trim()))) {
    invalid.push("KINTONE_OAUTH_PKCE");
  }

  return { ok: missing.length === 0 && invalid.length === 0, missing, invalid };
}
