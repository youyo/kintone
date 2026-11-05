/**
 * 「この grant の kintone トークンは更新が必要」という印(OAUTH_KV の `kintone-stale:<userId>:<grantId>`)。
 *
 * /mcp のツール呼び出しで kintone が 401 を返したら api-handler が置き、次の MCP トークン更新で
 * tokenExchangeCallback が期限に関係なく kintone を更新する(成功したら消す。ADR 0005)。
 * 保存期限(kintoneExpiresAt)より先に kintone 側でトークンが失効したときに、更新を待たずに回復するため。
 *
 * 値は目印の "1" だけ。トークンは置かない(トークンは grant の props にだけある)。
 * 印のキーは grant 単位(email = userId 単位にしない。ADR 0010)。
 * KV は結果整合なので、別の colo からは印がすぐ見えないことがある(V15)。
 */

/** 印の寿命。MCP クライアントは 401 の直後に更新するので短くてよい。 */
export const KINTONE_STALE_TTL_SECONDS = 15 * 60;

export function kintoneStaleKey(userId: string, grantId: string): string {
  return `kintone-stale:${userId}:${grantId}`;
}

export async function markKintoneStale(
  kv: KVNamespace,
  userId: string,
  grantId: string,
): Promise<void> {
  await kv.put(kintoneStaleKey(userId, grantId), "1", {
    expirationTtl: KINTONE_STALE_TTL_SECONDS,
  });
}

export async function isKintoneStale(
  kv: KVNamespace,
  userId: string,
  grantId: string,
): Promise<boolean> {
  return (await kv.get(kintoneStaleKey(userId, grantId))) !== null;
}

export async function clearKintoneStale(
  kv: KVNamespace,
  userId: string,
  grantId: string,
): Promise<void> {
  await kv.delete(kintoneStaleKey(userId, grantId));
}

/**
 * MCP アクセストークン(`{userId}:{grantId}:{secret}`。workers-oauth-provider の d.ts の Token に明記)から
 * userId と grantId を取り出す。形式が違えば undefined。トークン自体はログに出さないこと。
 */
export function grantRefFromAccessToken(
  token: string | undefined,
): { userId: string; grantId: string } | undefined {
  if (!token) return undefined;
  const parts = token.split(":");
  if (parts.length !== 3) return undefined;
  const [userId, grantId] = parts;
  if (!userId || !grantId) return undefined;
  return { userId, grantId };
}
