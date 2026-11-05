# ADR 0009: logbutler の規約のうち、採用するものと後回しにするもの

- 状態: 提案(M01〜M05 で段階的に採用)
- 日付: 2026-11-05
- 関連: logbutler `CLAUDE.md`、logbutler ADR 0008 / 0010 / 0011 / 0005、ADR 0010

## 決定

| 規約(logbutler の出典) | 判断 | いつ | 理由・メモ |
|---|---|---|---|
| mise(node/pnpm 固定、`test`/`test:unit`/`test:e2e`/`lint`/`fmt`/`build`/`dev`)、vitest + `@cloudflare/vitest-plugin` + Biome + TypeScript strict | 採用 | M01 | グローバル規約。logbutler の `mise.toml` / `vitest.config.ts` と同じ形 |
| 依存は正確なバージョン固定(`^` `~` なし)。**logbutler の `package.json` と同じバージョン**を使う(`@cloudflare/workers-oauth-provider` 1.2.3、`agents` 0.28.1、`@modelcontextprotocol/server` 2.3.1、`hono` 4.13.13、`zod` 4.6.7、`wrangler` 4.151.0 ほか) | 採用 | M01 | 2 週間ルールは logbutler が適用済み(2026-10-28〜29 時点)。`yaml`(conventions 用)は入れない。`@modelcontextprotocol/sdk` と `client` は verify:oauth・テストで使うなら入れる |
| 起動時の設定チェック(`checkConfig`、`/healthz` 503、それ以外 500、値を出さない)(ADR 0010) | 採用 | M01 | 項目は仕様書 §9 |
| `verify:oauth`(実環境の OAuth 確認ツール) | 採用 | M01 | kintone の Basic 認証・PKCE に合わせて移植。V01〜V05 の確認に使う |
| 出力・エラー形式(ADR 0004) | 採用(拡張あり) | M02 | ADR 0006 |
| 応答サイズの上限 `max_response_tokens`(ADR 0007) | 採用 | M02 | ADR 0008 |
| ツール登録の仕組み(`defineTool`、4 区分の annotation、`maxApiCalls`、tools/list のスナップショット) | 採用 | M02 | 6 ツールでも、スナップショットは引数名の退行を防ぐ。Go の tools/list は baseline として使わない(出力形式が変わるため)。Go の引数名は spec の表で固定し、テストで突き合わせる |
| `toolsets` / `read_only` の URL クエリ(ADR 0008) | 採用(小さく) | M02 | toolset は `app` / `record` の 2 つだけ。`read_only=true` が実用上の価値(読み取り専用の接続を作れる)。不正値・綴り違い・0 ツールは 400 |
| `timezone` 引数 | 後回し(不要) | - | 日付を解釈するツールがない |
| 冪等キャッシュ `idem:`(ADR 0005)| 採用 | M04 | `kintone_record_create` だけ(WriteNonIdempotent)。KV の `idem:<userId>:<grantId>:...`(grant 単位。ADR 0010)。`record_update`/`record_delete` は対象外 |
| upstream / 下流モデルと `render-wrangler-config`(ADR 0011) | 採用 | M05 | `wrangler.jsonc` は共有のダミー値。`KINTONE_MCP_*` 環境変数から `wrangler.deploy.jsonc`(gitignore)を生成。`deploy:check` は ダミー値の dry-run。M01 では `wrangler.jsonc` と gitignore の枠だけ作る |
| `.github/workflows/ci.yml`(アクションはコミット SHA 固定、`mise-action` のバージョン固定)、`deploy.yml` | 採用 | ci.yml の最小版(lint・test)は M01、`deploy:check` と deploy.yml は M05 | SHA は実装時に `gh api` で確かめる(logbutler のコメントの手順)。logbutler の SHA をそのまま写さず、更新確認してから固定 |
| `docs/runbook.md`、`docs/manual-verification.md` | 採用 | M05 / 今回(後者) | 後者は今回作成 |
| 下流セットアップ用スキル(`logbutler-setup`) | 後回し | M06 以降 | 利用者が増えてから |
| `test:live`(実環境 E2E) | 後回し | M05 以降 | 実 kintone への書き込みを伴う。ユーザーの許可が要る |
| 状態一覧のキャッシュ・親子課題・分析系・digest・conventions 等の Backlog 固有機能 | 対象外 | - | kintone に対応物なし |
| Cloudflare Access 連携(`access-jwt.ts`、`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD`、`/callback` の email 一致) | **採用**(`/authorize` と `/callback` だけ。Managed OAuth は使わない) | M01 | logbutler の M01 の実装(git 履歴 `8dac72c^:src/auth/access-jwt.ts`)を移植。logbutler が M06 で外した理由とは別の構成(ADR 0010) |
| ユーザー識別子(`backlogUserId`) | 置き換え | M01 | Access の email の SHA-256(base64url)。キャッシュ・冪等・stale 印は `<userId>:<grantId>`(ADR 0010) |

## 帰結
- 採用を M02 以降に回したものは、M01 のコード構成が後から差し込める形になっていること(`src/mcp/registry.ts` を M02 で追加)だけ意識する。
- 後回しにしたものを入れるときは、この表を更新し、理由を ADR に残す。
