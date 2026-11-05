/**
 * Access の email から利用者 ID を作る(ADR 0010)。
 * `userId = base64url(SHA-256(UTF-8(trim して小文字にした email)))`(43 文字、`:` を含まない)。
 * workers-oauth-provider は userId に `:` を含められず、userId は MCP トークンと KV のキーに入る。
 * ハッシュにすることで、クライアントに渡るトークンと KV のキーに生の email が載らない。
 */
export async function userIdFromEmail(email: string): Promise<string> {
  const normalized = email.trim().toLowerCase();
  if (normalized === "") throw new Error("email is empty");
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(normalized));
  let binary = "";
  for (const b of new Uint8Array(digest)) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
