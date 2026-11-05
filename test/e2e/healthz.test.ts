import { exports } from "cloudflare:workers";
import { describe, expect, it } from "vitest";

const entry = exports as unknown as { default: Fetcher };

describe("E2E: /healthz", () => {
  it("認証なしで {status:'ok'} を返す", async () => {
    const res = await entry.default.fetch("https://kintone-mcp.test/healthz");
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ status: "ok" });
  });
});
