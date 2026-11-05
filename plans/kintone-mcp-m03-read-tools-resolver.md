# M03: 読み取りツールと名前解決

ロードマップ: `plans/kintone-mcp-roadmap.md`(M03 の行)
仕様: `docs/specs/kintone-mcp-spec.md`(§5.1〜5.3 読み取りツール、§6 エラー表、§7 名前解決、§8 制限値、§10 キャッシュ)
設計判断: `docs/adr/0006`(出力・エラー形式)、`0007`(名前解決とキャッシュ)、`0008`(上限・最悪ケースの式)、`0010`(grant 単位のキー)
書式の手本: `plans/kintone-mcp-m02-client-and-foundation.md`。M02 で作った `defineTool` / `registerTools` / `ToolContext` / `max_response_tokens` / tools/list のスナップショットの上に載せる
旧 Go 版の参照(`archive/go`): `internal/resolver/app.go` `field.go`、`internal/service/operations/{app_describe,records_query,resolver}.go`。仕様書と違う点は仕様書に従う

## 目的

M02 までは `kintone_apps_search`(`limit` だけ)しか動かない。M03 で読み取りツール 3 つを仕様どおりに揃え、
名前解決(`app_ref`・フィールド参照)と、その結果を 10 分だけ持つ KV キャッシュを作る。
書き込みツール(M04)がそのまま使えるよう、リゾルバーの `mode: "write"`(完全一致だけで解決する)も M03 で実装し、unit で固定する。**書き込みツール自体と冪等キャッシュ `idem:` は作らない**。

## 完了条件

- `mise run test`(unit + e2e)と `mise run lint`(Biome + tsc 2 種)と `mise run build` が通る。検証コマンドは `pnpm exec vitest run --project unit --project node` / `--project e2e` / `pnpm exec biome check .` / `pnpm exec tsc --noEmit` / `pnpm exec tsc --noEmit -p scripts/tsconfig.json`
- **`kintone_apps_search`(仕様書 §5.1)**: 引数 `ids`(正の整数、100 件まで)`codes`(1〜64 文字、100 件まで)`name`(64 文字まで)`space_ids`(100 件まで)`limit`(1〜100)`offset`(0 以上)`max_response_tokens`。kintone の `ids[n]` `codes[n]` `name` `spaceIds[n]` `limit` `offset` へ対応づけ、生の応答をそのまま返す。範囲外・未知の引数は kintone を呼ばずに `argument_error`。1 回の呼び出しで 1 ページだけ。リゾルバー・キャッシュは使わない(読まない・書かない)
- **`kintone_app_describe`(§5.2)**: `app` / `app_ref`(どちらか 1 つ必須・排他)と `lang`(`ja` `en` `zh` `user` `default`)。`GET /k/v1/app.json?id=` → `GET /k/v1/app/form/fields.json?app=&lang=` の順。出力 `{app:{app_id,code,name,description,space_id?,thread_id?,created_at?,creator?,modified_at?,modifier?},fields:{<コード>:{...生}},revision?}`(`app_id` `code` `name` `description` は常に出し、ほかは空・null なら省略。Go の `omitempty` と同じ)。**キャッシュを読まない**(`app_ref` の解決でも読まず、常に最新を取る)。取得したフィールド定義でフィールドのキャッシュを更新する
- **`kintone_records_query`(§5.3)**: `app` / `app_ref`、`query`、`fields`(100 件まで。`fields[0]`〜`fields[99]`)、`total_count`。`GET /k/v1/records.json`(`totalCount=true`)。出力 `{records:[...生],total_count?:数値}`(kintone の文字列の `totalCount` を数値に変換。指定しなければ出さない)。1 回の呼び出しで 1 リクエスト、自動ページングなし
- **`app` / `app_ref`**: どちらか 1 つ必須・排他。両方・どちらもなしは `argument_error`(kintone を呼ばない)。`app` は正の整数、`app_ref` は 1 文字以上の文字列。公開スキーマの `description` にも排他と書く
- **リゾルバー(§7)**(`src/resolver/`):
  1. アプリ(`resolveApp(ref, {mode})`): 空文字は `argument_error` → **正の整数に読める文字列は ID としてそのまま採用**(API を呼ばない。数字だけのコードより優先)→ **コード完全一致**(`codes[0]=<ref>&limit=100`)→ **名前完全一致** → **名前部分一致**。各段階でヒットしたら即 return し、次の段階へはフォールバックしない。コード 1 件 → 採用、2 件以上 → `resolver_app_ambiguous`
  2. 名前の検索は `name=<ref>&limit=100&offset=` を 100 件ずつ、**最大 10 ページ**。10 ページ全部が満杯なら `resolver_app_list_too_large`(`details.kind=app, ref`)。取得結果のうち `name === ref`(大文字小文字を区別)が完全一致、残りが部分一致
  3. **read モード**: 完全一致 1 件 → 採用、2 件以上 → `resolver_app_ambiguous`。完全一致 0 件なら、部分一致 1 件 → 採用、2 件以上 → `ambiguous`、0 件 → `resolver_app_not_found`
  4. **write モード**: 完全一致は read と同じ。完全一致 0 件で部分一致が 1 件以上 → **採用せず** `resolver_app_exact_match_required`(`details.candidates` に部分一致の候補。1 件でも複数件でも)、0 件 → `resolver_app_not_found`。ID・コード完全一致は両モード共通
  5. フィールド(`resolveField(appId, ref, {mode})`): `GET /k/v1/app/form/fields.json?app=` の `properties`(トップレベルだけ)から、**コード完全一致 → label 完全一致 → label 部分一致(大文字小文字を区別)**。各段階でヒットしたら即 return。完全一致が複数なら `resolver_field_ambiguous`。read: 部分一致 1 件採用・複数 `ambiguous`。**write: 部分一致しか当たらなければ 1 件でも複数件でも `resolver_field_exact_match_required`**。なし → `resolver_field_not_found`。候補はコード順
  6. エラーの `details`: `kind`(`app` / `field`)`ref` `candidates`(アプリ `[{id, code?, name?}]`、フィールド `[{code, label?}]`。空の項目は省略。`not_found` は空配列)
- **KV キャッシュ(§10)**(`src/resolver/cache.ts`):
  1. 置き場は `OAUTH_KV`。キーは `kcache:v1:<userId>:<grantId>:appcode:<h>` / `appname:<h>` / `fields:<appId>`(`<h>` は `ref` の SHA-256 の先頭 16 文字の 16 進)。`userId:grantId` は MCP アクセストークン(`{userId}:{grantId}:{secret}`)から取り出す。email 単位にしない。別 grant とは共有しない。トークンの形式が違う(grant が分からない)ときはキャッシュを使わない
  2. 値は最小限(アプリ `{appId, code, name}` の配列、フィールド `{code, label, type}` の配列)。**TTL 600 秒**(`expirationTtl: 600`)
  3. アプリ名・コードの検索は段階(`code` / `name`)ごとに結果全体(空配列も)をキャッシュする
  4. **`not_found` の取り直し**: キャッシュ由来の解決が `not_found`(アプリ・フィールドとも)になったら、**キャッシュから読んだ段階だけ**キャッシュを無視して 1 回だけ取り直す。1 回の解決で同じ段階の API を 2 度は呼ばない(メモ化)ので、取り直しを含めても最悪の呼び出し回数 A(下の式)を超えない。取り直しで見つからなければそのままエラー
  5. キャッシュで解決したアプリへの kintone の呼び出しが `not_found`(HTTP 404)を返したら、そのアプリのキャッシュ(読んだ段階のキーと `fields:<appId>`)を消してエラーを返す(再解決はしない)
  6. **KV の失敗(get / put / delete の例外)はツール呼び出しを止めない**(キャッシュなしで続行。ログは例外名だけ。トークン・キー・値は出さない)。キャッシュの値が壊れている・形が違う場合は「なし」として扱う
  7. 読む: 名前解決(`app_ref` とフィールド参照)。**`kintone_apps_search` と `kintone_app_describe` は読まない**。書く: 名前解決で API から取ったとき、`kintone_app_describe` がフィールド定義を取ったとき
- **最悪ケースと `maxApiCalls`(ADR 0008)**: 論理呼び出し数の式 `A = コード 1 + 名前 10 = 11`、`F = 1`。ツールごとに `maxApiCalls` を**明示**し、`論理最悪 × (1 + リトライ 2) ` に固定する(読み取りは 1 呼び出しあたり最大 3 回試行):

  | ツール | 論理最悪の式 | 論理 | `maxApiCalls`(論理 × 3) |
  |---|---|---|---|
  | `kintone_apps_search` | 1 | 1 | **3** |
  | `kintone_app_describe` | A + app.json 1 + fields.json 1 | 13 | **39** |
  | `kintone_records_query` | A + records.json 1 | 12 | **36** |

  unit で、(a) 各ツールの宣言値が上の値であること、(b) 全ページ満杯のモックで数えた論理呼び出し数が式どおり(13 / 12 / 1)であること、(c) すべての呼び出しが 429 を 2 回返してから成功する最悪のリトライでも、試行数が宣言値ちょうどで成功すること、(d) 名前検索が 10 ページ満杯なら 11 回で `resolver_app_list_too_large` で止まること、(e) キャッシュの取り直しを含めても解決が A=11 以内に収まること、を固定する。M04 の書き込みは式の `× 3` がない(書き込みはリトライしない)ので、M04 で式から決める
- **`max_response_tokens` の公開スキーマに `minimum: 0` が出る**(M02 の低優先の指摘)。zod の `.int()` が出す `minimum: -9007199254740991` を、`.meta({ minimum: 0 })` で 0 に上書きする。**検証の挙動は変えない**(スキーマの `safeParse` は負の値を通し、`resolveResponseLimit` が kintone を呼ぶ前に `argument_error` にする。M02 のテストはそのまま通る)
- **E2E**(Worker 全体への HTTP。`test/helpers/oauth-flow.ts`): `/register` → `/authorize` → `/callback` → `/token` → `/mcp` で、3 ツールの `tools/call` を通す。
  - `apps_search`: `ids` `codes` `name` `space_ids` `limit` `offset` が kintone のクエリになる
  - `app_describe`: `app` と `app_ref`(コード・名前)。出力の形。KV にフィールドのキャッシュができる
  - `records_query`: `app_ref` で解決 → `fields` `query` `total_count`。`total_count` が数値
  - キャッシュ: 同じ `app_ref` の 2 回目は `apps.json` を呼ばない。キーに `<userId>:<grantId>` が入る。別の利用者(別 grant)とは共有しない。`app_describe` は常に kintone を呼ぶ。TTL 600
  - リゾルバーのエラー(`resolver_app_not_found` / `ambiguous` の `details.candidates`、`resolver_app_list_too_large`)。`app` と `app_ref` の排他違反
  - `tools/list` が 3 ツール。`?toolsets=app` は 2 ツール、`?toolsets=record` は 1 ツール
- **tools/list のスナップショットを意図して更新する**(`mise run test:update-snapshots` → `git diff` で引数・description・annotation を確認してからコミット)
- ドキュメント: ロードマップの M03 を `[x]`、ADR 0007 / 0008 を「採用(M03)」に更新(実装で変えた点を追記)、仕様書の「状態」と §5〜§10 の補足、`docs/manual-verification.md` に M03 で増えた未検証の前提、CLAUDE.md の構成・ツールの節。本計画の末尾に「実装後の記録(計画との差分)」を書く
- **実機確認(V09・V11・V12 ほか)は完了条件に含めない**(ユーザー実施)。実 kintone・実 Cloudflare との通信、デプロイ、secret の登録はしない

## 対象範囲

### 構成(M03 で足す・変えるもの)
```
src/
  resolver/
    cache.ts                ResolverCache(OAUTH_KV。grant 単位のキー・TTL 600 秒・失敗は握りつぶして続行)、GrantRef、キー生成
    resolver.ts             createResolver({client, cache}): resolveApp / resolveField(mode: read | write)。10 ページ上限、candidates、
                            not_found の取り直し(段階ごと・メモ化)。fieldEntriesOf(properties)
    budget.ts               最悪ケースの式(A・F・リトライ係数)と readBudget(論理回数)
  mcp/
    registry.ts             ToolContext に resolver / cache を足す。RegisterDeps に kv? / grant? を足す(省略時はキャッシュなし)
    server.ts               kv / grant を registerTools へ渡す(型は RegisterDeps の継承)
    api-handler.ts          kv = env.OAUTH_KV、grant = grantRefFromAccessToken(ctx.auth.token) を渡す。createMcpApiHandler({tools?}) でテスト用に公開候補を差し替えられる
    response-limit.ts       max_response_tokens の公開スキーマに minimum: 0(検証は変えない)
    tools/
      app-target.ts         appTargetShape(app / app_ref)、withResolvedApp(排他検査・解決・キャッシュ由来の not_found でキャッシュを消す)
      apps-search.ts        引数拡張
      app-describe.ts       新規
      records-query.ts      新規
      index.ts              ALL_TOOLS = [appsSearch, appDescribe, recordsQuery]
test/
  unit/    resolver-cache / resolver-app / resolver-field / resolver-cache-behavior / call-budget / tool-apps-search(拡張)/ tool-app-describe / tool-records-query /
           response-limit(スキーマ)/ tools-list-snapshot(3 ツール)/ mcp-api-handler(公開ツールの更新)
  e2e/     read-tools.test.ts(新規)、mcp-tools.test.ts(公開ツールの一覧の更新)
  helpers/ fake-kv.ts(メモリ上の KV。呼び出しの記録と失敗の注入)、oauth-flow.ts(setupMocks に追加の宛先を渡せるようにする)
  snapshots/tools-list.json
```

### 1. キャッシュ(`src/resolver/cache.ts`)
- `createResolverCache(kv | undefined, grant | undefined): ResolverCache`。どちらかが欠ければ何もしない実装(読みは常に「なし」、書き・削除は何もしない)
- `getApps(stage, ref)` / `putApps(stage, ref, apps)` / `deleteApps(stage, ref)` / `getFields(appId)` / `putFields(appId, fields)` / `deleteFields(appId)`。すべて失敗しない(例外は握りつぶして `console.warn` に例外名だけ)
- 読んだ値は形を検査する(配列で、各要素が文字列の `appId` `code` `name` を持つ。フィールドは `code` `label` `type`)。不正なら「なし」

### 2. リゾルバー(`src/resolver/resolver.ts`)
- 入力はクライアントとキャッシュだけ。1 回のツール呼び出しにつき 1 つ作る(メモ化はその中だけ)
- `resolveApp(ref, {mode, useCache = true})` → `{appId: number, fromCache: boolean, invalidate(): Promise<void>}`。`useCache: false` はキャッシュを読まない(書きは行う)。`app_describe` が使う
- `resolveField(appId, ref, {mode, useCache = true})` → フィールドコード(文字列)
- `appId` が正の整数に読めない・安全な整数でない `appId` が API から返った場合は `resolver_app_not_found`

### 3. ツール共通の `app` / `app_ref`(`src/mcp/tools/app-target.ts`)
- `appTargetShape = { app: z.number().int().positive().optional(), app_ref: z.string().min(1).optional() }`(`description` に「どちらか 1 つ。`app_ref` はアプリ ID・コード・名前(読み取りは部分一致も可)」)
- `withResolvedApp(args, ctx, {mode, useCache}, run)`: 排他検査 → `app` ならそのまま `run`、`app_ref` なら解決して `run`。`run` が `not_found` を返し、`fromCache` ならキャッシュを消してそのまま投げる

### 4. 環境変数・バインディング
変更なし(`OAUTH_KV` をキャッシュにも使う)。

## TDD の進め方(小さい順。各段階で失敗を確認してから実装)

1. **`max_response_tokens` の `minimum: 0`**: `response-limit.test.ts` に公開スキーマの `minimum` が 0 であること、検証は負を通すこと(既存のアサーションを保つ)を足す。**期待する失敗**: `minimum` が -9007199254740991
2. **キャッシュ(Red から)**: `resolver-cache.test.ts` とヘルパー `fake-kv.ts`。キー形式・TTL 600・grant 別の分離・値の検査・KV の例外を握りつぶすこと・ログに値を出さないこと。**期待する失敗**: モジュールがない
3. **リゾルバー(アプリ)**: `resolver-app.test.ts`。解決順序(ID → コード → 名前完全一致 → 部分一致)、各段階で止まること(後ろの API を呼ばない)、ID は API を呼ばない、read / write の全分岐、10 ページ(満杯・9 ページ目で終わる・ちょうど 10 ページ目で終わる境界)、`candidates` の形。**期待する失敗**: モジュールがない
4. **リゾルバー(フィールド)**: `resolver-field.test.ts`。コード → label 完全 → label 部分、大文字小文字の区別、サブテーブル内を探さない、read / write、`candidates` のコード順
5. **リゾルバー + キャッシュ**: `resolver-cache-behavior.test.ts`。ヒットで API を呼ばない・`not_found` の取り直し(段階ごと・最悪 11 回以内・KV の書き込みが失敗しても二重に取らない)・KV の get が失敗しても解決できる・`useCache: false`・キャッシュ由来のアプリの `not_found` でキャッシュが消える
6. **ツール(Red から)**: `tool-apps-search.test.ts`(拡張)、`tool-app-describe.test.ts`、`tool-records-query.test.ts`。引数の検証、kintone へのクエリ、出力の形、排他、`max_response_tokens` の付与、キャッシュを読まない(app_describe・apps_search)。既存の `tool-apps-search.test.ts` は、公開ツールの絞り込み(`tools: [appsSearch]`)と「未知の引数」の例(`name` は有効な引数になったので `nope` に)だけ直し、それ以外のアサーションは残す
7. **最悪ケース**: `call-budget.test.ts`。実クライアント + `mockOutboundFetch` で数える。宣言値・論理回数・最悪のリトライ・10 ページ満杯・取り直し
8. **スナップショットと公開ツール**: `tools-list-snapshot.test.ts` に 3 ツールの名前・`app` / `app_ref` の説明・`max_response_tokens` の `minimum: 0` を足す。`mcp-api-handler.test.ts` の公開ツールの一覧を更新(`?toolsets=record` は 0 ツールでなくなった。0 ツールの 400 は、書き込みしかない合成ツール一覧を `createMcpApiHandler({tools})` に渡して保つ)
9. **E2E(Red から)**: `read-tools.test.ts`、`mcp-tools.test.ts` の公開ツール一覧
10. 実装後: `mise run test:update-snapshots` でスナップショットを更新して差分をレビュー → 全体(`mise run test` / `lint` / `build`)→ ドキュメント更新

## 作るテストの一覧

| 種別 | ファイル | 内容 |
|---|---|---|
| unit | `resolver-cache.test.ts` | キー(`kcache:v1:<userId>:<grantId>:...`、ハッシュ 16 文字)、TTL 600、grant 別の分離、値の形の検査、KV の失敗(get / put / delete)で例外を出さない、ログに値を出さない、KV・grant なしは何もしない |
| unit | `resolver-app.test.ts` | 解決順序と即 return、ID の直接採用(数字だけのコードより優先・API 呼び出し 0)、コード 1 件 / 複数、名前の完全 / 部分、read / write 全分岐、ページ上限の境界、`candidates`、空文字 |
| unit | `resolver-field.test.ts` | コード → label 完全 → 部分、大文字小文字、トップレベルのみ、read / write、`candidates` のコード順、`fields.json` を 1 回だけ呼ぶ |
| unit | `resolver-cache-behavior.test.ts` | キャッシュヒット、`not_found` の取り直し(1 回だけ・段階ごと・A 以内)、KV の失敗でも続行、`useCache: false`、アプリの `not_found` でキャッシュを消す |
| unit | `tool-apps-search.test.ts` | M02 の全ケースを維持 + 新引数の対応づけ・範囲・配列の上限・キャッシュを使わない |
| unit | `tool-app-describe.test.ts` | 引数の排他、呼び出し順(app.json → fields.json)、出力の形(空の項目の省略)、`lang`、フィールドのキャッシュ更新、キャッシュを読まない、`resolver_*` のエラー |
| unit | `tool-records-query.test.ts` | 引数の検証(`fields` 100 件)、クエリの対応づけ(`fields[n]` `totalCount`)、`total_count` の数値化、解決した ID で呼ぶ、キャッシュ由来の 404 でキャッシュを消す |
| unit | `call-budget.test.ts` | 宣言値 3 / 39 / 36、論理回数 1 / 13 / 12、最悪のリトライ、10 ページ満杯 = 11 回で止まる、取り直しでも 11 回以内 |
| unit | `response-limit.test.ts` | 公開スキーマの `minimum: 0` を追加(既存の検証のアサーションは維持) |
| unit | `tools-list-snapshot.test.ts` / `mcp-api-handler.test.ts` | 3 ツール、`app` / `app_ref`、公開ツールの一覧の更新 |
| e2e | `read-tools.test.ts` | 3 ツールの `tools/call`、キャッシュ(2 回目・grant 別・TTL)、リゾルバーのエラー、排他 |
| e2e | `mcp-tools.test.ts` | 公開ツールの一覧を 3 ツールに更新 |

## やってはいけないこと
- 書き込みツール(`record_create` / `record_update` / `record_delete`)、冪等キャッシュ `idem:`、入力上限(100 件)の送信前検査のうち書き込み分(M04)。リゾルバーの `mode: "write"` は作るが、ツールからは使わない
- `kintone_apps_search` / `kintone_app_describe` がキャッシュを読むこと
- キャッシュのキーを email 単位(`userId` だけ)にすること、別 grant で共有すること
- 書き込み(POST / PUT / DELETE)のリトライ、並列リクエスト、名前検索のページ上限を 10 より増やすこと(増やすなら `maxApiCalls` の式から再計算する)
- トークン・クライアントシークレットをログ・エラー・応答・スナップショットに出す。KV のキー・値(アプリ名など)をログに出す
- テストを弱める・skip する・消す。スナップショットを `--update` で書き換えて差分をレビューせずにコミットする
- 実 kintone・実 Cloudflare への通信、デプロイ、secret の登録
- 依存の追加、Durable Objects / McpAgent、`structuredContent` の使用

## 未検証の前提(実機でしか分からないもの。ブロッカーにせず、`docs/manual-verification.md` に追記して進む)
- `form/fields.json` の必要権限(V09)。一般の利用者で `app_describe` が `permission_error` になるか
- `GET` の URL 長(`fields` 100 個 + 長い `query`。V11)
- KV の読み書きが subrequest の 50 に数えられるか(V12)。数えられる場合、`app_describe` の最悪(API 39 + KV の読み書き)が 50 を超えうる。M03 は KV の呼び出しを `maxApiCalls` に含めない(Workers の subrequest とは別に数える)
- kintone の `apps.json` の `codes` / `name` の長さの上限(64 文字)を超える `app_ref` を渡したときの応答(400 になる想定。そのまま `argument_error`)
- `apps.json` の `name` の部分一致が実際に大文字小文字を区別しないか、全角半角をどう扱うか
- kintone の `totalCount` の実際の型(文字列の想定。数値で返っても `Number()` で数値になる)

## 成果物
- 上記のコードとテスト、`test/snapshots/tools-list.json`
- ADR 0007 / 0008 を「採用(M03)」に更新(実装で変えた点を追記)、仕様書(状態・§5〜§10 の補足)
- `docs/manual-verification.md` の更新(M03 で増えた前提)
- ロードマップの M03 を `[x]` にする。CLAUDE.md の構成・ツールの節を更新
- 本ファイルの末尾に「実装後の記録(計画との差分)」

## 実装後の記録(計画との差分)

計画から外れた点と、実装中に決めた細部。ADR 0006 / 0007 / 0008 と仕様書の §5・§8・§10、CLAUDE.md にも反映した。

- **`maxApiCalls` を「既定の 50 のまま」から「論理最悪 × 3 を明示」に変更**(3 / 39 / 36)。ADR 0008 は M02 時点で「50 のまま・ツールごとの個別値は却下」だったが、M03 の指示(式から決めた値を明示してテストで固定)に合わせ、ADR と仕様書の該当箇所を更新した。理由は、読み取りの最悪の試行数は論理 × 3 そのものなので、50 のような緩い上限は実装の誤りで呼び出しが増えても気づけないため。
- **`app_describe` は `app_ref` の解決でもキャッシュを読まない**。ADR 0007 の「`kintone_app_describe` は読まない」を、フィールド定義だけでなくアプリの解決にも適用した(結果は書く)。仕様書 §10 の「読む」の記述はツール単位の指示と読み、計画の完了条件に明記した。
- **`not_found` の取り直しを「読んだ段階だけ」に絞った**。計画の段階では「キャッシュを無視して取り直す」だけだったが、全段階を取り直すと(コードだけがキャッシュ由来で名前を 10 ページ取ったあとだと)合計が A = 11 を超えうるので、段階ごとに取り直し、1 回の解決で同じ段階の API を 2 度呼ばない(メモ化)ことにした。
- **既存テストの変更(アサーションは同等以上を残した)**:
  - `tool-apps-search.test.ts`: `createMcpServer` に `tools: [appsSearch]` を渡して「このファイルは apps_search だけ」を保った。公開スキーマの引数の期待値を `["limit","max_response_tokens"]` から §5.1 の 7 つに更新。「未知の引数」の例を `name`(有効な引数になった)から `nope` に変更
  - `mcp-api-handler.test.ts`: 有効なクエリの `tools/list` の期待値を 1 ツールから 3 ツールに更新。`?toolsets=record` は公開ツール 0 個ではなくなったので 400 の一覧から外し(`?toolsets=record` が通ることは別のテストで確認)、0 ツールの 400 は `createMcpApiHandler({ tools })` に書き込みだけの合成ツール一覧を渡して同じ検査を残した
  - `e2e/mcp-tools.test.ts`: クエリごとの `tools/list` の期待値を更新(`?toolsets=record` を追加)
  - `e2e/oauth-flow.test.ts`・`e2e/verify-oauth.test.ts`: `tools/list` のツール名(1 → 3)とツール数(1 → 3)の期待値を更新
  - `test/helpers/oauth-flow.ts`: `setupMocks` に追加の宛先(`extra`)を渡せるようにした(既存の使い方は変えない)
- `createMcpApiHandler` に `{ tools }` の任意引数を足した(テスト用。本番は引数なしで `ALL_TOOLS`)。
- `RegisterDeps` に `kv?` / `grant?` を足した。`registerTools` が `tools/call` ごとにキャッシュとリゾルバーを作って `ToolContext`(`client` `resolver` `cache`)で渡す。
- 計画のテスト名 `resolver-app` などはそのまま作り、補助として `test/helpers/{fake-kv,fake-kintone-data,tool-harness}.ts` を足した。
- **依存の追加なし**。`wrangler deploy --dry-run` でバンドルできることを確認した。

### Red / Green

| 段階 | Red(失敗の理由) | Green |
|---|---|---|
| 計画 | - | `plans/kintone-mcp-m03-read-tools-resolver.md` |
| テスト一式 | 新規 8 ファイルは `src/resolver/*` が存在せず import で失敗。既存の更新分は期待どおりの理由で失敗(`tool-apps-search` の引数一覧・新引数の対応づけ、`response-limit` の `minimum` が -9007199254740991、`tools-list-snapshot` の 3 ツール・`minimum: 0`、`mcp-api-handler` の公開ツール・`createMcpApiHandler({tools})`・kcache のキー、E2E の 20 件) | 実装後に unit 640 / e2e 133 が緑 |
| 実装 | - | `src/resolver/{budget,cache,resolver}.ts`、`src/mcp/tools/{app-target,app-describe,records-query,apps-search,index}.ts`、`registry.ts` / `api-handler.ts` / `response-limit.ts` の変更 |
| スナップショット | `tools-list.json` が古い(1 ツール) | `--update` のあと差分を確認(3 ツール、`minimum: 0`) |
| 既存 E2E の更新 | `oauth-flow` / `verify-oauth` がツール数 1 を期待 | 3 に更新して緑 |

### 未検証の前提(追加)
- V22(名前検索の部分一致の挙動・長さの上限)と V23(`totalCount` の型)を `docs/manual-verification.md` に追加した。V09・V11・V12 は M03 のツールが実際に使うので、実機確認の優先度が上がった(`app_describe` の権限、`records_query` の URL 長、KV と subrequest)。
