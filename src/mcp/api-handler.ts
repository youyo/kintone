import type { OAuthResourceContext } from "@cloudflare/workers-oauth-provider";
import { createMcpHandler } from "agents/mcp/server";
import { grantRefFromAccessToken, markKintoneStale } from "../auth/kintone-stale";
import type { GrantProps } from "../auth/props";
import type { Env } from "../env";
import { createKintoneClient } from "../kintone/client";
import { MCP_PATH, protectedResourceMetadataUrl } from "../urls";
import { callMcpHandler } from "./request-body";
import { createMcpServer } from "./server";

/**
 * OAuthProvider の apiHandler。`ctx.props` は grant の props(kintone のトークン入り)。
 * ステートレス: リクエストごとに McpServer を作り、Durable Objects もセッションも使わない。
 *
 * kintone が 401 を返したら、MCP の応答も HTTP 401 + invalid_token にして、
 * MCP クライアントにリフレッシュ(tokenExchangeCallback 経由で kintone も更新)させる。
 * そのとき grant に「kintone の更新が必要」という印を置く(保存した期限より先に kintone 側で失効した場合でも、
 * 次のリフレッシュで期限に関係なく kintone を更新させるため。kintone-stale.ts)。
 */
export function createMcpApiHandler(): Required<Pick<ExportedHandler<Env>, "fetch">> {
  return {
    async fetch(request, env, ctx) {
      const oauthCtx = ctx as Partial<OAuthResourceContext<GrantProps>> &
        ExecutionContext<GrantProps>;
      const props = oauthCtx.props;
      let kintoneUnauthorized = false;

      const handler = createMcpHandler(
        () =>
          createMcpServer({
            getClient: () =>
              createKintoneClient({
                host: env.KINTONE_HOST,
                accessToken: props.kintoneAccessToken,
              }),
            onKintoneUnauthorized: () => {
              kintoneUnauthorized = true;
            },
          }),
        { route: MCP_PATH },
      );

      const response = await callMcpHandler(handler, request);

      // ステートレスの tools/call は 1 回の応答で終わる。kintone の 401 を HTTP の 401 に変換するため、
      // POST の応答は(SSE でも)ここで読み切ってから判定する。
      if (request.method !== "POST") return response;
      const body = await response.text();
      if (kintoneUnauthorized) {
        // 印はクライアントが直後に叩く /token より先に書き終える(waitUntil にしない)。失敗しても 401 は返す。
        let staleMarked = false;
        const ref = grantRefFromAccessToken(oauthCtx.auth?.token);
        if (ref) {
          try {
            await markKintoneStale(env.OAUTH_KV, ref.userId, ref.grantId);
            staleMarked = true;
          } catch {
            staleMarked = false;
          }
        }
        // 診断ログ(値は出さない): props 上の kintone トークンの残り秒数。負なら保存期限どおりの失効。
        console.warn(
          JSON.stringify({
            event: "kintone_unauthorized",
            kintone_remaining_seconds: Math.floor((props.kintoneExpiresAt - Date.now()) / 1000),
            stale_marked: staleMarked,
          }),
        );
        return new Response(JSON.stringify({ error: "invalid_token" }), {
          status: 401,
          headers: {
            "Content-Type": "application/json",
            "WWW-Authenticate": `Bearer realm="OAuth", error="invalid_token", error_description="kintone access token was rejected; refresh the token", resource_metadata="${protectedResourceMetadataUrl(env)}"`,
          },
        });
      }
      return new Response(body, {
        status: response.status,
        statusText: response.statusText,
        headers: response.headers,
      });
    },
  };
}
