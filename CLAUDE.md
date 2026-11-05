# CLAUDE.md

kintone MCP(kintone 用 Remote MCP サーバー。Cloudflare Workers + KV + Hono)の作業規約。
Go 製 CLI + MCP サーバー(v0.5.6)は `archive/go` ブランチ・タグに保存済みで、このブランチ(`feat/workers-rewrite`)で Workers 版へ作り直している。
仕様は `docs/specs/kintone-mcp-spec.md`、計画は `plans/`(ロードマップ `kintone-mcp-roadmap.md` と各マイルストーン)、設計判断は `docs/adr/`、実機でしか確かめられない前提は `docs/manual-verification.md`(V01〜 / P01〜)。
姉妹プロジェクト `logbutler`(Backlog 用。`/Users/youyo/src/github.com/youyo/logbutler`)と同じ構成。

## 絶対に守ること

- **TDD**: 失敗するテストを先に書き、`mise run test:unit` / `mise run test:e2e` で期待どおりの理由で失敗するのを確認してから実装する。テストを弱める・skip・削除しない。バグ修正は再現テストが先。
- **mise**: ランタイムとコマンドは `mise.toml` で管理する。`npm test` や `vitest` や `pnpm` を直接叩かず `mise run <task>` を使う。新しいコマンドが要るなら先にタスクへ追加する(依存を変えたときだけ `mise run install:update` で `pnpm-lock.yaml` を更新する)。
- 完了条件は `mise run test`(unit + e2e)と `mise run lint` が通ること(`mise run build` も通ること)。
- **Durable Objects / McpAgent は使わない**。MCP はステートレス(`createMcpHandler`)。
- **kintone のトークンは grant の `props` にだけ置く**。ログ・エラーメッセージ・別の KV キー・MCP の応答に出さない。クライアント ID / シークレットも同様。
- **kintone API と Access はモックのみ**。unit / e2e テストで実 kintone・実 Access に通信しない(`test/helpers/mock-fetch.ts` は未登録の宛先への通信を例外にする。Access の JWKS も fetch のモック、JWT はテスト用の鍵で署名する)。
- 依存は公開から 2 週間以上たった正確なバージョンに固定する(`^` `~` を使わない)。`wrangler.jsonc` の `compatibility_date` も、固定した workerd が対応する日付まで。
- コミット・push・PR・リモート作成・デプロイは、ユーザーの指示があるときだけ。`archive/go` の push もユーザーが行う。
- **デプロイは実行しない**: `wrangler deploy`(`--dry-run` 以外)・`wrangler secret put`・KV の作成は実行しない。アカウント固有の値(Worker 名・KV id・ドメイン・kintone ホスト・Access の値・client id/secret)は追跡ファイルに書かない(`wrangler.jsonc` はダミー値の共有設定)。
- GitHub Actions は**コミット SHA で固定**する(`.github/workflows/ci.yml`)。更新時は `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` で実在を確かめる。

## 認証・利用者の識別子(ADR 0001〜0005・0010)

- 利用者の識別子は **Cloudflare Access の email(必須)**。`/authorize`(GET・POST)と `/callback` だけを Access の self-hosted アプリで守り、Worker が `Cf-Access-Jwt-Assertion` を検証する(`src/auth/access-jwt.ts`。署名 RS256・`iss`・`aud`・`exp` 必須・`email`・`sub`)。`/mcp` `/token` `/register` `/.well-known/*` `/healthz` に Access の検証は足さない。Access の Managed OAuth は使わない。
- 失敗の応答: JWT なし・不正 → 403、JWKS を取得できない → 503(fail closed)。ログには種別だけ(JWT・email は出さない)。
- `userId = userIdFromEmail(email)` = `base64url(SHA-256(trim して小文字にした email))`(`src/auth/user-id.ts`。43 文字、`:` を含まない)。**生の email は `props`・`metadata`・KV のキー・MCP トークン・ログに出さない**(`beginUpstream` の暗号化された `data` に `accessEmail` を置くのは `/callback` での一致確認のため)。**UUID などへのフォールバックは作らない**: `ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` が欠けた Worker は `checkConfig` で `/healthz` 以外 500。
- `/callback` は `finishUpstream` のあと、JWT の email が `/authorize` と同じか確かめる(不一致は 403 でコード交換しない)。`completeAuthorization` は `revokeExistingGrants: true` を明示(同じ email・同じ MCP クライアントの旧 grant を失効)。
- キャッシュキー・冪等キー・stale 印は **grant 単位**(`<userId>:<grantId>`)。email 単位にしない(同じ email で別の kintone アカウントを認可しうるため。ADR 0010)。
- kintone OAuth: トークンエンドポイントは **HTTP Basic**(フォームに `client_secret` を入れない)、PKCE(S256。`KINTONE_OAUTH_PKCE=off` で無効化できる)、scope は **6 つ**(`src/auth/kintone-oauth.ts` の `KINTONE_SCOPES`)、リダイレクト URI は `<PUBLIC_BASE_URL>/callback`、接続先は `KINTONE_HOST`。API トークン(`X-Cybozu-API-Token`)経路は持たない。
- トークン更新(ADR 0005): `tokenExchangeCallback`(`src/auth/token-exchange.ts`)は期限切れか stale 印があるときだけ kintone を更新する。更新の実挙動は未検証(V01)なので、期限内に同じトークンが返る場合(モード A)と毎回新しいトークンが返る場合(モード B)の両方で同じテストを回す(`test/helpers/fake-kintone-oauth.ts`)。
- 認可の最後に `GET /k/v1/apps.json?limit=1` で疎通確認する。

## 出力規約・エラー

- MCP ツールの出力は JSON を text content で返す(structuredContent は使わない)。成功は kintone のデータそのもの(加工したものは snake_case)。
- エラーは `isError: true` で `{"schema_version":"1","error":{"code":"...","message":"...","retryable":false,"details"?:{...}}}`(`src/errors.ts` に仕様書 §6 の全コード `ERROR_CODES` と `details`、`src/kintone/errors.ts` に HTTP 応答の分類。`test/unit/error-table.test.ts` が表の全行をテストする)。**コードを足したら `error-table.test.ts` に行を足す**(`ERROR_CODES` の全コードが表に現れることを検査している)。401・403・404・429 はステータスが先、そのあと 400・422・`CB_VA` 接頭辞 → `argument_error`。200 なのに JSON でない応答は `api_error`(retryable、`http_status: 200`)。
- **kintone クライアント**(`src/kintone/client.ts`): 1 ツール呼び出し = 1 クライアントで、リクエストは直列。**読み取り(GET)の 429・503 だけ最大 2 回リトライ**(`Retry-After` は整数秒で 10 秒まで、なければ 1 秒)。**書き込み(POST / PUT / DELETE)はリトライしない**(これを変えない)。呼び出し回数の上限 50(リトライ含む)、応答本文 10 MiB、タイムアウト 30 秒、リダイレクトは辿らない。トークンはエラー・ログに出さず、kintone の message からも伏せる。
- **ツールの足し方**: `src/mcp/tools/<名前>.ts` で `defineTool`(`kintone_<resource>_<action>`、区分 = ReadOnly / WriteIdempotent / WriteNonIdempotent / Destructive、toolset = `app` / `record`、`z.strictObject`)→ `src/mcp/tools/index.ts` の `ALL_TOOLS` に足す → `mise run test:update-snapshots` で `test/snapshots/tools-list.json` を更新し、`git diff` で引数・description・annotation の差分を確認してからコミットする。ReadOnly のツールには `max_response_tokens`(既定 50000、0 で無効、超えたら切り詰めず `response_too_large`。公開スキーマは `minimum: 0`、負は `argument_error`)が自動で付く。書き込み系には付けない。
- **名前解決とキャッシュ**(ADR 0007、`src/resolver/`): ツールは `ctx.resolver` と `withResolvedApp`(`app` / `app_ref` はどちらか 1 つ必須・排他)で ID を得る。`app_ref` の順序は ID → コード完全一致 → 名前完全一致 → 名前部分一致(各段で即 return)、名前検索は 10 ページまで(`resolver_app_list_too_large`)。**`mode: "write"` は完全一致だけ**(部分一致のみなら `resolver_app_exact_match_required` / `resolver_field_exact_match_required`。M04 の書き込みツールが使う)。キャッシュは `OAUTH_KV` の `kcache:v1:<userId>:<grantId>:...`(grant 単位、TTL 600 秒)で、KV の失敗は握りつぶして続行し、`not_found` は読んだ段階だけ 1 回取り直す。`kintone_apps_search` と `kintone_app_describe` はキャッシュを読まない。
- **`maxApiCalls`**: 読み取りツールは `readBudget(論理最悪)`(= 論理 × 3)を明示する(apps_search 3 / app_describe 39 / records_query 36)。ツールを足すときは式(ADR 0008)から決め、`test/unit/call-budget.test.ts` に最悪ケースを足す。
- **`/mcp` の URL クエリ**: `?toolsets=app,record`(既定 all)と `?read_only=true`(ReadOnly 以外を隠す)。不正な値・綴り違いのキー(`read` / `tool` で始まる未知のキー)・公開ツールが 0 個になる組み合わせは HTTP 400 `{"error":"invalid_query",...}`(`src/mcp/registry.ts` の `parseToolConfig`。無関係なキーは無視する)。テストで実在しないクエリ(`?x=1` 等)を使わない。
- 入力の検証は自前(zod の `safeParse`)で行い、SDK には公開用のスキーマだけを渡す(SDK の検証エラーはエンベロープにならないため)。
- kintone が 401 を返したら、ツールは `authentication_error` を返し、`/mcp` の応答も HTTP 401 + `invalid_token` にして stale 印を置く(`src/mcp/api-handler.ts`)。
- **起動時の設定チェック**(`src/config-check.ts`、仕様書 §9): 必須の vars / secrets / KV が欠けている・プレースホルダ(`<...>`)のまま・形式が違うなら、`/healthz` 以外は 500 `{"error":"server_misconfigured"}`、`/healthz` は 503 で項目名だけを返す。ログにも応答にも**値は出さない**。必須の設定を足したら `checkConfig` と unit / E2E を更新する。

## 構成

```
src/index.ts                  default export { fetch }。checkConfig → OAuthProvider を PUBLIC_BASE_URL から作って呼ぶ
src/app.ts                    Hono: /healthz, /authorize(同意), /callback。/authorize と /callback に accessGuard(Access JWT 検証)
src/env.ts  src/urls.ts  src/errors.ts  src/config-check.ts
src/auth/authorization-server.ts  OAuthProvider の設定(/mcp は apiHandler、それ以外は app.ts)
src/auth/access-jwt.ts        Access JWT の検証(JWKS を 5 分キャッシュ、未知の kid は取得から 60 秒以上で取り直す)
src/auth/user-id.ts           email → userId
src/auth/kintone-oauth.ts     kintone の認可 URL・コード交換・リフレッシュ(Basic、PKCE、エラー分類)
src/auth/pkce.ts              PKCE の verifier / challenge
src/auth/token-exchange.ts    tokenExchangeCallback: MCP トークン更新時に kintone も更新
src/auth/kintone-stale.ts     kintone の 401 を受けた grant に印を置き、次の MCP トークン更新で(期限に関係なく)kintone も更新させる
src/auth/consent-page.ts      同意画面(クライアント由来の文字列と email は必ずエスケープ)
src/auth/props.ts             GrantProps(kintone のトークンだけ)
src/kintone/client.ts         kintone REST クライアント(直列・読み取りのリトライ・maxApiCalls・10 MiB・タイムアウト・redact。get / post / put / delete)
src/kintone/errors.ts         KintoneUnauthorizedError / KintoneApiError(HTTP 応答 → エラー表の分類)/ KintoneCallLimitError
src/mcp/api-handler.ts        /mcp の下流。kintone 401 を HTTP 401 に変換
src/mcp/server.ts             McpServer のファクトリ(リクエストごとに生成。registerTools に委譲)
src/mcp/registry.ts           defineTool / annotationsFor(4 区分)/ TOOLSETS / parseToolConfig(URL クエリ)/ registerTools(自前の zod 検証・エンベロープ・応答サイズ)
src/mcp/response-limit.ts     max_response_tokens(推定トークン数、既定 50000)
src/mcp/request-body.ts       MCP の POST 本文の読み取り(2026-07-28 / 旧版の両レーン。上限 8 MiB)
src/mcp/tools/index.ts        ALL_TOOLS(M03 は読み取りの 3 つ。M04 で書き込みを足す)
src/mcp/tools/apps-search.ts  kintone_apps_search(ids / codes / name / space_ids / limit / offset。リゾルバー・キャッシュは使わない)
src/mcp/tools/app-describe.ts kintone_app_describe(app.json + form/fields.json。キャッシュを読まず、フィールドのキャッシュを更新)
src/mcp/tools/records-query.ts kintone_records_query(app_ref を解決して records.json を 1 回。total_count は数値)
src/mcp/tools/app-target.ts   app / app_ref の共通部(排他検査・解決・キャッシュ由来の 404 でキャッシュを消す)
src/resolver/resolver.ts      名前解決(アプリ: ID → コード → 名前完全 → 名前部分。フィールド: コード → label 完全 → 部分。mode read|write、10 ページ上限、candidates、not_found の取り直し)
src/resolver/cache.ts         KV キャッシュ(kcache:v1:<userId>:<grantId>:...、TTL 600 秒、失敗しても続行)
src/resolver/budget.ts        最悪ケースの式(A = 11、F = 1)と readBudget(論理回数 × 3)
scripts/verify-oauth/         実環境の OAuth 確認 CLI(`mise run verify:oauth`)。core.ts(Web 標準 API だけ。入出力は VerifyIo で注入)・args.ts・loopback.ts(node:http)・main.ts(Node 24 の型ストリップで直接実行。TS の enum / parameter property は使わない)
.github/workflows/ci.yml      lint / test(アクションは SHA 固定。deploy:check は M05)
test/unit/  test/e2e/  test/node/  test/node-e2e/(空)  test/helpers/  test/snapshots/(tools/list のスナップショット)
```

## テストの書き方

- 実行環境は workerd(`@cloudflare/vitest-plugin`)。`vitest.config.ts` の `unit` / `e2e` の 2 プロジェクトと、Node 環境の `node` プロジェクト(`test/node/`。`node:http` が要る verify:oauth のループバック受信だけ。`test:unit` に含まれる)と `node-e2e` プロジェクト(`test/node-e2e/`。M05 から使う。空の間は `test:e2e` のタスクから外している(`vitest.config.ts` には定義済み。M05 で `mise.toml` の `test:e2e` に `--project node-e2e` を足す))。テスト用の束縛(ダミー値)は `vitest.config.ts` の `testBindings`。
- workerd のテストで `cloudflare:test` を import しない(import すると並列実行時に他ファイルの初回リクエストがタイムアウトする)。Worker 全体を任意の env で呼ぶときは `import worker from "../../src/index"` に env とダミーの ctx を渡す(`test/e2e/config-check.test.ts`、`test/helpers/oauth-flow.ts` の `useEnv`)。
- E2E は `test/helpers/oauth-flow.ts` の `worker.fetch`(Worker 全体への HTTP)で、`/register` → `/authorize` → `/callback` → `/token` → `/mcp` を通しで行う。`/authorize` と `/callback` の各リクエストには、テスト用の鍵で署名した `Cf-Access-Jwt-Assertion`(`test/helpers/access.ts`)を付ける。kintone と Access の JWKS は `vi.spyOn(globalThis, "fetch")` で差し替える(同じ isolate で動くため Worker にも効く。`setupMocks`)。
- 時刻に依存する挙動(kintone トークンの期限切れ、JWKS のキャッシュ)は `vi.useFakeTimers({ toFake: ["Date"] })` で進める。コード側は `Date.now` の参照を保持せず、呼び出し時に `Date.now()` を読むこと。JWT の発行時刻も同じ Date を使うので、時刻を進めたあとに JWT を使うテストは `exp` を十分先にする。
- kintone クライアントのテストは `mockOutboundFetch`(`test/helpers/mock-fetch.ts`。呼び出しに `signal` / `redirect` も記録する)と、注入できる `sleep`(リトライの待機を実際には待たない)を使う。ツールの unit は `test/helpers/fake-kintone-client.ts`(渡したメソッド以外は呼ばれたら例外)と `test/helpers/tool-harness.ts`(実 `createMcpServer` の `tools/call` を通し、kintone の GET を記録する。KV・grant も渡せる)。kintone のデータ層の偽物(apps.json のフィルタ・ページング、form/fields.json)は `test/helpers/fake-kintone-data.ts`、メモリ上の KV(失敗の注入つき)は `test/helpers/fake-kv.ts`。トークンらしい文字列(`Bearer <値>`)をテストに書くときは文字列を分けて組み立てる(エディタ・ツールが伏せるため。`test/unit/kintone-client.test.ts` の `BEARER`)。
- tools/list のスナップショット(`test/unit/tools-list-snapshot.test.ts`)は `mise run test:update-snapshots` で更新する(更新後に差分をレビュー)。タスクは `vitest run <ファイル> --project unit --update` の順で書く(`--update` の後ろにファイルを置くと指定が効かず unit 全体が走る)。
- verify:oauth の E2E(`test/e2e/verify-oauth.test.ts`)は、core を Worker とモック kintone に対して動かし、ブラウザの段は `io.openBrowser` で `/authorize` → `/callback` を Access の JWT 付きで直接たどる。`main.ts` と `loopback.ts` の実機側は `test/node/` と手動確認。

## API の確認

このリポジトリの OAuth / MCP の API 名は記憶でなく `node_modules` の `.d.ts` と docs で確認する:
`node_modules/@cloudflare/workers-oauth-provider/dist/oauth-provider.d.ts` と `node_modules/@cloudflare/workers-oauth-provider/docs/`、`node_modules/agents/docs/mcp-servers.md`。
