/** PKCE (RFC 7636)。verifier は kintone にもブラウザにも送らず、beginUpstream の data に暗号化して置く(ADR 0004)。 */

function base64url(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** 32 バイトの乱数を base64url にした 43 文字の code_verifier。 */
export function generateCodeVerifier(): string {
  return base64url(crypto.getRandomValues(new Uint8Array(32)));
}

/** code_challenge_method=S256 の challenge。 */
export async function codeChallengeS256(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return base64url(new Uint8Array(digest));
}
