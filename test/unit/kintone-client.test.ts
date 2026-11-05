import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createKintoneClient,
  KintoneApiError,
  KintoneUnauthorizedError,
} from "../../src/kintone/client";
import { jsonResponse, mockOutboundFetch } from "../helpers/mock-fetch";

const APPS = "GET https://example.cybozu.com/k/v1/apps.json";
const make = () => createKintoneClient({ host: "example.cybozu.com", accessToken: "kt-at-1" });

afterEach(() => vi.restoreAllMocks());

describe("KintoneClient.get", () => {
  it("Authorization: Bearer 付きで https://<host>/k/v1/... を呼び、JSON を返す。クエリは付けた分だけ", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [{ appId: "1" }] }) });
    const body = await make().get("/k/v1/apps.json", { limit: 5, name: "顧客" });
    expect(body).toEqual({ apps: [{ appId: "1" }] });
    const call = m.calls[0];
    expect(call?.headers.get("authorization")).toBe("Bearer kt-at-1");
    const url = new URL(call?.url ?? "");
    expect(url.origin + url.pathname).toBe("https://example.cybozu.com/k/v1/apps.json");
    expect(url.searchParams.get("limit")).toBe("5");
    expect(url.searchParams.get("name")).toBe("顧客");
  });

  it("undefined のクエリは付けない", async () => {
    const m = mockOutboundFetch({ [APPS]: () => jsonResponse({ apps: [] }) });
    await make().get("/k/v1/apps.json", { limit: undefined });
    expect(new URL(m.calls[0]?.url ?? "").search).toBe("");
  });

  it("401 は KintoneUnauthorizedError(クライアントにリフレッシュさせる合図)", async () => {
    mockOutboundFetch({
      [APPS]: () => jsonResponse({ code: "CB_AU01", id: "x", message: "unauthorized" }, 401),
    });
    await expect(make().get("/k/v1/apps.json")).rejects.toBeInstanceOf(KintoneUnauthorizedError);
  });

  it("その他のエラーは status を持つ KintoneApiError(トークンを含めない)", async () => {
    mockOutboundFetch({
      [APPS]: () => jsonResponse({ message: "echo kt-at-1 Bearer kt-at-1" }, 503),
    });
    const err = await make()
      .get("/k/v1/apps.json")
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(KintoneApiError);
    expect((err as KintoneApiError).status).toBe(503);
    expect((err as KintoneApiError).message).not.toContain("kt-at-1");
  });

  it("通信失敗は status 0 の KintoneApiError(トークンを含めない)", async () => {
    mockOutboundFetch({ [APPS]: () => Promise.reject(new Error("boom kt-at-1")) });
    const err = await make()
      .get("/k/v1/apps.json")
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(KintoneApiError);
    expect((err as KintoneApiError).status).toBe(0);
    expect(String((err as Error).message)).not.toContain("kt-at-1");
  });

  it("成功ステータスで JSON でない応答は KintoneApiError", async () => {
    mockOutboundFetch({ [APPS]: () => new Response("<html>", { status: 200 }) });
    await expect(make().get("/k/v1/apps.json")).rejects.toBeInstanceOf(KintoneApiError);
  });
});
