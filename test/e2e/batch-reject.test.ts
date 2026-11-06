/**
 * `/mcp` への JSON-RPC バッチ: tools/call を 2 件以上含む POST は 400 invalid_request で拒否し、kintone を呼ばない。
 * 1 件ごとに maxApiCalls が新しく割り当てられ、1 件の kintone 401 で応答全体が HTTP 401 になり他の件の書き込み結果が
 * 失われるのを防ぐ。Worker 全体への HTTP(/register → /authorize → /callback → /token → /mcp)で通す。
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  MCP_URL,
  runAuthorizationFlow,
  setupMocks,
  type TokenSet,
  worker,
} from "../helpers/oauth-flow";

afterEach(() => vi.restoreAllMocks());

const callItem = (id: number) => ({
  jsonrpc: "2.0",
  id,
  method: "tools/call",
  params: { name: "kintone_apps_search", arguments: {} },
});

function postBatch(token: string, body: unknown) {
  return worker.fetch(MCP_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(body),
  });
}

describe("E2E: tools/call を 2 件以上含むバッチの拒否", () => {
  it("tools/call 2 件のバッチは 400 invalid_request。認可後の kintone への通信は 0 回", async () => {
    const { accessKey, calls } = await setupMocks();
    const flow = await runAuthorizationFlow({ accessKey });
    const tokens = (await flow.tokenResponse.json()) as TokenSet;
    const before = calls.length;

    const res = await postBatch(tokens.access_token, [callItem(1), callItem(2)]);
    expect(res.status).toBe(400);
    const body = (await res.json()) as { error: string; error_description: string };
    expect(body.error).toBe("invalid_request");
    expect(calls.length).toBe(before);
  });

  it("tools/call 1 件のバッチは従来どおり実行され、kintone を呼ぶ", async () => {
    const { accessKey, calls } = await setupMocks();
    const flow = await runAuthorizationFlow({ accessKey });
    const tokens = (await flow.tokenResponse.json()) as TokenSet;
    const before = calls.length;

    const res = await postBatch(tokens.access_token, [callItem(1)]);
    expect(res.status).toBe(200);
    expect(calls.length).toBeGreaterThan(before);
  });
});
