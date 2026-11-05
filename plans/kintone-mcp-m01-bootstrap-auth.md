# M01: 雛形と OAuth の骨格

ロードマップ: `plans/kintone-mcp-roadmap.md`
仕様: `docs/specs/kintone-mcp-spec.md`(§3 エンドポイント、§4 OAuth・§4.5 Access、§9 設定)
設計判断: `docs/adr/0001`〜`0005`、`0009`、`0010`(Access の email を利用者 ID にする)
書式の手本: logbutler の `plans/logbutler-m01-bootstrap-auth.md`

## 目的

Go 版を取り除き、Cloudflare Workers 版の土台を作る。MCP クライアント(Cloudflare MCP Portal を想定)が OAuth で接続し、
**利用者ごとの kintone OAuth で認可された身元**(利用者 ID は Cloudflare Access の email。`/authorize` と `/callback` で JWT を検証する)として、最小のツール `kintone_apps_search`(`limit` だけ受ける。`GET /k/v1/apps.json`)を呼べるところまでを、モックの kintone に対して通しで動かす。

## 完了条件

- `mise run test`(unit + e2e)と `mise run lint` が通る
- `@cloudflare/vitest-plugin`(workerd)上で Worker 全体に HTTP リクエストを送り、次が E2E(`mise run test:e2e`)で確認できる。kintone の OAuth / REST と Access の JWKS は outbound fetch のモックで差し替える(Access の JWT はテスト用の鍵で署名して `Cf-Access-Jwt-Assertion` に付ける)
  1. トークンなしで `/mcp` を呼ぶと `401` と `WWW-Authenticate: Bearer ... resource_metadata="<origin>/.well-known/oauth-protected-resource/mcp"` が返り、その URL が 200 で `authorization_servers` と `resource`(`<origin>/mcp`)を含む JSON を返す
  2. `/register`(DCR)→ `/authorize`(同意画面 → 承認)→ kintone の認可画面(モック。**認可 URL に `code_challenge`・`code_challenge_method=S256`・`scope=k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write`(6 つ、スペース区切り)・`redirect_uri=<origin>/callback` が付くこと**)→ `/callback` → `/token` でアクセストークンを取得できる。コード交換が **Basic 認証ヘッダー + フォーム(`code_verifier` を含み、`client_secret` を含まない)**であること、コード交換後に `GET /k/v1/apps.json?limit=1` の疎通確認が呼ばれること
  3. 取得したトークンで `/mcp` の `tools/list` と `tools/call kintone_apps_search` が成功する(kintone への呼び出しに `Authorization: Bearer <kintone のアクセストークン>`)
  4. kintone のアクセストークンが期限切れの状態で MCP クライアントがリフレッシュすると、`tokenExchangeCallback` で kintone のトークンも更新される(期限内のリフレッシュでは kintone を呼ばない)
  5. kintone の `/k/v1/...` が 401 を返すと、MCP の応答も HTTP 401(`invalid_token`)になり、stale 印が置かれ、次のリフレッシュで期限に関係なく kintone が更新されて回復する
  6. `KINTONE_OAUTH_PKCE=off` では `code_challenge` / `code_verifier` が送られない
  7. **Access(ADR 0010)**: `/authorize`(GET・POST)と `/callback` は、正しい `Cf-Access-Jwt-Assertion` がないと 403(偽署名・`aud` 違い・`iss` 違い・期限切れ・`exp` なし・email なし)、JWKS を取得できなければ 503。`/mcp` `/token` `/register` `/.well-known/*` `/healthz` は JWT なしで到達できる。`/callback` で JWT の email が `/authorize` と違えば 403 でコード交換をしない。`userId` が email から決まる(同じ email なら同じ、別の email なら別。`:` を含まない。生の email が `userId`・MCP トークン・KV のキーに現れない)
  8. **再認可**: 同じ email・同じ MCP クライアントで再認可すると旧 grant が失効し(旧 MCP リフレッシュトークンの更新が `invalid_grant`)、新 grant は使える。別の email、または別のクライアントの grant は影響を受けない
- `/healthz` は認証なしで `{"status":"ok"}`。設定不備(`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` の未設定を含む)なら 503(項目名のみ)、それ以外のパスは 500 `{"error":"server_misconfigured"}`。UUID などへのフォールバックで認可が通ることはない
- `mise run verify:oauth`(実環境用の確認ツール)の core が、モックの Worker に対して全段階を通る(E2E)
- Go のコード・Go 向けの設定が削除されている(最初のコミット)。CLAUDE.md が Workers 版の規約に書き換わっている
- **実機確認(V01〜V05、P01〜)は完了条件に含めない**(ユーザーが `docs/manual-verification.md` に従って実施)

## 対象範囲

### 0. 最初のコミット: Go の削除(ユーザーが `archive/go` を push 済みか確認してから)
- 削除: `cmd/` `internal/` `go.mod` `go.sum` `Makefile` `Dockerfile` `.dockerignore` `.goreleaser.yaml` `.golangci.yml` `.github/workflows/release.yml`、Go 向けの計画書・仕様書(`plans/kintone-m*.md`、`plans/kintone-roadmap.md`、乱数名の 3 ファイル、`docs/specs/kintone_spec.md`、`docs/decisions/`)、`README.ja.md`
- 置き換え: `mise.toml`(Go → node/pnpm)、`.gitignore`(Go 向け → logbutler の形)、`README.md`(「Workers 版へ作り直し中。Go 版は `archive/go`」の短い案内。完全版は M05)
- 残す: `LICENSE`、`CHANGELOG.md`(M05 で整理)、新規の `docs/` `plans/kintone-mcp-*.md`
- このコミットは削除と置き換えだけで、アプリのコードは足さない。**`ci.yml` はこのコミットでは削除し(`package.json` がなく `mise run lint` が赤になるため)、雛形(§1)のコミットで最小版を作り直す**。`mise.toml` / `.gitignore` / `README.md` はこのコミットで置き換えてよい

### 1. リポジトリの雛形(logbutler と同じ形)
- `mise.toml`: `[tools]` に node と pnpm を**正確なバージョンで**固定(logbutler の `mise.toml` と同じ値。着手時に 2 週間ルールを再確認)。タスク `install` `test` `test:unit` `test:e2e` `test:update-snapshots`(M02 から有効) `lint` `fmt` `build` `dev` `verify:oauth`(`deploy` 系は M05)
- `package.json`(**依存は logbutler の `package.json` と同じ正確なバージョン**。`yaml` は入れない。`jose`(Access の JWT 検証と、テスト用の鍵・JWT 署名。logbutler の M01 時点(コミット 861c23c)の `package.json` と同じバージョン `6.2.12`。現在の logbutler は M06 で外している)は入れる。`@modelcontextprotocol/sdk` / `client` は verify:oauth とテストで使うものだけ)、`pnpm-lock.yaml`、`pnpm-workspace.yaml`(logbutler にあるものと同じ)、`tsconfig.json`(strict)、`scripts/tsconfig.json`、`biome.json`、`vitest.config.ts`(`unit` / `e2e` / `node` / `node-e2e` の 4 プロジェクト。テスト用束縛はダミー値: `KINTONE_HOST=example.cybozu.com`、`PUBLIC_BASE_URL=https://kintone-mcp.test`、`KINTONE_CLIENT_ID` / `KINTONE_CLIENT_SECRET` のダミー、`ACCESS_TEAM_DOMAIN=kintone-mcp-test.cloudflareaccess.com`、`ACCESS_AUD=test-access-aud`)
- `wrangler.jsonc`(共有のダミー値。`name: "kintone-mcp"`、`main: src/index.ts`、`compatibility_date` は固定した workerd が対応する最新日、`nodejs_compat` は必要になったときだけ、`kv_namespaces: [{binding: "OAUTH_KV", id: "000…"}]`、`vars: {KINTONE_HOST, PUBLIC_BASE_URL, ACCESS_TEAM_DOMAIN, ACCESS_AUD}`。ダミー値 `example-team.cloudflareaccess.com` / `replace-with-access-application-aud`)
- `.github/workflows/ci.yml`(mise の lint/test の最小版。アクションはコミット SHA で固定。SHA は `gh api repos/<owner>/<repo>/git/ref/tags/<tag>` で確かめる)
- `.dev.vars.example`(`KINTONE_CLIENT_ID` `KINTONE_CLIENT_SECRET` の雛形。実値はコミットしない。`ACCESS_*` は秘密でないので `wrangler.jsonc` の vars)
- `CLAUDE.md`(Workers 版の作業規約。logbutler の `CLAUDE.md` の該当部分に合わせる: TDD・mise・出力規約・エラーエンベロープ・kintone トークンは props にだけ・kintone API はモックのみ・依存の固定・デプロイは実行しない・ADR への参照)

### 2. 構成
```
src/
  index.ts              default export { fetch }。checkConfig → OAuthProvider(PUBLIC_BASE_URL から生成して isolate 内で使い回す)
  app.ts                Hono: /healthz, /authorize (GET/POST), /callback。/authorize と /callback に accessGuard(Access JWT 検証)
  env.ts                Env 型 (OAUTH_KV, KINTONE_HOST, PUBLIC_BASE_URL, ACCESS_TEAM_DOMAIN, ACCESS_AUD, KINTONE_CLIENT_ID, KINTONE_CLIENT_SECRET, KINTONE_OAUTH_PKCE?, OAUTH_PROVIDER)
  urls.ts               MCP_PATH, mcpResource, protectedResourceMetadataUrl, callbackUrl (= <base>/callback)
  config-check.ts       checkConfig (仕様書 §9)
  errors.ts             最小のエラー型 (M02 でエンベロープへ拡張)
  auth/
    authorization-server.ts  OAuthProvider の設定
    access-jwt.ts            Access JWT の検証 (logbutler の git 履歴 8dac72c^:src/auth/access-jwt.ts を移植。JWKS キャッシュ、AccessJwtError)
    user-id.ts               userIdFromEmail (trim + 小文字 → SHA-256 → base64url)
    kintone-oauth.ts         buildAuthorizeUrl / exchangeCode / refreshTokens (Basic 認証、PKCE、エラー分類)
    pkce.ts                  generateCodeVerifier / codeChallengeS256
    token-exchange.ts        tokenExchangeCallback (ADR 0005)
    kintone-stale.ts         stale 印 (kintone-stale:<userId>:<grantId>)、grantRefFromAccessToken
    consent-page.ts          同意画面 (kintone ホストと Access のログイン email を表示。クライアント由来の文字列と email はエスケープ)
    props.ts                 GrantProps { kintoneAccessToken, kintoneRefreshToken, kintoneExpiresAt }
  kintone/
    client.ts                最小: bearer 付き GET、401 → KintoneUnauthorizedError (M02 で本格化)
  mcp/
    api-handler.ts           /mcp の下流。kintone 401 → HTTP 401 + stale 印
    server.ts                McpServer のファクトリ (リクエストごと)
    request-body.ts          logbutler の同名ファイルと同じ (MCP 2026-07-28 / 旧版の両レーン)
    tools/apps-search.ts     最小 (limit のみ)
scripts/verify-oauth/        core.ts / args.ts / loopback.ts / browser.ts / main.ts (logbutler から移植し、Basic・PKCE・kintone の段階名に合わせる)
test/
  unit/  e2e/  node/  node-e2e/(空でも可)  helpers/
```
- 認可サーバーと MCP は同居(ADR 0002)。`createMcpHandler` の戻り値を直接 default export しない。
- `userId` は Access の email から作る(`userIdFromEmail`。ADR 0010)。`GrantProps` には `userId` も email も持たない(`userId` はトークンから取り出す)。`completeAuthorization` に `revokeExistingGrants: true` を明示する。
- 構成は ADR 0002 の同居のまま。Access を保護するのは `/authorize` と `/callback` だけ(ADR 0010)。

### 3. 認証の流れ
仕様書 §4.3、§4.4 のとおり。logbutler の `src/app.ts` / `src/auth/*` を雛形に、次を kintone 用に変える:
- トークンエンドポイントは Basic 認証 + フォーム(client_secret はフォームに入れない)
- `POST /authorize` で verifier を生成し `beginUpstream({ data: { verifier } })`。`/callback` で `finishUpstream<{verifier: string}>` から取り出す
- 認可 URL は `https://<KINTONE_HOST>/oauth2/authorization`、トークンは `https://<KINTONE_HOST>/oauth2/token`
- `/authorize`(GET・POST)と `/callback` の先頭で Access JWT を検証する(仕様書 §4.5)。`POST /authorize` で `beginUpstream` の `data` に `{ verifier, accessEmail }`、`/callback` で `finishUpstream<{verifier: string; accessEmail: string}>` から取り出し、JWT の email と一致しなければ 403
- `/callback` のコード交換後に `GET /k/v1/apps.json?limit=1` で疎通確認 → `completeAuthorization`(`userId = userIdFromEmail(email)`、`revokeExistingGrants: true`)
- 失敗の応答: JWT なし・不正 → 403、JWKS 取得不能 → 503(ログは種別だけ。JWT・email は出さない)
- 更新エラーの分類: `invalid_grant` → grant 失効、`invalid_client` / `unauthorized_client` → `server_error`、5xx・429・通信失敗 → `temporarily_unavailable`

### 4. 環境変数・バインディング
- KV: `OAUTH_KV`
- vars: `KINTONE_HOST`、`PUBLIC_BASE_URL`、`ACCESS_TEAM_DOMAIN`(必須)、`ACCESS_AUD`(必須)、(任意)`KINTONE_OAUTH_PKCE`
- secrets: `KINTONE_CLIENT_ID`、`KINTONE_CLIENT_SECRET`

## TDD の進め方(小さい順。各段階で失敗を確認してから実装)

1. `/healthz` と `checkConfig`(unit: 欠け・空白・プレースホルダ・形式・値を結果に載せない、`KINTONE_OAUTH_PKCE` の不正値、**`ACCESS_TEAM_DOMAIN`(未定義・空・プレースホルダ・スキームやパス付き)と `ACCESS_AUD`(未定義・空・プレースホルダ)**。E2E: 設定を欠かした env(`ACCESS_*` を欠かしたものを含む)で `/healthz` 503・他パス 500・ログに項目名だけ)
1b. **Access JWT の検証(`access-jwt.ts`。Red から)** — JWKS は fetch モック(`mockOutboundFetch`)、JWT はテスト用の鍵(`createTestKey` で RS256 鍵を生成し `signAccessJwt` で署名。logbutler の `test/helpers/access.ts` を写す)。unit(`access-jwt.test.ts`):
   - 正しい JWT を受け入れ、email と sub を返す(`aud` が配列でも通る)
   - **拒否**: ヘッダーなし(`missing`)、**偽署名**(同じ `kid` の別の鍵で署名)、**`aud` 違い**、`iss` 違い(別チーム)、**期限切れ**、`exp` なし、**email なし**(空文字・数値・claim 欠落)、`sub` なし、`alg` が RS256 でない(例 HS256 で JWKS の公開鍵を秘密として署名)
   - JWKS を取得できなければ拒否(`jwks_unavailable`、fail closed。404・fetch 例外・`keys` が配列でない)
   - JWKS を 5 分キャッシュし 2 回目は取得しない。未知の `kid` は取得から 60 秒以上たっていれば取り直す(鍵ローテーション)。60 秒未満・初回取得の直後は取り直さない
   - エラーに JWT・email が含まれない
1c. **`userIdFromEmail`(unit: `user-id.test.ts`)**: 同じ email は同じ値、大文字小文字・前後の空白の違いは同じ値、別の email は別の値、`:` を含まない 43 文字の base64url、生の email が含まれない、`a:b@example.com` のように `:` を含む email でも `:` を含まない、空文字は例外
2. PKCE(unit: RFC 7636 付録 B のテストベクトル — verifier `dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk` から challenge `E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM`。生成した verifier が 43〜128 文字の URL-safe 文字であること)
3. kintone OAuth(unit、fetch をモック): 認可 URL(全パラメーター、scope のスペース区切り、PKCE off)、コード交換(Basic ヘッダー、フォームの中身、`client_secret` がフォームにない、`redirect_uri`)、更新(`refresh_token` なしの応答で元を維持、あれば差し替え)、エラー分類(`invalid_grant` / `invalid_client` / 5xx / 429 / 通信失敗 / JSON でない応答)、トークンがエラーメッセージに出ない
4. stale 印(unit: キー、TTL 15 分、値は `1` だけ、`grantRefFromAccessToken`)
5. `tokenExchangeCallback`(unit: 期限内は kintone を呼ばず寿命 = 残り + 5 秒、残り 0 以下は更新、stale 印は期限に関係なく更新して印を消す、同じトークンが返った場合の寿命、`new_token` の式、上限 1800・下限 60、ログに値が出ない)
6. 同意画面(unit: クライアント名・URL・scope のエスケープ、kintone ホストの表示、**Access のログイン email の表示とエスケープ(`<script>` を含む email 風の文字列)、次の kintone ログインで同じ本人のアカウントを選ぶ旨の注意書き**)
7. MCP: `tools/list` と `kintone_apps_search`(kintone クライアントをモック。401 → `KintoneUnauthorizedError`)
8. `/mcp` の api-handler(unit: kintone 401 → HTTP 401 + `WWW-Authenticate` + stale 印、印の書き込み失敗でも 401 を返す)
9. 認可フロー全体の E2E(完了条件 1〜3、6。`test/helpers/oauth-flow.ts` を移植。`/authorize` と `/callback` の各リクエストに、テスト用の鍵で署名した `Cf-Access-Jwt-Assertion` を付けるヘルパーを足す): 認可 URL(scope 6 つ)とコード交換の検査、疎通確認の呼び出し、`userId` が email から決まる(同じ email なら同じ、別の email なら別。`:` を含まない。生の email が MCP トークン・KV のキーに現れない)こと、props 以外に kintone のトークンが KV に平文で残らないこと
10. リフレッシュの E2E(完了条件 4・5): `test/helpers/fake-kintone-oauth.ts` — 状態と時刻を持つ fake。**更新の挙動は 2 モード**(A: 期限内の更新は同じトークンを残り寿命つきで返す / B: 毎回新しいトークン)で同じテストを回す(kintone の実挙動が未検証=V01 のため、どちらでも壊れないことを固定する)。失効の 401 → stale 印 → 1 回のリフレッシュで回復
11. 認可入力の検証 E2E(logbutler の `authorize-validation.test.ts` 相当。Access の JWT 付きで行う): 未登録 `redirect_uri` の拒否、偽造 state の `/callback` の拒否、同意画面の XSS
12. **Access 保護の E2E(`access-protection.test.ts`。logbutler の git 履歴 `8dac72c^:test/e2e/access-protection.test.ts` を写して kintone 用に直す。Red から)**:
    - JWT なしの GET /authorize・POST /authorize・GET /callback はすべて 403(kintone のトークン交換・疎通確認は呼ばれない)
    - 期限切れ・`aud` 違い・偽署名・別チーム(`iss` 違い)・email なしの JWT は 403
    - JWKS を取得できないときは 503
    - 正しい JWT なら `/authorize` は同意画面(200)を返し、画面に email が出る
    - `/mcp`(401 + `WWW-Authenticate`)・`/token`・`/register`・`/.well-known/*`・`/healthz` は Access の JWT なしで到達できる
    - **`/callback` で email 不一致**: `/authorize`(email A)で始めた state を、email B の JWT で `/callback` に送ると 403、コード交換が呼ばれない、grant が作られない
    - `/callback` で JWT が欠けても同様に 403
    - **`ACCESS_*` 未設定の Worker では `/authorize` が 500(`server_misconfigured`)で、認可が通らない**(UUID へのフォールバックがない回帰テスト)
12b. **再認可の E2E(`reauthorize.test.ts`)**: 同じ email・同じクライアントで 2 回認可 → 1 回目の MCP リフレッシュトークンは `invalid_grant`、2 回目は有効。別の email は影響を受けない。別のクライアント(別の DCR 登録)で同じ email が認可しても、既存 grant は残る
13. `verify:oauth` の core(E2E でモック Worker に対して全段階。Node の `loopback.ts` / `main.ts` は `test/node/`)。`authorize` 段階は実環境ではブラウザが Access のログインを通る。core は「ブラウザ」役を注入できる形にし、E2E ではテスト用の鍵で署名した JWT を付ける fetch 役を渡す
14. 生成物の確認: `mise run build`(`wrangler deploy --dry-run`)が通る

## 作るテストの一覧

| 種別 | ファイル | 内容 |
|---|---|---|
| unit | `config-check.test.ts` | 欠け・不正・プレースホルダ・値を出さない・PKCE 設定・`ACCESS_*` |
| unit | `access-jwt.test.ts` | JWT 検証の全ケース(偽署名・aud 違い・iss 違い・期限切れ・exp なし・email なし・JWKS 取得不能・キャッシュ・鍵ローテーション)。JWKS は fetch モック |
| unit | `user-id.test.ts` | email → userId(正規化・`:` を含まない・生の email を含まない) |
| unit | `pkce.test.ts` | RFC 7636 ベクトル、verifier の形式 |
| unit | `kintone-oauth.test.ts` | 認可 URL、Basic、フォーム、エラー分類、機密を出さない |
| unit | `kintone-stale.test.ts` | 印のキー・TTL・値、トークンからの取り出し |
| unit | `token-exchange.test.ts` | ADR 0005 の全規則 |
| unit | `consent-page.test.ts` | エスケープ、ホスト表示、email 表示 |
| unit | `mcp-api-handler.test.ts` | 401 変換、stale 印、書き込み失敗 |
| unit | `tool-apps-search.test.ts` | 最小ツールの引数検証 |
| unit | `fake-kintone-oauth.test.ts` | fake 自体の挙動(2 モード) |
| e2e | `healthz.test.ts` / `config-check.test.ts` | 正常・設定不備 |
| e2e | `oauth-flow.test.ts` | 完了条件 1〜3・6 |
| e2e | `refresh.test.ts` | 完了条件 4・5 |
| e2e | `authorize-validation.test.ts` | 未登録 redirect_uri・偽造 state・XSS |
| e2e | `access-protection.test.ts` | JWT なし・不正は 403、JWKS 不能は 503、保護しないパスの到達、`/callback` の email 不一致、`ACCESS_*` 未設定 |
| e2e | `reauthorize.test.ts` | 同じ email・同じクライアントの再認可で旧 grant が失効 |
| e2e | `verify-oauth.test.ts` | core の全段階 |
| node | `verify-oauth-loopback.test.ts` | ループバック受信 |

## やってはいけないこと
- Durable Objects / McpAgent を使う
- kintone のトークン・クライアントシークレットをログに出す、`props` 以外(平文の KV キーなど)に保存する
- テストを弱める・skip する・消す
- 実 kintone への通信を unit / e2e テストに入れる(`mock-fetch` は未登録の宛先への通信を例外にする)
- API トークン(`X-Cybozu-API-Token`)経路を足す(ADR 0001)
- Access の JWT 検証を `/authorize` `/callback` 以外に足す、Access の Managed OAuth を使う、Access が無効・未設定のときに UUID などで代替して認可を通す(ADR 0010)
- 生の email を `props`・`metadata`・KV のキー・MCP トークン・ログに出す
- コミット・push・リモート作成・デプロイ(ユーザーの指示があるときだけ)。`archive/go` の push はユーザー
- M02 以降の機能(本格的なクライアント、エラー表の全行、名前解決、残り 5 ツール)を先取りする
- logbutler リポジトリを変更する、logbutler の `mise.local.toml` / `.verify-oauth.local.json` / `wrangler.deploy.jsonc` / `.wrangler/` を読む・写す

## 未検証の前提(実デプロイしないため、ブロッカーにせず ADR に明記して進む。確認手順は `docs/manual-verification.md`)
- 更新のローテーション・期限内の更新で同じトークンが返るか(V01)、初回の `expires_in`(V02)
- 利用者の許可がないときの失敗(V03)
- PKCE の検証有無・拒否の有無(V04)。拒否されたら `KINTONE_OAUTH_PKCE=off`
- redirect_uri の条件(V05)
- リフレッシュトークン 10 個上限の挙動(V06)
- トークンエンドポイントと REST のエラー本文・ステータス(V07)
- KV の結果整合性(V15)
- Access の前提: `email` claim が IdP によらず入るか(V17)、パス指定で 2 パスだけ守れるか(V18)、`/callback` のリダイレクトで JWT が付くか・セッション切れ(V19)、`workers.dev` での利用(V20)、Portal 経由のブラウザ遷移(P04)、kintone のトークン取り消し(V21)

## 成果物
- 上記のコードとテスト
- `docs/adr/0001`〜`0005`・`0009`・`0010` を「提案」から「採用(M01)」に更新(実装で変えた点があれば追記)
- `CLAUDE.md`(Workers 版)、`README.md`(短い案内)
- `docs/manual-verification.md` の「M01 で実機確認する項目」に、`verify:oauth` の使い方(ブラウザで Access のログインを通る手順を含む)を追記
- `.dev.vars.example` と `wrangler.jsonc` に `ACCESS_*` を追記。runbook は M05 だが、Access アプリ作成の要点(パス 2 つ・AUD)は M01 の `README.md` の短い案内に 1 行で書く
