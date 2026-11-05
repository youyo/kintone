import type { ConsentDescription } from "@cloudflare/workers-oauth-provider";

const escapeHtml = (value: string): string =>
  value.replace(/[&<>"']/g, (char) => `&#${char.charCodeAt(0)};`);

export interface ConsentPageInput {
  details: ConsentDescription;
  handle: string;
  /** トークンの送信先として表示する kintone のホスト。 */
  kintoneHost: string;
  /** 検証済みの Cloudflare Access のログイン email(画面に出すだけで保存しない。ADR 0010)。 */
  accessEmail: string;
}

/**
 * 同意画面。クライアント由来の文字列(名前・URL・スコープ)はすべてエスケープする。
 * 表示必須: クライアント名・スコープ・リダイレクト先ホスト(workers-oauth-provider の docs/consent-page.md)。
 */
export function renderConsentPage({
  details,
  handle,
  kintoneHost,
  accessEmail,
}: ConsentPageInput): string {
  const name = escapeHtml(details.clientName);
  const origin = details.clientDomain
    ? `Published by <strong>${escapeHtml(details.clientDomain)}</strong>.`
    : "This app registered itself; its name is not verified.";
  const scopes = details.scope.length
    ? `<ul>${details.scope.map((s) => `<li>${escapeHtml(s)}</li>`).join("")}</ul>`
    : "<p>Access to your kintone account through this server.</p>";
  const loopback = details.redirectIsLoopback
    ? "<p><strong>This sends access to an app on your computer.</strong> Continue only if you just started signing in from it.</p>"
    : "";
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorize ${name}</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:3rem auto;padding:0 1rem;line-height:1.5}button{font-size:1rem;padding:.5rem 1.25rem;margin-right:.5rem}</style>
</head>
<body>
<h1>Allow ${name} to access kintone?</h1>
<p>${origin} Access will be sent to <strong>${escapeHtml(details.redirectHost)}</strong>.</p>
${loopback}
<p>Signed in with Cloudflare Access as <strong>${escapeHtml(accessEmail)}</strong>.</p>
<p>You will be asked to sign in to kintone at <strong>${escapeHtml(kintoneHost)}</strong> next. Choose your own kintone account (the same person as the Access login above): this server cannot check which kintone account you pick.</p>
${scopes}
<form method="post">
  <input type="hidden" name="handle" value="${escapeHtml(handle)}">
  <button type="submit" name="decision" value="approve">Allow</button>
  <button type="submit" name="decision" value="deny">Deny</button>
</form>
</body>
</html>`;
}
