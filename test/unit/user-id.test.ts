import { describe, expect, it } from "vitest";
import { userIdFromEmail } from "../../src/auth/user-id";

const BASE64URL_43 = /^[A-Za-z0-9_-]{43}$/;

describe("userIdFromEmail", () => {
  it("同じ email は同じ値", async () => {
    expect(await userIdFromEmail("alice@example.com")).toBe(
      await userIdFromEmail("alice@example.com"),
    );
  });

  it("大文字小文字・前後の空白の違いは同じ値", async () => {
    const base = await userIdFromEmail("alice@example.com");
    expect(await userIdFromEmail("Alice@Example.COM")).toBe(base);
    expect(await userIdFromEmail("  alice@example.com\n")).toBe(base);
  });

  it("別の email は別の値", async () => {
    expect(await userIdFromEmail("alice@example.com")).not.toBe(
      await userIdFromEmail("bob@example.com"),
    );
  });

  it("`:` を含まない 43 文字の base64url で、生の email を含まない", async () => {
    const email = "alice@example.com";
    const id = await userIdFromEmail(email);
    expect(id).toMatch(BASE64URL_43);
    expect(id).not.toContain(":");
    expect(id).not.toContain(email);
    expect(id.toLowerCase()).not.toContain("alice");
  });

  it("`:` を含む email でも `:` を含まない", async () => {
    const id = await userIdFromEmail("a:b@example.com");
    expect(id).toMatch(BASE64URL_43);
    expect(id).not.toContain(":");
  });

  it("SHA-256(小文字 email)の base64url と一致する(固定値)", async () => {
    // printf 'alice@example.com' | openssl dgst -sha256 -binary | basenc --base64url
    const digest = await crypto.subtle.digest(
      "SHA-256",
      new TextEncoder().encode("alice@example.com"),
    );
    const expected = btoa(String.fromCharCode(...new Uint8Array(digest)))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
    expect(await userIdFromEmail("ALICE@example.com")).toBe(expected);
  });

  it("空文字・空白だけは例外", async () => {
    await expect(userIdFromEmail("")).rejects.toThrow();
    await expect(userIdFromEmail("   ")).rejects.toThrow();
  });
});
