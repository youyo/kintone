import { exports, env as testEnv } from "cloudflare:workers";
import { expect } from "vitest";
import { clearJwksCache } from "../../src/auth/access-jwt";
import type { Env } from "../../src/env";
import indexHandler from "../../src/index";
import {
  CERTS_URL,
  createTestKey,
  jwksResponse,
  type SignOptions,
  signAccessJwt,
  type TestKey,
} from "./access";
import { jsonResponse, mockOutboundFetch, type RecordedCall } from "./mock-fetch";

export const ORIGIN = "https://kintone-mcp.test";
export const MCP_URL = `${ORIGIN}/mcp`;
export const CLIENT_REDIRECT = "https://client.example/cb";
export const KINTONE_ORIGIN = "https://example.cybozu.com";
export const KINTONE_TOKEN = `POST ${KINTONE_ORIGIN}/oauth2/token`;
export const KINTONE_APPS = `GET ${KINTONE_ORIGIN}/k/v1/apps.json`;
export const DEFAULT_EMAIL = "alice@example.com";

// wrangler.jsonc の main (src/index.ts) の default export。Worker 全体への HTTP 入口(SELF.fetch 相当)。
const entry = exports as unknown as { default: Fetcher };

let envOverrides: Partial<Record<keyof Env, unknown>> | undefined;
const ctx = { waitUntil() {}, passThroughOnException() {} } as unknown as ExecutionContext;

/**
 * env の一部(例 KINTONE_OAUTH_PKCE)を差し替えて Worker 全体を呼ぶ。afterEach で resetEnv() すること。
 * 差し替えがないときは wrangler.jsonc の main の default export を HTTP 入口として呼ぶ。
 */
export function useEnv(overrides: Partial<Record<keyof Env, unknown>>) {
  envOverrides = overrides;
}
export function resetEnv() {
  envOverrides = undefined;
}

export const worker = {
  fetch: (url: string, init: RequestInit = {}) => {
    if (envOverrides) {
      const merged = { ...testEnv, ...envOverrides } as unknown as Env;
      return indexHandler.fetch(new Request(url, { redirect: "manual", ...init }), merged, ctx);
    }
    return entry.default.fetch(url, { redirect: "manual", ...init });
  },
};

export interface MockOptions {
  kintoneToken?: (call: RecordedCall) => Response | Promise<Response>;
  kintoneApps?: (call: RecordedCall) => Response | Promise<Response>;
  jwks?: (call: RecordedCall) => Response | Promise<Response>;
  /** 追加の宛先("METHOD https://host/path" → ハンドラ)。M03 以降の読み取りツールの kintone REST など。 */
  extra?: Record<string, (call: RecordedCall) => Response | Promise<Response>>;
}

/**
 * kintone(OAuth・REST)と Access の JWKS の outbound fetch モックを用意する。
 * Access の JWT はテスト用の鍵で署名する(accessKey)。登録していない宛先への通信は例外になる。
 */
export async function setupMocks(opts: MockOptions = {}) {
  clearJwksCache();
  const accessKey = await createTestKey("access-kid-1");
  const kintoneToken =
    opts.kintoneToken ??
    (() =>
      jsonResponse({
        access_token: "kintone-at-1",
        refresh_token: "kintone-rt-1",
        token_type: "Bearer",
        expires_in: 3600,
      }));
  const kintoneApps =
    opts.kintoneApps ??
    ((call: RecordedCall) =>
      call.headers.get("authorization")?.startsWith("Bearer kintone-at-")
        ? jsonResponse({ apps: [{ appId: "1", code: "CRM", name: "顧客管理" }] })
        : jsonResponse({ code: "CB_AU01", message: "unauthorized" }, 401));
  const mocks = mockOutboundFetch({
    [CERTS_URL]: opts.jwks ?? (() => jwksResponse(accessKey)),
    [KINTONE_TOKEN]: kintoneToken,
    [KINTONE_APPS]: kintoneApps,
    ...(opts.extra ?? {}),
  });
  return { accessKey, ...mocks };
}

/** `Cf-Access-Jwt-Assertion` ヘッダー(テスト用の鍵で署名した JWT)。 */
export async function accessHeaders(
  key: TestKey,
  opts: SignOptions = {},
): Promise<Record<string, string>> {
  return { "Cf-Access-Jwt-Assertion": await signAccessJwt(key, opts) };
}

/** Set-Cookie を Cookie ヘッダーに畳む最小のジャー。 */
export class CookieJar {
  private cookies = new Map<string, string>();
  update(res: Response) {
    for (const line of res.headers.getSetCookie()) {
      const [pair = ""] = line.split(";");
      const idx = pair.indexOf("=");
      const name = pair.slice(0, idx).trim();
      const value = pair.slice(idx + 1).trim();
      if (/;\s*max-age=0/i.test(line) || value === "") this.cookies.delete(name);
      else this.cookies.set(name, value);
    }
  }
  header(): string {
    return [...this.cookies].map(([k, v]) => `${k}=${v}`).join("; ");
  }
}

function base64url(bytes: Uint8Array): string {
  return btoa(String.fromCharCode(...bytes))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

export async function pkcePair() {
  const verifier = base64url(crypto.getRandomValues(new Uint8Array(32)));
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return { verifier, challenge: base64url(new Uint8Array(digest)) };
}

export async function registerClient(clientName = "Test MCP Client"): Promise<string> {
  const res = await worker.fetch(`${ORIGIN}/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      client_name: clientName,
      redirect_uris: [CLIENT_REDIRECT],
      token_endpoint_auth_method: "none",
      grant_types: ["authorization_code", "refresh_token"],
      response_types: ["code"],
    }),
  });
  expect(res.status).toBe(201);
  const body = (await res.json()) as { client_id: string };
  return body.client_id;
}

/** MCP クライアントが送る /authorize の URL。 */
export async function authorizeUrl(clientId: string, state = "client-state-1") {
  const { verifier, challenge } = await pkcePair();
  const url = new URL(`${ORIGIN}/authorize`);
  url.search = new URLSearchParams({
    response_type: "code",
    client_id: clientId,
    redirect_uri: CLIENT_REDIRECT,
    state,
    code_challenge: challenge,
    code_challenge_method: "S256",
    resource: MCP_URL,
  }).toString();
  return { url: url.toString(), verifier };
}

export interface TokenSet {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
}

export interface FlowOptions {
  accessKey: TestKey;
  /** /authorize の JWT の email(既定 alice@example.com)。 */
  email?: string;
  /** /callback の JWT の email(既定は email と同じ)。不一致のテスト用。 */
  callbackEmail?: string;
  /** 登録済みのクライアントを使う(省略時は新しく DCR する)。 */
  clientId?: string;
  clientName?: string;
}

/** /register → /authorize(同意)→ kintone(モック)→ /callback → /token を通しで行う。各リクエストに Access の JWT を付ける。 */
export async function runAuthorizationFlow(opts: FlowOptions) {
  const email = opts.email ?? DEFAULT_EMAIL;
  const clientId = opts.clientId ?? (await registerClient(opts.clientName));
  const { url, verifier } = await authorizeUrl(clientId);
  const jar = new CookieJar();

  const consent = await worker.fetch(url, {
    headers: await accessHeaders(opts.accessKey, { email }),
  });
  expect(consent.status).toBe(200);
  jar.update(consent);
  const html = await consent.clone().text();
  const handle = /name="handle" value="([^"]+)"/.exec(html)?.[1] ?? "";
  expect(handle).not.toBe("");

  const kintoneRedirect = await worker.fetch(`${ORIGIN}/authorize`, {
    method: "POST",
    headers: {
      Cookie: jar.header(),
      "Content-Type": "application/x-www-form-urlencoded",
      ...(await accessHeaders(opts.accessKey, { email })),
    },
    body: new URLSearchParams({ handle, decision: "approve" }),
  });
  jar.update(kintoneRedirect);
  const kintoneLocation = kintoneRedirect.headers.get("Location") ?? "";
  const state = new URL(kintoneLocation, ORIGIN).searchParams.get("state") ?? "";

  const callbackResponse = await worker.fetch(
    `${ORIGIN}/callback?code=kintone-code&state=${encodeURIComponent(state)}`,
    {
      headers: {
        Cookie: jar.header(),
        ...(await accessHeaders(opts.accessKey, { email: opts.callbackEmail ?? email })),
      },
    },
  );
  const back = callbackResponse.headers.get("Location") ?? "";
  const clientCode = new URL(back, ORIGIN).searchParams.get("code") ?? "";

  const tokenResponse = await worker.fetch(`${ORIGIN}/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code: clientCode,
      client_id: clientId,
      redirect_uri: CLIENT_REDIRECT,
      code_verifier: verifier,
      resource: MCP_URL,
    }),
  });

  return {
    clientId,
    email,
    consent,
    html,
    kintoneRedirect,
    kintoneLocation,
    callbackResponse,
    tokenResponse,
    clientCode,
    state,
  };
}

export function refreshRequest(clientId: string, refreshToken: string) {
  return worker.fetch(`${ORIGIN}/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({
      grant_type: "refresh_token",
      refresh_token: refreshToken,
      client_id: clientId,
    }),
  });
}

export function mcpRequest(
  accessToken: string | null,
  method: string,
  params: Record<string, unknown> = {},
  url: string = MCP_URL,
): Promise<Response> {
  return worker.fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
      ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
    },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method, params }),
  });
}

export interface RpcResponse {
  result?: {
    tools?: {
      name: string;
      description?: string;
      annotations?: { readOnlyHint?: boolean; destructiveHint?: boolean };
      inputSchema?: unknown;
    }[];
    content?: { type: string; text: string }[];
    isError?: boolean;
  };
  error?: { code: number; message: string };
}

/** tools/call の最初の text content を取り出す。 */
export function firstText(rpc: RpcResponse): string {
  return rpc.result?.content?.[0]?.text ?? "";
}

export async function readRpc(res: Response): Promise<RpcResponse> {
  const text = await res.text();
  const data = text.startsWith("{")
    ? text
    : text
        .split("\n")
        .find((l) => l.startsWith("data:"))
        ?.slice(5)
        .trim();
  return JSON.parse(data ?? "null");
}

export const MODERN_VERSION = "2026-07-28";
