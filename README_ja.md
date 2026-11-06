# kintone MCP

[English](README.md) | 日本語

kintone を AI から操作するための **Remote MCP サーバー**(Cloudflare Workers 上で動く)。MCP クライアント(Claude、Cloudflare MCP Portal など)が OAuth で接続し、**利用者ごとの kintone OAuth で認可された身元**で kintone REST API を呼びます。アプリの検索・フィールド定義の取得・レコードの検索と作成・更新・削除ができます。

> **Go 版(CLI + MCP サーバー、v0.5.6 まで)を使っていた方へ**: この版は作り直しで、**破壊的変更があります**。CLI は廃止されました。→ [Go 版の利用者への移行のお知らせ](#go-版の利用者への移行のお知らせ)

- 利用者ごとの権限: kintone のトークンは利用者ごとの OAuth で、見えるアプリ・触れるレコードはその人の kintone の権限のまま。API トークンの共有はしません
- 利用者の識別と入口の保護: **Cloudflare Access**(self-hosted アプリ)の email で利用者を識別し、認可の入口(`/authorize` と `/callback`)だけを守ります
- 名前でアプリを指定できる: ID のほか、アプリコード・アプリ名(読み取りは部分一致も)で指定できます。書き込みは**完全一致だけ**(取り違えを防ぐため)
- 書き込みの安全策: 書き込みはリトライしない、入力は送る前に検査する、`kintone_record_create` の再送は 5 分間は重複させない
- ステートレスな MCP(Durable Objects なし)。保存先は Cloudflare KV だけ

## ツール

**6 個のツール**を、2 つの toolset(`app`・`record`)に分けて提供します。名前はすべて `kintone_` で始まります。

| toolset | 件数 | ツール |
|---|---|---|
| `app` | 2 | `kintone_apps_search`, `kintone_app_describe` |
| `record` | 4 | `kintone_records_query`, `kintone_record_create`, `kintone_record_update`, `kintone_record_delete` |

| ツール | 区分 | 内容 |
|---|---|---|
| `kintone_apps_search` | ReadOnly | アプリを検索する(ID・コード・名前・スペース)。1 回で 1 ページ |
| `kintone_app_describe` | ReadOnly | アプリの情報とフィールド定義を取得する |
| `kintone_records_query` | ReadOnly | kintone のクエリでレコードを検索する(`limit` / `offset` はクエリに書く。件数も取れる) |
| `kintone_record_create` | WriteNonIdempotent | レコードを 1〜100 件作る。同じ引数の再送(5 分以内)は前回の結果を返す |
| `kintone_record_update` | WriteIdempotent | レコードを更新する(ID、または重複禁止フィールドの値で指定。リビジョンによる排他) |
| `kintone_record_delete` | Destructive | レコードを 1〜100 件削除する |

- 区分は MCP の annotation(`readOnlyHint` / `destructiveHint` / `idempotentHint`)の 4 種です。
- 読み取りツールには、応答のサイズを制限する `max_response_tokens`(既定 50000。超えたら切り詰めず `response_too_large`)が付きます。
- アプリは `app`(ID)または `app_ref`(ID・コード・名前)のどちらか 1 つで指定します。書き込みの `app_ref` は完全一致だけで、部分一致しかなければ候補を返し、書き込みはしません。
- 接続 URL にクエリを付けて、ツールを絞れます: `https://<domain>/mcp?toolsets=app,record`、`?read_only=true`(読み取りのツールだけ)。
- 出力は JSON(text content)。エラーは `isError: true` と `{"schema_version":"1","error":{"code":"...","message":"...","retryable":...}}`。仕様の全体は [docs/specs/kintone-mcp-spec.md](docs/specs/kintone-mcp-spec.md)。
- 対象は 1 つの cybozu.com ホスト(1 デプロイ = 1 ホスト)。ゲストスペースのアプリは扱えません。

## 使い始める(運用者向け)

自分の Cloudflare アカウントと cybozu.com で動かす手順は **[docs/runbook.md](docs/runbook.md)** に、上から順にたどれるように書いてあります。必要なもの:

1. 公開できるホスト名(カスタムドメイン、または `workers.dev`)
2. cybozu.com 共通管理で登録する **OAuth クライアント**(リダイレクト URI は `https://<domain>/callback`)
3. Cloudflare Access の **self-hosted アプリ**(保護するのは `/authorize` と `/callback` の 2 つだけ)
4. Cloudflare の KV namespace と、GitHub Actions の Variables / Secrets

エージェント(Claude Code / Codex)に案内してもらうなら、スキル `kintone-mcp-setup`(`.claude/skills/kintone-mcp-setup/SKILL.md`)を使います。下流リポジトリの作成(fork またはミラー)から案内します。このリポジトリ(upstream)の clone を開いたエージェントに「kintone MCP を自社で動かしたい」と伝えるだけで始められます。下流リポジトリのディレクトリで新しいセッションを始めるときは、最初に次のように送ってください。

```text
このリポジトリは kintone MCP の下流リポジトリです。`.claude/skills/kintone-mcp-setup/SKILL.md` のスキル kintone-mcp-setup に従って、Step 0 から一緒にセットアップを進めてください。秘密値は私が自分のターミナルで入れます。
```

このリポジトリは **upstream** です。`wrangler.jsonc` はダミー値の共有設定のままで、アカウント固有の値(Worker 名・KV id・ドメイン・kintone のホスト・Access の値・client id / secret)は書きません。下流のリポジトリ(fork またはミラー)が、GitHub の Variables / Secrets から `wrangler.deploy.jsonc` を生成してデプロイします(`mise run deploy:render` / `mise run deploy`、設計は [ADR 0012](docs/adr/0012-upstream-downstream-deploy.md))。**Variable `KINTONE_MCP_WORKER_NAME` が空の upstream では、デプロイのジョブは何もしません。**

## しくみ

```
MCP クライアント ── OAuth 2.1 (DCR, PKCE) と MCP ──▶ Cloudflare Worker ──▶ https://<subdomain>.cybozu.com/k/v1/...
                         ※ブラウザが通る /authorize と /callback だけ Cloudflare Access を経由
```

Worker は OAuth の認可サーバーと MCP サーバーを兼ねます(`@cloudflare/workers-oauth-provider` + Hono)。利用者が初めて接続するとき、ブラウザで Access にログインし、kintone の認可画面で同意します。kintone のトークンは Worker が暗号化して KV に保存し、MCP クライアントには Worker が発行した別のトークンを渡します。詳しい設計は [docs/adr/](docs/adr/)、仕様は [docs/specs/kintone-mcp-spec.md](docs/specs/kintone-mcp-spec.md)、実機でしか確かめられない前提は [docs/manual-verification.md](docs/manual-verification.md) にあります。

既知の限界(抜粋。全体は仕様書 §11):

- Access の email は「Access にログインした人」で、kintone で認可したアカウントとは限りません(kintone に持ち主を返す API がないため)。キャッシュなどは grant 単位で分けています
- cybozu.com の IP 制限を使っている環境では、Worker の送信元 IP を許可できず使えない可能性が高いです
- `kintone_records_query` は自動ページングをしません(`offset` は 10,000 まで)
- 冪等キャッシュは KV の結果整合の範囲で動き、ほぼ同時の 2 回の呼び出しは両方実行されえます

## Go 版の利用者への移行のお知らせ

以前の kintone CLI + MCP サーバー(Go 製、v0.5.6 まで)は、この版で**作り直されました**。Cloudflare Workers 上の Remote MCP サーバーに一本化し、**CLI は廃止**しました。

**Go 版はそのまま使えます。**

- ソースコードは **`archive/go` ブランチ・タグ**に保存してあります(`git checkout archive/go`)
- **v0.5.6 の Homebrew(`brew install youyo/tap/kintone`)と ghcr(`ghcr.io/youyo/kintone`)のイメージは、そのまま使えます**。この版への移行を急ぐ必要はありません

Workers 版へ移る方は、次の**破壊的変更**に注意してください。

| 項目 | Go 版(v0.5.6 まで) | Workers 版(この版) |
|---|---|---|
| 提供形態 | CLI(`kintone ...`)+ MCP サーバー(stdio / HTTP)。Homebrew・Docker・`go install` | **Remote MCP サーバーのみ**(Cloudflare Workers)。CLI・stdio・Docker・Homebrew は**廃止** |
| ツール名 | `apps_search` `app_describe` `records_query` `record_create` `record_update` `record_delete` | **`kintone_` が付く**: `kintone_apps_search` `kintone_app_describe` `kintone_records_query` `kintone_record_create` `kintone_record_update` `kintone_record_delete`。引数名は Go 版を踏襲(`app` / `app_ref` など) |
| 出力の形式 | 成功 `{"ok":true,"data":{...}}`、失敗 `{"ok":false,"error":{...}}` | 成功は **`ok` / `data` の包みがなく、kintone のデータそのもの**(加工したものは snake_case)。失敗は **`isError: true`** と `{"schema_version":"1","error":{"code","message","retryable","details"?}}`。`{"ok":...}` を前提にしたクライアントは壊れます |
| エラーコード | 大文字(`KINTONE_UNAUTHORIZED` `RESOLVER_APP_NOT_FOUND` など) | **小文字の snake_case**(`authentication_error` `permission_error` `not_found` `argument_error` `rate_limited` `api_error` `resolver_app_not_found` など。全一覧は仕様書 §6)。Go 版のコードとの対応は仕様書 §6 の表の「Go 版の code」の列。書き込みの `app_ref` が部分一致のみのときの `resolver_app_exact_match_required` など、新しいコードもあります |
| 認証 | API トークン / OAuth(CLI の loopback は廃止済み)/ OIDC(idproxy)。トークンは SQLite・Redis・DynamoDB に保存 | **利用者ごとの kintone OAuth**(Worker が OAuth 認可サーバー。トークンは暗号化して Cloudflare KV)+ **Cloudflare Access の email で利用者を識別**(`/authorize` と `/callback` を Access の self-hosted アプリで保護)。OIDC(idproxy)は廃止 |
| **API トークン** | `X-Cybozu-API-Token`(`KINTONE_API_TOKEN`) | **廃止**。共有の API トークンでは操作できません。各利用者が kintone の OAuth で認可します |
| OAuth のリダイレクト URI | `<外部 URL>/oauth/kintone/callback` | **`<PUBLIC_BASE_URL>/callback`**。cybozu.com の OAuth クライアントの登録のやり直し(または URI の変更)が要ります。scope は Go 版と同じ 6 つ。登録した OAuth クライアントの「利用者の設定」(STEP2)で、使う利用者を許可します |
| 設定項目 | `KINTONE_DOMAIN` `KINTONE_API_TOKEN` `KINTONE_AUTH` `KINTONE_OAUTH_CLIENT_ID` / `_SECRET` / `_REDIRECT_URL` `KINTONE_STORE_*` `KINTONE_MCP_*`(OIDC・署名鍵・Cookie)`KINTONE_CONFIG_PATH` `KINTONE_PROFILE` と `config.toml` | **Cloudflare の Variables / Secrets と wrangler の設定**: `KINTONE_HOST`(`KINTONE_DOMAIN` の後継。ホスト全体 `<subdomain>.cybozu.com`)`PUBLIC_BASE_URL` `ACCESS_TEAM_DOMAIN` `ACCESS_AUD` `KINTONE_CLIENT_ID` / `KINTONE_CLIENT_SECRET`(Secrets)`KINTONE_OAUTH_PKCE`(任意)と KV の `OAUTH_KV`。ファイルの設定・プロファイル・SQLite / Redis / DynamoDB は廃止。一覧は [docs/runbook.md](docs/runbook.md) の設定値の対応表 |
| 名前解決とキャッシュ | キャッシュは利用者に依存しない・TTL 1 年・`cache clear` で無効化。名前検索は最大 100 ページ | キャッシュは**利用者(grant)ごと**・TTL 600 秒。名前検索は最大 10 ページ(超えたら `resolver_app_list_too_large`)。**書き込みの `app_ref` と `update_key_field_ref` は完全一致だけ** |
| 書き込み | リトライしない | リトライしない(同じ)。`kintone_record_create` は 5 分以内の同じ引数の再送を重複させない(冪等キャッシュ) |
| その他 | | `?toolsets=` / `?read_only=true` の URL クエリ、`max_response_tokens`、ゲストスペースは対象外、1 デプロイ = 1 ホスト |

Go 版のコード(`archive/go`)と、旧仕様・旧計画書は、このブランチの履歴から辿れます。移行に関する質問や不具合は、リポジトリの Issue へ。

## 開発

```sh
mise install
mise run install        # 依存をロックファイルどおりに入れる
mise run lint           # Biome + 型検査
mise run test           # unit + e2e (kintone と Access の JWKS は fetch のモック。node-e2e は生成スクリプトの実プロセス + wrangler の dry-run)
mise run build          # wrangler deploy --dry-run
mise run deploy:check   # デプロイ設定の生成と dry-run(ダミー値。Cloudflare には何も変更しない)
```

- 規約: [CLAUDE.md](CLAUDE.md)、計画: [plans/kintone-mcp-roadmap.md](plans/kintone-mcp-roadmap.md)
- `mise run verify:oauth -- https://<domain>/mcp`: デプロイ済みの Worker に対する OAuth の通し確認([docs/manual-verification.md](docs/manual-verification.md))
- デプロイ・secret の登録・KV の作成は、運用者が [docs/runbook.md](docs/runbook.md) に従って行います(テストやエージェントは実行しません)

## 商標・免責

**kintone はサイボウズ株式会社の商標(登録商標)です。本プロジェクトはサイボウズ株式会社とは無関係の、非公式のオープンソースソフトウェアです**(サイボウズ株式会社による提供・承認・保証を受けていません)。その他の会社名・製品名は各社の商標または登録商標です。

## ライセンス

MIT
