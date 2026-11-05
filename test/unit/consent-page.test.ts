import type { ConsentDescription } from "@cloudflare/workers-oauth-provider";
import { describe, expect, it } from "vitest";
import { renderConsentPage } from "../../src/auth/consent-page";

const details: ConsentDescription = {
  clientId: "client-1",
  clientName: "Test MCP Client",
  clientUri: "https://client.example",
  redirectUri: "https://client.example/cb",
  redirectHost: "client.example",
  redirectIsLoopback: false,
  scope: [],
};

function render(overrides: Partial<ConsentDescription> = {}, accessEmail = "alice@example.com") {
  return renderConsentPage({
    details: { ...details, ...overrides },
    handle: "handle-1",
    kintoneHost: "example.cybozu.com",
    accessEmail,
  });
}

describe("renderConsentPage", () => {
  it("クライアント名・送信先ホスト・kintone のホスト・フォームのハンドルを表示する", () => {
    const html = render();
    expect(html).toContain("Test MCP Client");
    expect(html).toContain("client.example");
    expect(html).toContain("example.cybozu.com");
    expect(html).toContain('name="handle" value="handle-1"');
    expect(html).toContain('value="approve"');
    expect(html).toContain('value="deny"');
  });

  it("Access のログイン email を表示し、次の kintone ログインで同じ本人のアカウントを選ぶ旨を添える", () => {
    const html = render();
    expect(html).toContain("alice@example.com");
    expect(html).toContain("Cloudflare Access");
    expect(html).toMatch(/same person|your own|same account/i);
  });

  it("クライアント由来の文字列(名前・ホスト・scope)をエスケープする", () => {
    const payload = "<script>alert(1)</script>";
    const html = render({
      clientName: payload,
      redirectHost: payload,
      clientDomain: payload,
      scope: [payload, '"><img src=x onerror=1>'],
    });
    expect(html).not.toContain("<script>");
    expect(html).not.toContain("<img");
    expect(html).toContain("&#60;script&#62;alert(1)&#60;/script&#62;");
  });

  it("Access の email も(<script> を含む email 風の文字列でも)エスケープする", () => {
    const html = render({}, '"><script>alert(2)</script>@evil.example');
    expect(html).not.toContain("<script>");
    expect(html).toContain("&#60;script&#62;alert(2)&#60;/script&#62;");
  });

  it("ループバックのリダイレクト先には警告を出す", () => {
    expect(render({ redirectIsLoopback: true })).toContain("on your computer");
    expect(render()).not.toContain("on your computer");
  });

  it("署名済みドメインがあれば Published by として表示、なければ自己申告と明記する", () => {
    expect(render({ clientDomain: "client.example" })).toContain("Published by");
    expect(render()).toContain("not verified");
  });
});
