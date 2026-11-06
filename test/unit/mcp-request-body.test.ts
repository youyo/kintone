import { McpServer } from "@modelcontextprotocol/server";
import { createMcpHandler } from "agents/mcp/server";
import { describe, expect, it } from "vitest";
import { z } from "zod";
import { callMcpHandler, MAX_MCP_REQUEST_BODY_BYTES } from "../../src/mcp/request-body";

let runs = 0;
function createEchoServer(): McpServer {
  const server = new McpServer({ name: "echo", version: "0.0.0" });
  server.registerTool(
    "echo_length",
    { description: "d", inputSchema: z.object({ text: z.string() }) },
    async ({ text }) => {
      runs++;
      return {
        content: [{ type: "text" as const, text: JSON.stringify({ length: text.length }) }],
      };
    },
  );
  return server;
}

const handler = () => createMcpHandler(createEchoServer, { route: "/mcp" });

const post = (body: string, headers: Record<string, string> = {}) =>
  new Request("https://kintone-mcp.test/mcp", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json, text/event-stream",
      ...headers,
    },
    body,
  });
const call = (text: string) =>
  JSON.stringify({
    jsonrpc: "2.0",
    id: 1,
    method: "tools/call",
    params: { name: "echo_length", arguments: { text } },
  });

describe("callMcpHandler(MCP の POST 本文の上限)", () => {
  it("上限は 8 MiB（SDK 既定の 4 MiB では 4 MiB のファイルを base64 にした約 5.6 MB を通せない）", () => {
    expect(MAX_MCP_REQUEST_BODY_BYTES).toBe(8 * 1024 * 1024);
  });

  it("4 MiB を超え 8 MiB 以下の本文も処理できる", async () => {
    runs = 0;
    const text = "a".repeat(5 * 1024 * 1024);
    const res = await callMcpHandler(handler(), post(call(text)));
    expect(res.status).toBe(200);
    expect(await res.text()).toContain(`\\"length\\":${text.length}`);
    expect(runs).toBe(1);
  });

  it("8 MiB を超える本文は、実行せずに 413", async () => {
    runs = 0;
    const res = await callMcpHandler(handler(), post(call("a".repeat(MAX_MCP_REQUEST_BODY_BYTES))));
    expect(res.status).toBe(413);
    expect(runs).toBe(0);
  });

  it("Content-Length が上限超なら本文を読まずに 413", async () => {
    runs = 0;
    const res = await callMcpHandler(
      handler(),
      post(call("x"), { "Content-Length": String(MAX_MCP_REQUEST_BODY_BYTES + 1) }),
    );
    expect(res.status).toBe(413);
    expect(runs).toBe(0);
  });

  it("壊れた JSON は SDK の応答（4xx）に任せる。500 にしない", async () => {
    const res = await callMcpHandler(handler(), post("{not json"));
    expect(res.status).toBeGreaterThanOrEqual(400);
    expect(res.status).toBeLessThan(500);
    expect(res.status).not.toBe(413);
  });

  it("POST 以外はそのまま渡す", async () => {
    const res = await callMcpHandler(
      handler(),
      new Request("https://kintone-mcp.test/mcp", { method: "GET" }),
    );
    expect(res.status).toBeLessThan(500);
  });

  describe("tools/call を 2 件以上含む JSON-RPC バッチは拒否する", () => {
    const item = (id: number, method = "tools/call", text = "x") => ({
      jsonrpc: "2.0",
      id,
      method,
      params: { name: "echo_length", arguments: { text } },
    });

    it("tools/call が 2 件のバッチは、実行せずに 400 invalid_request", async () => {
      runs = 0;
      const res = await callMcpHandler(handler(), post(JSON.stringify([item(1), item(2)])));
      expect(res.status).toBe(400);
      expect(res.headers.get("Content-Type")).toContain("application/json");
      const body = (await res.json()) as { error: string; error_description: string };
      expect(body.error).toBe("invalid_request");
      expect(body.error_description).toContain("tools/call");
      expect(runs).toBe(0);
    });

    it("tools/call が 100 件でも 400。ほかのメソッドが混ざっていても tools/call が 2 件なら 400", async () => {
      runs = 0;
      const many = Array.from({ length: 100 }, (_, i) => item(i + 1));
      expect((await callMcpHandler(handler(), post(JSON.stringify(many)))).status).toBe(400);
      const mixed = [item(1), item(2, "tools/list"), item(3)];
      expect((await callMcpHandler(handler(), post(JSON.stringify(mixed)))).status).toBe(400);
      expect(runs).toBe(0);
    });

    it("tools/call が 1 件だけのバッチは従来どおり通す", async () => {
      runs = 0;
      const res = await callMcpHandler(
        handler(),
        post(JSON.stringify([item(1), item(2, "tools/list")])),
      );
      expect(res.status).toBe(200);
      expect(await res.text()).toContain('\\"length\\":1');
      expect(runs).toBe(1);
    });

    it("tools/call 以外だけのバッチは(2 件以上でも)この検査の対象外", async () => {
      const res = await callMcpHandler(
        handler(),
        post(JSON.stringify([item(1, "tools/list"), item(2, "tools/list")])),
      );
      expect(res.status).toBe(200);
    });

    it("バッチでない通常の tools/call は通す。配列でない本文は method を検査しない", async () => {
      runs = 0;
      const res = await callMcpHandler(handler(), post(call("abc")));
      expect(res.status).toBe(200);
      expect(runs).toBe(1);
    });

    it("要素が object でない・method が文字列でない配列でも落ちない(数えない)", async () => {
      const res = await callMcpHandler(
        handler(),
        post(JSON.stringify([null, 1, "tools/call", { method: 1 }, item(1)])),
      );
      expect(res.status).not.toBe(500);
    });
  });
});
