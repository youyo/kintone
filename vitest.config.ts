import { cloudflareTest } from "@cloudflare/vitest-plugin";
import { defineConfig } from "vitest/config";

// テスト専用の束縛。実在しないダミー値のみ(秘密情報ではない)。
const testBindings = {
  KINTONE_HOST: "example.cybozu.com",
  PUBLIC_BASE_URL: "https://kintone-mcp.test",
  KINTONE_CLIENT_ID: "test-kintone-client-id",
  KINTONE_CLIENT_SECRET: "test-kintone-client-secret",
  ACCESS_TEAM_DOMAIN: "kintone-mcp-test.cloudflareaccess.com",
  ACCESS_AUD: "test-access-aud",
};

// workerd のテストは、各ファイルの最初のテストで Worker の起動とモジュールの読み込みが走る。
// ファイルを並列に流すと、その起動が CPU を取り合い、CPU の少ない CI では 5 秒の既定を超える。
// 起動コストの揺れを吸収するための余裕で、個々のテストの重さの許容ではない(通常は各テスト 1 秒未満)。
const WORKERD_TEST_TIMEOUT_MS = 30_000;

function project(name: string, include: string) {
  return {
    plugins: [
      cloudflareTest({
        wrangler: { configPath: "./wrangler.jsonc" },
        miniflare: { bindings: testBindings },
      }),
    ],
    test: {
      name,
      include: [include],
      testTimeout: WORKERD_TEST_TIMEOUT_MS,
      hookTimeout: WORKERD_TEST_TIMEOUT_MS,
    },
  };
}

// verify:oauth のループバック受信(node:http)は workerd では動かないので、Node 環境のプロジェクトで実機の HTTP を確かめる。
const nodeProject = {
  test: { name: "node", environment: "node", include: ["test/node/**/*.test.ts"] },
};

// 生成スクリプトなどを実プロセスとして起動する E2E 用。workerd ではなく Node 環境で動かす(M01 は空)。
const nodeE2eProject = {
  test: { name: "node-e2e", environment: "node", include: ["test/node-e2e/**/*.test.ts"] },
};

export default defineConfig({
  test: {
    projects: [
      project("unit", "test/unit/**/*.test.ts"),
      project("e2e", "test/e2e/**/*.test.ts"),
      nodeProject,
      nodeE2eProject,
    ],
  },
});
