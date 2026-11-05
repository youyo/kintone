/**
 * kintone OAuth 2.0 (上流 IdP。Worker が OAuth クライアント。ADR 0004)。
 * - 認可: https://{host}/oauth2/authorization
 * - トークン: POST https://{host}/oauth2/token(クライアント認証は HTTP Basic。フォームに client_secret は入れない)
 *
 * トークン・client_secret をログやエラーメッセージに出さない。
 */

/** 要求する scope は 6 つ(ADR 0004 決定 3)。今のツールが使うのは先頭の 3 つ。 */
export const KINTONE_SCOPES = [
  "k:app_record:read",
  "k:app_record:write",
  "k:app_settings:read",
  "k:app_settings:write",
  "k:file:read",
  "k:file:write",
] as const;

export interface KintoneOAuthConfig {
  /** 例 example.cybozu.com */
  host: string;
  clientId: string;
  clientSecret: string;
  /** `off` では code_challenge / code_verifier を送らない(KINTONE_OAUTH_PKCE=off。V04)。 */
  pkce: "s256" | "off";
}

export function pkceMode(value: string | undefined): "s256" | "off" {
  return value?.trim() === "off" ? "off" : "s256";
}

export interface KintoneTokens {
  accessToken: string;
  refreshToken: string;
  /** アクセストークンの期限(エポックミリ秒)。 */
  expiresAt: number;
}

export type KintoneOAuthErrorKind =
  /** コードやリフレッシュトークンが無効(失効・取り消し)。再認可が必要。 */
  | "invalid_grant"
  /** クライアント ID / シークレットの誤り(運用者の設定ミス)。grant は失効させない。 */
  | "invalid_client"
  /** 一時的な障害(5xx・429・通信失敗)。リトライ可能。 */
  | "temporarily_unavailable"
  /** 上記以外の 4xx(invalid_request など)。再試行で直らない互換性・設定の問題。 */
  | "rejected"
  /** 成功応答だが形式が想定と違う。 */
  | "invalid_response";

export class KintoneOAuthError extends Error {
  constructor(
    readonly kind: KintoneOAuthErrorKind,
    message: string,
  ) {
    super(message);
    this.name = "KintoneOAuthError";
  }
}

export function buildAuthorizeUrl(
  cfg: KintoneOAuthConfig,
  params: { redirectUri: string; state: string; codeChallenge?: string },
): string {
  const url = new URL(`https://${cfg.host}/oauth2/authorization`);
  url.searchParams.set("response_type", "code");
  url.searchParams.set("client_id", cfg.clientId);
  url.searchParams.set("redirect_uri", params.redirectUri);
  url.searchParams.set("scope", KINTONE_SCOPES.join(" "));
  url.searchParams.set("state", params.state);
  if (cfg.pkce === "s256" && params.codeChallenge) {
    url.searchParams.set("code_challenge", params.codeChallenge);
    url.searchParams.set("code_challenge_method", "S256");
  }
  return url.toString();
}

export function exchangeCode(
  cfg: KintoneOAuthConfig,
  params: { code: string; redirectUri: string; codeVerifier?: string },
  now: number = Date.now(),
): Promise<KintoneTokens> {
  const grant: Record<string, string> = {
    grant_type: "authorization_code",
    code: params.code,
    redirect_uri: params.redirectUri,
  };
  if (cfg.pkce === "s256" && params.codeVerifier) grant.code_verifier = params.codeVerifier;
  return requestTokens(cfg, grant, undefined, now);
}

/**
 * リフレッシュトークンでアクセストークンを更新する。
 * 応答に refresh_token があれば差し替え、なければ元のものを維持する(両対応。ADR 0005)。
 */
export function refreshTokens(
  cfg: KintoneOAuthConfig,
  refreshToken: string,
  now: number = Date.now(),
): Promise<KintoneTokens> {
  return requestTokens(
    cfg,
    { grant_type: "refresh_token", refresh_token: refreshToken },
    refreshToken,
    now,
  );
}

function basicAuthorization(cfg: KintoneOAuthConfig): string {
  const bytes = new TextEncoder().encode(`${cfg.clientId}:${cfg.clientSecret}`);
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return `Basic ${btoa(binary)}`;
}

async function readErrorCode(res: Response): Promise<string | undefined> {
  try {
    const body = (await res.json()) as { error?: unknown } | null;
    return typeof body?.error === "string" ? body.error : undefined;
  } catch {
    return undefined;
  }
}

async function requestTokens(
  cfg: KintoneOAuthConfig,
  grant: Record<string, string>,
  fallbackRefreshToken: string | undefined,
  now: number,
): Promise<KintoneTokens> {
  let res: Response;
  try {
    res = await fetch(`https://${cfg.host}/oauth2/token`, {
      method: "POST",
      headers: {
        Authorization: basicAuthorization(cfg),
        "Content-Type": "application/x-www-form-urlencoded",
      },
      body: new URLSearchParams(grant),
    });
  } catch {
    throw new KintoneOAuthError("temporarily_unavailable", "kintone token endpoint unreachable");
  }

  if (res.status === 429 || res.status >= 500) {
    throw new KintoneOAuthError(
      "temporarily_unavailable",
      `kintone token endpoint returned ${res.status}`,
    );
  }
  if (!res.ok) {
    // 本文はエラーコード(error)だけを読む。error_description などは載せない(トークンを含みうる)。
    const code = await readErrorCode(res);
    if (res.status === 400 || res.status === 401) {
      if (code === "invalid_client" || code === "unauthorized_client") {
        throw new KintoneOAuthError(
          "invalid_client",
          `kintone token endpoint rejected the client (${res.status})`,
        );
      }
      // 本文の error が明示的に invalid_grant のときだけ失効扱い。本文なし・JSON でない・error なしは
      // 前段のプロキシや WAF の一時的な拒否かもしれないので、下の rejected にして grant を残す。
      if (code === "invalid_grant") {
        throw new KintoneOAuthError(
          "invalid_grant",
          `kintone token endpoint rejected the grant (${res.status})`,
        );
      }
    }
    throw new KintoneOAuthError(
      "rejected",
      `kintone token endpoint rejected the request (${res.status})`,
    );
  }

  let body: unknown;
  try {
    body = await res.json();
  } catch {
    throw new KintoneOAuthError("invalid_response", "kintone token response is not JSON");
  }
  const b = body as Record<string, unknown> | null;
  const accessToken = typeof b?.access_token === "string" ? b.access_token : undefined;
  const refreshToken =
    typeof b?.refresh_token === "string" && b.refresh_token !== ""
      ? b.refresh_token
      : fallbackRefreshToken;
  const expiresIn = typeof b?.expires_in === "number" ? b.expires_in : undefined;
  if (!accessToken || !refreshToken || expiresIn === undefined) {
    throw new KintoneOAuthError("invalid_response", "kintone token response is missing fields");
  }
  // 診断ログ: 値は出さず、寿命の秒数とローテーションの有無だけ(V01・V02 の確認用)。
  console.log(
    JSON.stringify({
      event: "kintone_token",
      grant_type: grant.grant_type,
      expires_in: expiresIn,
      ...(fallbackRefreshToken === undefined
        ? {}
        : { refresh_token_rotated: refreshToken !== fallbackRefreshToken }),
    }),
  );
  return { accessToken, refreshToken, expiresAt: now + expiresIn * 1000 };
}
