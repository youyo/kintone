# M02: kintone クライアントと基盤

ロードマップ: `plans/kintone-mcp-roadmap.md`(M02 の行)
仕様: `docs/specs/kintone-mcp-spec.md`(§5 ツール共通、§6 エラー表、§8 制限値と URL クエリ)
設計判断: `docs/adr/0006`(出力・エラー形式)、`0008`(上限・リトライ・応答サイズ)、`0009`(採用する規約)
書式の手本: `plans/kintone-mcp-m01-bootstrap-auth.md`。実装の型は logbutler の `src/backlog/client.ts`、`src/errors.ts`、`src/mcp/registry.ts`、`src/mcp/response-limit.ts`、`test/unit/tools-list-snapshot.test.ts`(読み取り専用の参照。書き換えない)

## 目的

M01 の最小実装(bearer 付き GET だけのクライアント、4 コードだけのエラー、`kintone_apps_search` 1 個の直書き登録)を、
M03 以降のツールが「`defineTool` で定義して一覧に足すだけ」で載る基盤に置き換える。
**ツールは増やさない**(M02 で動くのは `kintone_apps_search` だけ。引数の拡張・リゾルバー・KV キャッシュは M03、書き込みツールは M04)。
書き込み用のクライアント API(POST / PUT / DELETE)と、それをリトライしない規則は M02 でテストごと固める(M04 のツールが使う)。

## 完了条件

- `mise run test`(unit + e2e)と `mise run lint`(Biome + tsc 2 種)と `mise run build` が通る。検証コマンドは `pnpm exec vitest run --project unit --project node` / `--project e2e` / `pnpm exec biome check .` / `pnpm exec tsc --noEmit` / `pnpm exec tsc --noEmit -p scripts/tsconfig.json`
- **kintone クライアント**(`src/kintone/client.ts`。unit。fetch はモック)
  1. 1 つのクライアントのリクエストは直列。並行に `get` を呼んでも、前のリクエスト(リトライの待機を含む)が終わるまで次の `fetch` を始めない。前が失敗しても後続は動く
  2. **読み取り(GET)の 429・503 は最大 2 回リトライ**(合計 3 回まで)。`Retry-After`(秒)があれば従い、なければ 1 秒。10 秒を超える `Retry-After` は待たずに打ち切る。上限回数を超えたら最後の応答をエラーにする。リトライ回数の上限・待ち時間の上限・`Retry-After` の読み方(整数秒のみ。日付形式・負・小数は「なし」扱い)をテストで固定する
  3. **書き込み(POST / PUT / DELETE)は 429・503 でも 1 回で打ち切る**(リトライしない)。通信失敗・タイムアウトでもリトライしない
  4. **呼び出し回数の上限 `maxApiCalls`(既定 50。リトライ含む)**。上限に達したら fetch せずに `api_error`(retryable=false)
  5. **応答本文は 10 MiB まで**。ちょうど 10 MiB は通り、超えたら読むのをやめて `response_too_large`(`Content-Length` が超過を宣言していれば本文を読まない)。応答の本文を全部読んでから判定しない
  6. **タイムアウト 30 秒 / リクエスト**(本文の読み取り中に止まった場合も同じ期限で打ち切る)。通信失敗・タイムアウトは `api_error`(retryable=true)
  7. **200 なのに JSON でない・空の本文**は、通信失敗と同じ `api_error`(retryable=true)にする。M01 は 502 を偽装していた(`details.http_status` が実際の 200 と食い違う)ので、`http_status` は実際の値(200)、message は「JSON でない応答」と区別する(見直し)
  8. **トークンをエラー・ログに出さない**: エラーの message / details、`redact()` の結果、想定外の例外のログのどこにもアクセストークンと `Bearer ...` が現れない(kintone の `message` にトークンが混ざっていても伏せる)。リダイレクトは辿らない(`redirect: "manual"`。トークンを別ホストへ送らない)
  9. GET はクエリ(配列は `key[0]=v` 形式)、POST / PUT / DELETE は JSON 本文(DELETE も本文つき。仕様書 §5.6)
- **エラー分類とエンベロープ**(`src/errors.ts`、`src/kintone/errors.ts`)
  1. 仕様書 §6 のエラー表の**全行**(下の「エラー表と対応するテスト」)が、コード・`retryable`・`details` ごとに unit で確認できる。`ERROR_CODES` の全コードが表のテストに現れることを機械的に検査する(コードを足してテストを書き忘れると赤くなる)
  2. `details` の拡張(`http_status` `kintone_code` `kintone_id` `retry_after_sec` `kind` `ref` `candidates`)。空なら `details` キーごと出さない。トークンを含めない
  3. 想定外の例外は固定文 `Unexpected error.`(`generic_error`)で、内部情報を応答に出さない(ログには例外名と、トークンを伏せた message)
- **ツール登録の仕組み**(`src/mcp/registry.ts`、`src/mcp/tools/index.ts`)
  1. `defineTool`(名前の形式検査 `kintone_<resource>_<action>`、4 区分の annotation、toolset `app` / `record`、任意の `maxApiCalls`)。annotation の値は 4 区分で固定(ReadOnly / WriteIdempotent / WriteNonIdempotent / Destructive)
  2. `registerTools` の `tools/call`: 入力は自前の zod `safeParse` で検証し(SDK には公開用のスキーマだけ渡す)、失敗は `argument_error` で kintone を呼ばない。kintone の 401 は `onKintoneUnauthorized` を呼ぶ。1 回の `tools/call` につきクライアントを 1 つ作る(`maxApiCalls` を宣言したツールはその値を渡す)。重複する名前は登録時に例外
  3. `/mcp` の URL クエリ `read_only` / `toolsets`(`parseToolConfig`): `read_only=true` で ReadOnly 以外を隠す。`toolsets=app,record,all`(既定 all。大文字小文字・空白を許容)。**不正な値(未知の toolset、`read_only` が true / false 以外・複数回)・綴り違いのキー(`readonly` `readOnly` `tool_sets` など `read` / `tool` で始まる未知のキー)・公開ツールが 0 個になる組み合わせは HTTP 400** `{"error":"invalid_query","error_description":"..."}`。無関係なキー(`x=1` など)は無視する。GET / POST / DELETE のどれでも同じ
- **`max_response_tokens`**: ReadOnly のツールにだけ自動で付く任意引数(整数。既定 50000、0 で無効、負・非整数は kintone を呼ぶ前に `argument_error`)。応答 JSON の推定トークン数(ASCII は 4 文字 1 トークン切り上げ、ASCII 以外は 1 文字 1 トークン)が上限を超えたら、**切り詰めずに** `response_too_large`(message に絞り込みの案内)。書き込み・破壊的ツールには付けず、成功した書き込みを失敗にしない
- **tools/list のスナップショットテスト**(`test/unit/tools-list-snapshot.test.ts`、`test/snapshots/tools-list.json`)。引数名・description・annotation の変更が差分として見える。`mise run test:update-snapshots` で更新できる
- **`kintone_apps_search` を新しい仕組み(`defineTool` + `registerTools`)に載せ替える**。引数は `limit` のまま(M03 で拡張)。`max_response_tokens` が付く。M01 の挙動(kintone の生データを JSON text で返す、`limit` の検証、401 の合図、`generic_error`)は変えない
- **E2E**(Worker 全体への HTTP):
  - `/mcp?read_only=true` `?toolsets=app` で `tools/list` / `tools/call` が通る。`?toolsets=nope` `?readonly=true` `?read_only=maybe` は 400 `invalid_query`(`tools/list` も `tools/call` も kintone を呼ばない)
  - `tools/call` で kintone が 429(`Retry-After: 0`)を 1 回返しても成功する(読み取りのリトライ)。429 が続けば `rate_limited` のエンベロープ(`retry_after_sec` 入り)
  - `max_response_tokens` を小さくすると `response_too_large`、`0` で通る
  - M01 の `test/e2e/verify-oauth.test.ts` の「クエリ付き URL」のテストは、検証される `toolsets` / `read_only` と衝突する `?x=1` から、実在するクエリ `?read_only=true` に差し替える(意図 = クエリ付き URL がそのまま `/mcp` に送られ、`resource` は正規 URL を使うこと、は保つ)
- ドキュメント: ロードマップの M02 を `[x]`、ADR 0006 / 0008 / 0009 を「採用(M02)」に更新(実装で変えた点を追記)、仕様書の「状態」と §6 / §8 の補足、`docs/manual-verification.md` に M02 で増えた未検証の前提、CLAUDE.md の構成・出力規約・mise タスクの節
- **実機確認(V07・V08 ほか)は完了条件に含めない**(ユーザー実施)。実 kintone・実 Cloudflare との通信、デプロイ、secret の登録はしない

## 対象範囲

### 構成(M02 で足す・変えるもの)
```
src/
  errors.ts                 エラー表の全コード(ERROR_CODES)、ErrorDetails、KintoneMcpError(details 付き)、ArgumentError、
                            ResponseTooLargeError、ResolverError(M03 のリゾルバーが投げる型。M02 では型とエンベロープだけ)、
                            toErrorEnvelope(details は空なら出さない)
  kintone/
    errors.ts               KintoneUnauthorizedError / KintoneApiError / KintoneCallLimitError と、HTTP 応答 → エラー表の分類(classify)
    client.ts               直列化・リトライ・maxApiCalls・10 MiB・タイムアウト・redact。get / post / put / delete
  mcp/
    registry.ts             ToolCategory / annotationsFor / TOOLSETS / defineTool / parseToolConfig / selectTools / registerTools / QueryError
    response-limit.ts       estimateTokens / resolveResponseLimit / assertWithinResponseLimit / maxResponseTokensShape
    tools/index.ts          ALL_TOOLS(M02 は kintone_apps_search だけ)
    tools/apps-search.ts    defineTool に載せ替え(limit のまま)
    server.ts               createMcpServer(deps: getClient / onKintoneUnauthorized / config?)。registerTools に委譲
    api-handler.ts          URL クエリを parseToolConfig で検査(400)し、createMcpServer に渡す
test/
  unit/    errors / error-table / kintone-client / response-limit / mcp-registry / tool-config / tools-list-snapshot / tool-apps-search / mcp-api-handler
  e2e/     mcp-tools.test.ts(新規)、verify-oauth.test.ts(クエリの差し替え)
  helpers/ fake-kintone-client.ts(ツールのテスト用の KintoneClient 偽物)、mock-fetch.ts(呼び出しの記録に signal / redirect を追加。既存の使い方は変えない)
  snapshots/tools-list.json
mise.toml                   test:update-snapshots の説明(「M02 から有効」を外す)。コマンドは M01 から入っている
```

### 1. エラー(`src/errors.ts`、`src/kintone/errors.ts`)
- `ERROR_CODES`: `argument_error` `authentication_error` `permission_error` `not_found` `rate_limited` `api_error` `response_too_large` `resolver_app_not_found` `resolver_app_ambiguous` `resolver_app_exact_match_required` `resolver_field_exact_match_required` `resolver_field_not_found` `resolver_field_ambiguous` `resolver_app_list_too_large` `generic_error`
- `ErrorDetails`: `http_status` `kintone_code` `kintone_id` `retry_after_sec` `kind`(`app` | `field`)`ref` `candidates`(アプリ `[{id, code?, name?}]`、フィールド `[{code, label?}]`)
- 分類(`classify(status, kintoneCode)`。仕様書 §6 の表の順): 401 → `authentication_error` / 403 → `permission_error` / 404 → `not_found` / 429 → `rate_limited`(retryable) / **400・422・kintone コードが `CB_VA` で始まるもの → `argument_error`**(HTTP ステータスは問わない。520 + `CB_VA01` も `argument_error`) / その他の 4xx・3xx → `api_error`(retryable でない) / 5xx → `api_error`(retryable) / 通信失敗・タイムアウト・JSON でない 200 → `api_error`(retryable)。401・403・404・429 はステータスが先(`kintone_code` が `CB_VA...` でもステータスの分類)
- kintone のエラー本文 `{code, id, message}` は `details.kintone_code` / `kintone_id` と message に載せる(利用者向けの文言なのでそのまま。ただし長さを 500 文字に切り、トークンを伏せる)
- 呼び出し回数の上限超過 → `api_error`(retryable でない)。応答が上限超過(10 MiB / トークン)→ `response_too_large`(retryable でない)

### 2. クライアント(`src/kintone/client.ts`)
- インターフェース: `get(path, query?)` / `post(path, body)` / `put(path, body)` / `delete(path, body)` / `redact(text)`。`path` は `/k/v1/...`。戻り値は JSON
- 状態(クライアントごと = ツール呼び出しごと): 呼び出しカウンター、直列化のキュー(`tail` の Promise)
- オプション: `host` `accessToken` `timeoutMs`(30000)`maxRetries`(2)`maxRetryWaitSeconds`(10)`maxApiCalls`(50)`sleep`(テストで差し替える待機)
- 1 回の試行 = カウンターを 1 増やして `fetch`。`redirect: "manual"`。タイムアウトは本文の読み取り終了までの 1 本のタイマー(`AbortController`)で、本文の読み取りも中断に反応する(止まったストリームで固まらない)
- 本文は 10 MiB(= 10 × 1024 × 1024 バイト)まで読む。エラー応答の本文は 64 KiB まで(超えたら本文なし扱い)
- リトライの判定は GET だけ。待機は `Retry-After`(`/^\d+$/`)か 1 秒。待機がカウンターを使うのは次の試行を始めるときだけ

### 3. ツール登録(`src/mcp/registry.ts`)
- logbutler の `registry.ts` の形(`defineTool` / `parseToolConfig` / `registerTools`)を、kintone の toolset(`app` `record`)と ToolContext(`client` だけ。M03 でリゾルバー・キャッシュを足す)に合わせて移す
- `defineTool` の入力: `name` `title`(日本語)`description` `category` `toolset` `inputSchema`(`z.strictObject`)`maxApiCalls?` `responseLimit?` `handler(args, ctx)`。ReadOnly は `inputSchema` に `max_response_tokens` を自動で足す(`responseLimit: false` で外せる)
- annotation: ReadOnly = `{readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: true}`、WriteIdempotent = `{false, false, true, true}`、WriteNonIdempotent = `{false, false, false, true}`、Destructive = `{false, true, true, true}`(順に read-only / destructive / idempotent / open-world)。`title` は日本語
- `/mcp` の URL クエリは `api-handler.ts` で `parseToolConfig` に通す。`QueryError` → 400。公開するツールの決定は tools/list の時点(接続 URL)で行う
- toolset と区分の組み合わせで 0 個になる場合は 400。M02 の実ツールは `kintone_apps_search`(toolset `app`)だけなので、実際の `ALL_TOOLS` に対して `?toolsets=record` は 400 になる(M03 で `kintone_records_query` が入ると通る)。このためクエリの検証 unit は**合成したツール一覧**(4 区分 × 2 toolset)で行い、実 `ALL_TOOLS` に依存する E2E は `?read_only=true` と `?toolsets=app` だけにする

### 4. `max_response_tokens`(`src/mcp/response-limit.ts`)
- logbutler の `response-limit.ts` と同じ推定式。`resolveResponseLimit`(未指定 → 50000、0 → 無効、負 → `argument_error`)は `kintone` を呼ぶ前に評価する

### 5. tools/list のスナップショット
- 実 `ALL_TOOLS` に対する `tools/list`(`createMcpHandler` 経由)の結果を `test/snapshots/tools-list.json` に保存(`toMatchFileSnapshot`)。ツールを足す・引数を変えるとこのファイルの差分としてレビューできる
- 更新: `mise run test:update-snapshots`(`vitest run test/unit/tools-list-snapshot.test.ts --project unit --update`。`--update` の後ろにファイルを置くとファイル指定が効かず unit 全体が走るので、この順にする)。CI(`CI=1`)では更新しない(差分があれば失敗)
- 追加の固定: 全ツール名が `kintone_` で始まる、ReadOnly だけが `max_response_tokens` を持つ(M03 / M04 で増えたツールにも効く)

### 6. 環境変数・バインディング
変更なし。

## TDD の進め方(小さい順。各段階で失敗を確認してから実装)

1. **エラー(Red から)**: `errors.test.ts`(エンベロープの形、`details` の省略、固定文、`ERROR_CODES` の網羅)と `error-table.test.ts`(§6 の全行。HTTP 行はモックの fetch + 実クライアント、ツール入力・応答サイズ・resolver 行はエンベロープ変換)。**期待する失敗**: `ERROR_CODES` に新コードがない・`details` が出ない・`ResolverError` / `ResponseTooLargeError` が存在しない
2. **クライアント(Red から)**: `kintone-client.test.ts` を拡張。直列化(deferred で 2 本目が待たされる)、リトライ(429 / 503 を 2 回までと 3 回目の打ち切り、`Retry-After` の読み方と 10 秒上限、待機の回数・秒数は注入した `sleep` で見る)、書き込みの非リトライ(POST / PUT / DELETE × 429 / 503)、`maxApiCalls`(リトライも数える)、10 MiB の境界、タイムアウト(進まない `fetch` と止まった本文ストリーム)、JSON でない 200、トークンの伏せ、リダイレクトを辿らない、クエリの配列・JSON 本文。**期待する失敗**: `post` / `put` / `delete` / `redact` がない、リトライしない、上限がない
3. **`max_response_tokens`(Red から)**: `response-limit.test.ts`。推定式(ASCII 4 文字 = 1、日本語 1 文字 = 1)、既定 50000、0 で無効、負・非整数は `argument_error`、上限ちょうどは通り 1 超えで `response_too_large`。**期待する失敗**: モジュールがない
4. **レジストリ(Red から)**: `tool-config.test.ts`(`parseToolConfig`)と `mcp-registry.test.ts`(`defineTool` の名前検査・4 区分の annotation・`max_response_tokens` の自動付与、`registerTools` の検証 / 401 / 想定外の例外 / 応答サイズ / `maxApiCalls` の受け渡し / 書き込みに上限なし / 重複)。**期待する失敗**: `registry.ts` がない
5. **ツールの載せ替えとスナップショット(Red から)**: `tool-apps-search.test.ts` に `max_response_tokens`(付与・負・小さい値で `response_too_large`)を足し、`tools-list-snapshot.test.ts` を作る。**期待する失敗**: `max_response_tokens` が公開スキーマにない・`ALL_TOOLS` がない。実装後に `mise run test:update-snapshots` でスナップショットを作り、中身をレビューしてからコミットする
6. **`/mcp` のクエリと E2E(Red から)**: `mcp-api-handler.test.ts` と `e2e/mcp-tools.test.ts`。M01 の `verify-oauth.test.ts` の `?x=1` は、`toolsets` / `read_only` の検証を入れる**前**に `?read_only=true` へ差し替える(差し替え後も緑であることを確かめてから実装する)。**期待する失敗**: 不正なクエリでも 200 になる
7. 全体: `mise run test` / `lint` / `build`。ドキュメント(ロードマップ・ADR・仕様書・manual-verification・CLAUDE.md)の更新

## エラー表と対応するテスト

仕様書 §6 の各行を `test/unit/error-table.test.ts` の 1 ケース以上で確認する。HTTP の行は実際のクライアントにモックの fetch を渡し、`toErrorEnvelope` まで通す。

| §6 の行 | `code` / `retryable` | テストで見ること |
|---|---|---|
| 401 | `authentication_error` / false | `KintoneUnauthorizedError`。`details.http_status` = 401、`kintone_code`。HTTP 401 への変換は `mcp-api-handler.test.ts`(M01 から) |
| 403 | `permission_error` / false | `details.http_status` = 403 |
| 404 | `not_found` / false | `details.kintone_code`・`kintone_id` が載る |
| 400・422・`CB_VA` 接頭辞 | `argument_error` / false | 400、422、520 + `CB_VA01`、403 + `CB_VA01` は(ステータスが先なので)`permission_error` |
| 429(リトライ後) | `rate_limited` / **true** | GET は 3 回目も 429 で終了、`retry_after_sec` 入り。書き込みは 1 回で `rate_limited` |
| その他の 4xx | `api_error` / false | 405・409・413。3xx(リダイレクト)も同じ扱い |
| 5xx | `api_error` / **true** | 500・502・520、503 は GET で 3 回試して終了 |
| 通信失敗・タイムアウト | `api_error` / true | `http_status` なし。message が固定文でトークンを含まない |
| 呼び出し回数の上限超過 | `api_error` / false | `maxApiCalls` に達した次の呼び出しは fetch しない |
| ツール入力の検証エラー | `argument_error` / false | `registerTools` 経由(不正な型・未知の引数・負の `max_response_tokens`)。kintone を呼ばない |
| 応答が上限超過 | `response_too_large` / false | トークン(`max_response_tokens`)と 10 MiB の 2 経路 |
| `resolver_app_not_found` | false | `details.kind` = `app`、`ref`、`candidates` は空配列 |
| `resolver_app_ambiguous` | false | `details.candidates` = `[{id, code, name}]`(空の項目は省略) |
| `resolver_field_exact_match_required` | false | `details.kind` = `field`、`candidates` = `[{code, label}]` |
| `resolver_app_exact_match_required` | false | `details.candidates` に部分一致の候補 |
| `resolver_field_not_found` | false | `details.kind` = `field` |
| `resolver_field_ambiguous` | false | `details.candidates` = `[{code, label}]` |
| `resolver_app_list_too_large` | false | `details.kind` = `app`、`ref` |
| 想定外の例外 | `generic_error` / false | 固定文 `Unexpected error.`、例外のメッセージ(トークン入り)が応答に出ない |

## 作るテストの一覧

| 種別 | ファイル | 内容 |
|---|---|---|
| unit | `errors.test.ts` | エンベロープの形(`schema_version` `error.code/message/retryable`)、`details` の省略・全キー、想定外の例外の固定文、`ERROR_CODES` の内容 |
| unit | `error-table.test.ts` | 上の表の全行。`ERROR_CODES` の全コードが表に現れる検査 |
| unit | `kintone-client.test.ts` | M01 の既存ケース(認証ヘッダー・クエリ・401・トークンを含めない・通信失敗)+ 直列化、リトライ(429 / 503、回数・`Retry-After`・上限 10 秒)、書き込みの非リトライ、`maxApiCalls`、10 MiB(境界・`Content-Length`・ストリーム打ち切り)、タイムアウト(fetch・本文)、JSON でない 200 / 空の本文、`redact`、リダイレクト、クエリの配列、JSON 本文(POST / PUT / DELETE) |
| unit | `response-limit.test.ts` | 推定式、既定値、0、負・非整数、境界 |
| unit | `tool-config.test.ts` | `parseToolConfig`(`read_only` の真偽・不正・重複、`toolsets` の複数指定・大文字小文字・`all`・未知、綴り違いのキー、無関係なキーを無視、0 ツール)と `selectTools`(4 区分 × 2 toolset) |
| unit | `mcp-registry.test.ts` | `defineTool`(名前、annotation 4 区分、`max_response_tokens` の付与と除外)、`registerTools`(検証・401・想定外の例外とログのトークン伏せ・応答サイズ・`maxApiCalls`・重複) |
| unit | `tools-list-snapshot.test.ts` | `tools/list` のファイルスナップショット、名前の接頭辞、`max_response_tokens` が ReadOnly だけにあること |
| unit | `tool-apps-search.test.ts` | M01 の全ケースを維持 + `max_response_tokens`(公開スキーマ・負・`response_too_large`) |
| unit | `mcp-api-handler.test.ts` | M01 の全ケースを維持 + クエリ(`read_only=true` で `tools/list` が読み取りだけ、不正は 400 で kintone を呼ばない、GET でも 400) |
| e2e | `mcp-tools.test.ts` | `?read_only=true` / `?toolsets=app` の通し、不正なクエリの 400、kintone 429 のリトライ成功と `rate_limited`、`max_response_tokens` |
| e2e | `verify-oauth.test.ts` | クエリ付き URL のテストを `?read_only=true` に差し替え(意図は同じ) |

## やってはいけないこと
- `kintone_app_describe` / `kintone_records_query` / 書き込みツール、リゾルバー(`app_ref` の解決)、KV キャッシュ、冪等キャッシュ(M03 / M04)、`kintone_apps_search` の引数の拡張(M03)
- 書き込み(POST / PUT / DELETE)のリトライ、並列リクエスト、`maxApiCalls` の既定を 50 から変える
- 応答の切り詰め(`response_too_large` にする)、書き込みツールへの `max_response_tokens` の付与
- トークン・クライアントシークレットをログ・エラー・応答・スナップショットに出す
- テストを弱める・skip する・消す。スナップショットを `--update` で書き換えて差分をレビューせずにコミットする
- 実 kintone・実 Cloudflare への通信、デプロイ、secret の登録(`mock-fetch` は未登録の宛先への通信を例外にする)
- 依存の追加(必要になったら理由を添えて報告する。M02 は不要の見込み)
- Durable Objects / McpAgent、`structuredContent` の使用
- logbutler リポジトリの変更

## 未検証の前提(実機でしか分からないもの。ブロッカーにせず、`docs/manual-verification.md` に追記して進む)
- kintone の 429 に `Retry-After` が付くか、書き込みの 429 が「未処理」か(V08)。付かなければ 1 秒待ちで 2 回リトライする
- kintone の実際のエラーのステータスとコードの対応(V07)。`CB_VA` 接頭辞のものが 400 以外(520 など)で返るか
- 200 なのに JSON でない応答が実際に起きるか(IP 制限・プロキシの HTML など)。起きたときの分類(通信失敗扱い = retryable)が妥当か(V07 に追記)
- KV の読み書きが subrequest の 50 に数えられるか(V12。M03 のキャッシュで効く)
- tools/list のスナップショットは `agents` / `@modelcontextprotocol/server` の出力形式に依存する(依存を上げたら差分が出る。意図した変更かをレビューして更新する)

## 成果物
- 上記のコードとテスト、`test/snapshots/tools-list.json`
- `docs/adr/0006`・`0008`・`0009` を「提案」から「採用(M02)」に更新(実装で変えた点を追記)、仕様書(状態・§6・§8 の補足)
- `docs/manual-verification.md` の更新(M02 で増えた前提)
- ロードマップの M02 を `[x]` にする。CLAUDE.md の構成・出力規約・mise タスクの節を更新

## 実装後の記録(計画との差分)

計画から外れた点と、実装中に決めた細部。ADR 0006 / 0008 / 0009 と仕様書の §6・§8 にも反映した。

- **`mise run test:update-snapshots` のコマンドの順序**: M01 の `--update test/unit/...` はファイル指定が効かず unit 全体が走ったため、`vitest run test/unit/tools-list-snapshot.test.ts --project unit --update` に直した(説明文の「M02 から有効」も外した)。
- **`fetch` を中断シグナルと競合させる**: 計画は「本文の読み取りも中断に反応する」だけだったが、モックの `fetch` は `signal` に応じないため、`fetch` 自体も `AbortSignal` と競合させてタイムアウトを確実にした(本物の workerd でも害はない)。
- **上限に達したらリトライの待機をしない**: 呼び出し回数の上限は「次の試行を始めるとき」に見る計画だったが、待ったあとに上限エラーになるのは無駄なので、待機の前に判定する。
- **`Retry-After` は 429 だけでなく 503 でも `details.retry_after_sec` に載せる**(読めたときだけ)。
- **kintone の `id` にもトークンの伏せを適用**(`code` `id` `message` のすべて)。
- **既存テストの変更**: `tool-apps-search.test.ts` の「公開スキーマの引数」は `["limit"]` から `["limit", "max_response_tokens"]` に変えた(読み取りツールへの自動付与という M02 の仕様変更によるもので、検証を弱めたのではなく期待値を仕様に合わせた)。`kintone-client.test.ts` は M01 の全ケースを残して拡張した(`make` を、待機を記録する `setup` に置き換え)。
- **M02 の実 `ALL_TOOLS` では `?toolsets=record` が 400**(公開ツールが 0 個)。M03 で `kintone_records_query` が入ったら `mcp-api-handler.test.ts` の「公開ツールが 0 個になる組み合わせ」のケースは別の組み合わせ(例: `toolsets=record&read_only=true` が書き込みしかない場合)に変える。
- **依存の追加なし**。

