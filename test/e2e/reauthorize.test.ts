import { afterEach, describe, expect, it, vi } from "vitest";
import {
  mcpRequest,
  readRpc,
  refreshRequest,
  registerClient,
  runAuthorizationFlow,
  setupMocks,
  type TokenSet,
} from "../helpers/oauth-flow";

afterEach(() => {
  vi.restoreAllMocks();
});

const tokensOf = async (flow: { tokenResponse: Response }) =>
  (await flow.tokenResponse.json()) as TokenSet;

describe("E2E 8: 再認可は同じ email・同じ MCP クライアントの旧 grant を失効させて置き換える(ADR 0010)", () => {
  it("同じ email・同じクライアントで 2 回認可すると、1 回目のリフレッシュは invalid_grant、2 回目は有効", async () => {
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const first = await runAuthorizationFlow({ accessKey, clientId, email: "alice@example.com" });
    const t1 = await tokensOf(first);
    const second = await runAuthorizationFlow({ accessKey, clientId, email: "alice@example.com" });
    const t2 = await tokensOf(second);
    expect(second.tokenResponse.status).toBe(200);

    const old = await refreshRequest(clientId, t1.refresh_token);
    expect(old.status).toBe(400);
    expect(((await old.json()) as { error: string }).error).toBe("invalid_grant");

    const fresh = await refreshRequest(clientId, t2.refresh_token);
    expect(fresh.status).toBe(200);
    const t3 = (await fresh.json()) as TokenSet;
    const call = await mcpRequest(t3.access_token, "tools/call", {
      name: "kintone_apps_search",
      arguments: {},
    });
    expect(call.status).toBe(200);
    expect((await readRpc(call)).result?.isError).toBeFalsy();
  });

  it("旧 grant の MCP アクセストークンも使えなくなる", async () => {
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const t1 = await tokensOf(await runAuthorizationFlow({ accessKey, clientId }));
    await runAuthorizationFlow({ accessKey, clientId });
    const res = await mcpRequest(t1.access_token, "tools/list");
    expect(res.status).toBe(401);
  });

  it("別の email の grant は影響を受けない(同じクライアントでも)", async () => {
    const { accessKey } = await setupMocks();
    const clientId = await registerClient();
    const alice1 = await tokensOf(
      await runAuthorizationFlow({ accessKey, clientId, email: "alice@example.com" }),
    );
    const bob = await tokensOf(
      await runAuthorizationFlow({ accessKey, clientId, email: "bob@example.com" }),
    );
    // alice が再認可しても bob の grant は残る
    await runAuthorizationFlow({ accessKey, clientId, email: "alice@example.com" });
    expect((await refreshRequest(clientId, bob.refresh_token)).status).toBe(200);
    expect((await refreshRequest(clientId, alice1.refresh_token)).status).toBe(400);
  });

  it("別のクライアント(別の DCR 登録)で同じ email が認可しても、既存 grant は残る", async () => {
    const { accessKey } = await setupMocks();
    const clientA = await registerClient("Client A");
    const clientB = await registerClient("Client B");
    const a = await tokensOf(await runAuthorizationFlow({ accessKey, clientId: clientA }));
    const b = await tokensOf(await runAuthorizationFlow({ accessKey, clientId: clientB }));
    expect((await refreshRequest(clientA, a.refresh_token)).status).toBe(200);
    expect((await refreshRequest(clientB, b.refresh_token)).status).toBe(200);
  });
});
