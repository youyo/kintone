import {
  AuthorizationError,
  authorizationErrorRedirect,
  CimdFetchError,
} from "@cloudflare/workers-oauth-provider";
import { type Context, Hono } from "hono";
import { type AccessIdentity, AccessJwtError, verifyAccessJwt } from "./auth/access-jwt";
import { renderConsentPage } from "./auth/consent-page";
import {
  buildAuthorizeUrl,
  exchangeCode,
  type KintoneOAuthConfig,
  KintoneOAuthError,
  pkceMode,
} from "./auth/kintone-oauth";
import { codeChallengeS256, generateCodeVerifier } from "./auth/pkce";
import type { GrantProps } from "./auth/props";
import { userIdFromEmail } from "./auth/user-id";
import type { Env } from "./env";
import { createKintoneClient, KintoneApiError, KintoneUnauthorizedError } from "./kintone/client";
import { callbackUrl } from "./urls";

type AppEnv = { Bindings: Env; Variables: { access: AccessIdentity } };

/** beginUpstream の data(ライブラリが暗号化して KV に置き、/callback で返す)。 */
interface UpstreamData {
  /** PKCE の code_verifier。KINTONE_OAUTH_PKCE=off では持たない。kintone にもブラウザにも送らない。 */
  verifier?: string;
  /** /authorize で検証した Access の email。/callback の JWT と一致を確かめる(ADR 0010)。 */
  accessEmail: string;
}

const HTML_HEADERS = { "Content-Type": "text/html; charset=utf-8" } as const;

function kintoneConfig(env: Env): KintoneOAuthConfig {
  return {
    host: env.KINTONE_HOST,
    clientId: env.KINTONE_CLIENT_ID,
    clientSecret: env.KINTONE_CLIENT_SECRET,
    pkce: pkceMode(env.KINTONE_OAUTH_PKCE),
  };
}

function errorPage(status: number, message: string): Response {
  return new Response(
    `<!doctype html><meta charset="utf-8"><title>kintone MCP</title><p>${message.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)}</p>`,
    { status, headers: { ...HTML_HEADERS, "Cache-Control": "no-store" } },
  );
}

function sameEmail(a: string, b: string): boolean {
  return a.trim().toLowerCase() === b.trim().toLowerCase();
}

/**
 * OAuthProvider の defaultHandler。/mcp・/token・/register・/.well-known/* は OAuthProvider が処理し、
 * ここには /authorize・/callback・/healthz などそれ以外が届く。
 * /authorize と /callback は Cloudflare Access の self-hosted アプリで保護し、Worker 側でも JWT を検証する(ADR 0010)。
 */
export const app = new Hono<AppEnv>();

app.get("/healthz", (c) => c.json({ status: "ok" }));

// --- Cloudflare Access の JWT 検証(/authorize と /callback だけ。ADR 0010)---
app.use("/authorize", accessGuard);
app.use("/callback", accessGuard);

async function accessGuard(
  c: Context<AppEnv>,
  next: () => Promise<void>,
): Promise<Response | undefined> {
  try {
    const identity = await verifyAccessJwt(c.req.header("Cf-Access-Jwt-Assertion"), {
      teamDomain: c.env.ACCESS_TEAM_DOMAIN,
      aud: c.env.ACCESS_AUD,
    });
    c.set("access", identity);
  } catch (e) {
    // 理由は応答に出さない。ログには種別だけ(JWT・email は出さない)。
    const reason = e instanceof AccessJwtError ? e.reason : "error";
    console.warn(JSON.stringify({ event: "access_jwt_rejected", reason }));
    // JWKS を取得できないのは運用側の障害(fail closed の 503)、それ以外は利用者側の拒否(403)。
    if (reason === "jwks_unavailable") {
      return new Response("Service Unavailable", {
        status: 503,
        headers: { "Cache-Control": "no-store" },
      });
    }
    return new Response("Forbidden", { status: 403, headers: { "Cache-Control": "no-store" } });
  }
  await next();
  return undefined;
}

// --- GET /authorize: 同意画面 ---
app.get("/authorize", async (c) => {
  const oauth = c.env.OAUTH_PROVIDER;
  try {
    const request = await oauth.parseAuthRequest(c.req.raw);
    const details = await oauth.describeConsent(request);
    const consent = await oauth.beginConsent(request);
    const headers = new Headers(consent.headers);
    headers.set("Content-Type", HTML_HEADERS["Content-Type"]);
    return new Response(
      renderConsentPage({
        details,
        handle: consent.handle,
        kintoneHost: c.env.KINTONE_HOST,
        accessEmail: c.var.access.email,
      }),
      { headers },
    );
  } catch (e) {
    return renderAuthorizationFailure(e);
  }
});

// --- POST /authorize: 承認 → kintone へ / 拒否 → クライアントへ access_denied ---
app.post("/authorize", async (c) => {
  const oauth = c.env.OAUTH_PROVIDER;
  try {
    const form = await c.req.raw.formData();
    const handle = String(form.get("handle") ?? "");
    if (form.get("decision") !== "approve") {
      const denied = await oauth.denyConsent(c.req.raw, handle);
      const headers = new Headers(denied.headers);
      headers.set("Location", denied.redirectTo);
      return new Response(null, { status: 302, headers });
    }
    const approved = await oauth.approveConsent(c.req.raw, handle);
    const cfg = kintoneConfig(c.env);
    // PKCE の verifier を作り、暗号化された data に置く(kintone にもブラウザにも送らない)。
    const verifier = cfg.pkce === "s256" ? generateCodeVerifier() : undefined;
    const data: UpstreamData = {
      accessEmail: c.var.access.email,
      ...(verifier ? { verifier } : {}),
    };
    // 同意後にだけ上流(kintone)へ送る。state は beginUpstream が発行しブラウザに束縛する。
    const { state, headers } = await oauth.beginUpstream(approved.request, {
      data,
      headers: approved.headers,
    });
    headers.set(
      "Location",
      buildAuthorizeUrl(cfg, {
        redirectUri: callbackUrl(c.env),
        state,
        ...(verifier ? { codeChallenge: await codeChallengeS256(verifier) } : {}),
      }),
    );
    return new Response(null, { status: 302, headers });
  } catch (e) {
    return renderAuthorizationFailure(e);
  }
});

// --- GET /callback: kintone のコードをトークンに交換して認可を完了する ---
app.get("/callback", async (c) => {
  const oauth = c.env.OAUTH_PROVIDER;
  let resumed: Awaited<ReturnType<typeof oauth.finishUpstream<UpstreamData>>>;
  try {
    resumed = await oauth.finishUpstream<UpstreamData>(c.req.raw);
  } catch (e) {
    return renderAuthorizationFailure(e);
  }
  const { request, data, headers } = resumed;

  // /authorize と同じ Access の利用者か確かめる。違えばコード交換せず 403(ADR 0010)。
  const email = c.var.access.email;
  if (typeof data?.accessEmail !== "string" || !sameEmail(data.accessEmail, email)) {
    console.warn(JSON.stringify({ event: "access_email_mismatch" }));
    return new Response("Forbidden", { status: 403, headers: { "Cache-Control": "no-store" } });
  }

  const redirectWith = (location: string) => {
    headers.set("Location", location);
    return new Response(null, { status: 302, headers });
  };

  const params = new URL(c.req.url).searchParams;
  if (params.get("error")) {
    return redirectWith(authorizationErrorRedirect(request, "access_denied"));
  }
  const code = params.get("code");
  if (!code) {
    return redirectWith(authorizationErrorRedirect(request, "invalid_request"));
  }

  let props: GrantProps;
  try {
    const tokens = await exchangeCode(kintoneConfig(c.env), {
      code,
      redirectUri: callbackUrl(c.env),
      ...(data.verifier ? { codeVerifier: data.verifier } : {}),
    });
    // 疎通確認: トークンが使えること、利用者に API 権限があること、IP 制限に阻まれないことを認可の時点で確かめる。
    await createKintoneClient({
      host: c.env.KINTONE_HOST,
      accessToken: tokens.accessToken,
    }).get("/k/v1/apps.json", { limit: 1 });
    props = {
      kintoneAccessToken: tokens.accessToken,
      kintoneRefreshToken: tokens.refreshToken,
      kintoneExpiresAt: tokens.expiresAt,
    };
  } catch (e) {
    const transient =
      (e instanceof KintoneOAuthError && e.kind === "temporarily_unavailable") ||
      (e instanceof KintoneApiError && (e.status === 0 || e.status >= 500));
    // ログは種別だけ(トークン・email は出さない)。
    console.warn(
      JSON.stringify({
        event: "kintone_signin_failed",
        kind:
          e instanceof KintoneOAuthError
            ? e.kind
            : e instanceof KintoneUnauthorizedError
              ? "unauthorized"
              : "api_error",
        transient,
      }),
    );
    return redirectWith(
      authorizationErrorRedirect(
        request,
        transient ? "temporarily_unavailable" : "server_error",
        "kintone sign-in failed",
      ),
    );
  }

  const { redirectTo } = await oauth.completeAuthorization({
    request,
    userId: await userIdFromEmail(email),
    metadata: {},
    scope: request.scope,
    props,
    // 同じ userId + 同じ MCP クライアントの旧 grant は新 grant の保存後に失効する(既定だが明示。ADR 0010)。
    revokeExistingGrants: true,
  });
  return redirectWith(redirectTo);
});

/** 検証に失敗したリクエストは、クライアントと redirect_uri の検証が済んでいる場合だけクライアントへ戻す。 */
function renderAuthorizationFailure(e: unknown): Response {
  if (e instanceof AuthorizationError) {
    if (e.redirectTo) {
      return new Response(null, { status: 302, headers: { Location: e.redirectTo } });
    }
    return errorPage(400, e.description);
  }
  if (e instanceof CimdFetchError) {
    return errorPage(400, "This app could not be verified.");
  }
  throw e;
}
