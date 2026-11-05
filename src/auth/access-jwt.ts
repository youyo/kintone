import { createLocalJWKSet, errors, type JSONWebKeySet, jwtVerify } from "jose";

/**
 * Cloudflare Access の `Cf-Access-Jwt-Assertion` を検証する(ADR 0010)。
 * Access が前段で保護していても、Worker 側で署名・iss・aud・期限を必ず検証する(多層防御)。
 * JWKS は https://{team}/cdn-cgi/access/certs から取得し、isolate 内でキャッシュする。
 * JWT・email はエラーメッセージにもログにも出さない。
 */

export interface AccessJwtConfig {
  /** 例 your-team.cloudflareaccess.com */
  teamDomain: string;
  /** Access アプリケーションの AUD タグ */
  aud: string;
}

export interface AccessIdentity {
  email: string;
  subject: string;
}

export type AccessJwtErrorReason = "missing" | "invalid" | "jwks_unavailable";

export class AccessJwtError extends Error {
  constructor(
    readonly reason: AccessJwtErrorReason,
    message: string,
  ) {
    super(message);
    this.name = "AccessJwtError";
  }
}

/** JWKS のキャッシュ期間。 */
const JWKS_TTL_MS = 5 * 60 * 1000;
/** 未知の kid で JWKS を取り直してよくなるまでの、前回取得からの最短間隔。 */
const JWKS_REFETCH_MIN_INTERVAL_MS = 60 * 1000;
/** JWKS 取得のタイムアウト。超えたら jwks_unavailable(fail closed)。 */
const JWKS_FETCH_TIMEOUT_MS = 5000;

interface CachedJwks {
  jwks: JSONWebKeySet;
  fetchedAt: number;
}
const cache = new Map<string, CachedJwks>();
/** 実行中の取得。同時のリクエストは同じ取得を待つ。失敗したら消して次のリクエストで取り直す。 */
const inflight = new Map<string, Promise<CachedJwks>>();

export function clearJwksCache(): void {
  cache.clear();
  inflight.clear();
}

function fetchJwks(teamDomain: string): Promise<CachedJwks> {
  const running = inflight.get(teamDomain);
  if (running) return running;
  const p = doFetchJwks(teamDomain).finally(() => {
    if (inflight.get(teamDomain) === p) inflight.delete(teamDomain);
  });
  inflight.set(teamDomain, p);
  return p;
}

async function doFetchJwks(teamDomain: string): Promise<CachedJwks> {
  let res: Response;
  try {
    res = await fetch(`https://${teamDomain}/cdn-cgi/access/certs`, {
      signal: AbortSignal.timeout(JWKS_FETCH_TIMEOUT_MS),
    });
  } catch {
    throw new AccessJwtError("jwks_unavailable", "Access JWKS unreachable");
  }
  if (!res.ok) {
    throw new AccessJwtError("jwks_unavailable", `Access JWKS returned ${res.status}`);
  }
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    throw new AccessJwtError("jwks_unavailable", "Access JWKS is not JSON");
  }
  if (!body || !Array.isArray((body as JSONWebKeySet).keys)) {
    throw new AccessJwtError("jwks_unavailable", "Access JWKS is malformed");
  }
  const entry = { jwks: body as JSONWebKeySet, fetchedAt: Date.now() };
  cache.set(teamDomain, entry);
  return entry;
}

async function getJwks(teamDomain: string): Promise<CachedJwks> {
  const hit = cache.get(teamDomain);
  if (hit && Date.now() - hit.fetchedAt < JWKS_TTL_MS) return hit;
  return fetchJwks(teamDomain);
}

function verifyWith(token: string, cfg: AccessJwtConfig, jwks: JSONWebKeySet) {
  return jwtVerify(token, createLocalJWKSet(jwks), {
    issuer: `https://${cfg.teamDomain}`,
    audience: cfg.aud,
    algorithms: ["RS256"],
    requiredClaims: ["exp"],
  });
}

export async function verifyAccessJwt(
  token: string | null | undefined,
  cfg: AccessJwtConfig,
): Promise<AccessIdentity> {
  if (!token) throw new AccessJwtError("missing", "Cf-Access-Jwt-Assertion is missing");

  let result: Awaited<ReturnType<typeof verifyWith>>;
  try {
    const cached = await getJwks(cfg.teamDomain);
    try {
      result = await verifyWith(token, cfg, cached.jwks);
    } catch (e) {
      // 未知の kid は鍵ローテーションの可能性があるので JWKS を取り直す。ただし前回の取得から
      // 60 秒未満なら取り直さない(署名不一致や不正な JWT を連投されても Access への通信が増えない)。
      if (
        e instanceof errors.JWKSNoMatchingKey &&
        Date.now() - cached.fetchedAt >= JWKS_REFETCH_MIN_INTERVAL_MS
      ) {
        result = await verifyWith(token, cfg, (await fetchJwks(cfg.teamDomain)).jwks);
      } else {
        throw e;
      }
    }
  } catch (e) {
    if (e instanceof AccessJwtError) throw e;
    throw new AccessJwtError("invalid", "Access JWT verification failed");
  }

  const email = result.payload.email;
  if (typeof email !== "string" || email.trim() === "" || !result.payload.sub) {
    throw new AccessJwtError("invalid", "Access JWT has no identity");
  }
  return { email, subject: result.payload.sub };
}
