import { exportJWK, generateKeyPair, type JWK, SignJWT } from "jose";

/** テスト用の Access 設定(vitest.config.ts の束縛と同じダミー値)。 */
export const TEAM_DOMAIN = "kintone-mcp-test.cloudflareaccess.com";
export const ACCESS_AUD = "test-access-aud";
export const CERTS_URL = `GET https://${TEAM_DOMAIN}/cdn-cgi/access/certs`;

export interface TestKey {
  kid: string;
  privateKey: CryptoKey;
  publicJwk: JWK;
}

export async function createTestKey(kid: string): Promise<TestKey> {
  const { publicKey, privateKey } = await generateKeyPair("RS256", { extractable: true });
  const publicJwk = await exportJWK(publicKey);
  return { kid, privateKey, publicJwk: { ...publicJwk, kid, alg: "RS256", use: "sig" } };
}

export interface SignOptions {
  teamDomain?: string;
  aud?: string | string[];
  /** email claim の値。文字列以外(数値など)や空文字も渡せる(不正 JWT のテスト用)。 */
  email?: unknown;
  sub?: string;
  /** 発行時刻(エポック秒)。 */
  iat?: number;
  /** 期限(エポック秒)。 */
  exp?: number;
  /** claim を丸ごと外す。 */
  omit?: ("email" | "sub" | "exp")[];
}

export async function signAccessJwt(key: TestKey, opts: SignOptions = {}): Promise<string> {
  const omit = new Set(opts.omit ?? []);
  const iat = opts.iat ?? Math.floor(Date.now() / 1000);
  const payload: Record<string, unknown> = { type: "app" };
  if (!omit.has("email")) payload.email = "email" in opts ? opts.email : "alice@example.com";
  const jwt = new SignJWT(payload)
    .setProtectedHeader({ alg: "RS256", kid: key.kid })
    .setIssuer(`https://${opts.teamDomain ?? TEAM_DOMAIN}`)
    .setAudience(opts.aud ?? ACCESS_AUD)
    .setIssuedAt(iat);
  if (!omit.has("sub")) jwt.setSubject(opts.sub ?? "access-user-1");
  if (!omit.has("exp")) jwt.setExpirationTime(opts.exp ?? iat + 300);
  return jwt.sign(key.privateKey);
}

/** alg を HS256 にして、JWKS の公開鍵の内容を秘密として署名した JWT(アルゴリズム混同攻撃のテスト用)。 */
export async function signHs256Jwt(key: TestKey): Promise<string> {
  const secret = new TextEncoder().encode(JSON.stringify(key.publicJwk));
  const iat = Math.floor(Date.now() / 1000);
  return new SignJWT({ email: "alice@example.com" })
    .setProtectedHeader({ alg: "HS256", kid: key.kid })
    .setIssuer(`https://${TEAM_DOMAIN}`)
    .setAudience(ACCESS_AUD)
    .setSubject("access-user-1")
    .setIssuedAt(iat)
    .setExpirationTime(iat + 300)
    .sign(secret);
}

export function jwksResponse(...keys: TestKey[]): Response {
  return new Response(JSON.stringify({ keys: keys.map((k) => k.publicJwk) }), {
    headers: { "Content-Type": "application/json" },
  });
}
