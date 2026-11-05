# M04: 書き込みツール

ロードマップ: `plans/kintone-mcp-roadmap.md`(M04 の行)
仕様: `docs/specs/kintone-mcp-spec.md`(§5.4〜5.6 書き込みツール、§6 エラー表、§7 名前解決の write モード、§8 制限値・最悪ケース、§10 キャッシュ)
設計判断: `docs/adr/0006`(出力・エラー形式)、`0007`(名前解決。書き込み系は完全一致だけ)、`0008`(上限・最悪ケースの式)、`0009`(冪等キャッシュ `idem:` の採用)、`0010`(grant 単位のキー)
書式の手本: `plans/kintone-mcp-m03-read-tools-resolver.md`。M02 の `defineTool` / `registerTools` / `ToolContext` と、M03 の `withResolvedApp` / リゾルバー(`mode: "write"`)の上に載せる
旧 Go 版の参照(`archive/go` タグ): `internal/kintoneapi/{records_write,record_write}.go`(`BuildInsertRecordsBody` / `BuildUpdateRecordBody` / `BuildDeleteRecordsBody`)、`internal/service/operations/{record_create,record_update,record_delete}.go`。仕様書と違う点は仕様書に従う
姉妹プロジェクト logbutler の参照: `docs/adr/0005-idempotency-cache-on-kv.md` と `src/mcp/idempotency.ts`(公開リポジトリ。冪等キャッシュの設計の手本。キーの形と「明示キーの扱い」は kintone 向けに変える。下記)

## 目的

M03 までで読み取り 3 ツール・リゾルバー(read / write モード)・KV キャッシュができた。M04 で書き込みツール 3 つ
(`kintone_record_create` / `kintone_record_update` / `kintone_record_delete`)を仕様どおりに作る。
書き込みは取り返しがつかないので、次の 4 点を設計の中心に置く。

1. **完全一致だけで解決する**: `app_ref` と `update_key_field_ref` は write モード。部分一致しか当たらなければ、kintone の書き込み API を呼ばずに `resolver_app_exact_match_required` / `resolver_field_exact_match_required` を返す(ユーザー決定。ADR 0007)
2. **送る前に検査する**: 100 件の上限・排他・必須・`revisions` の長さを、kintone を呼ぶ前に(名前解決の API も呼ぶ前に)`argument_error` にする
3. **書き込みはリトライしない**: M02 のクライアントの保証をツール経由でもテストで固定する。多重作成を防ぐ冪等キャッシュ(`idem:`)は `record_create` だけに付ける
4. **`maxApiCalls` を最悪ケースの式から決めて固定する**: 名前解決の GET は読み取りなので 429・503 でリトライされる。書き込みツールでも「解決の読み取りは × 3、書き込み本体は × 1」で数える(下記。仕様書 §8 の表の「12 / 13 / 12」は読み取りのリトライを含めていないので直す)

M05 の範囲(デプロイ設定・runbook・README・CI の deploy)は作らない。

## 完了条件

- `mise run test`(unit + e2e)と `mise run lint`(Biome + tsc 2 種)と `mise run build` が通る。検証コマンドは `pnpm exec vitest run --project unit --project node` / `--project e2e` / `pnpm exec biome check .` / `pnpm exec tsc --noEmit` / `pnpm exec tsc --noEmit -p scripts/tsconfig.json`
- **`kintone_record_create`(§5.4、WriteNonIdempotent、toolset `record`)**: 引数 `app` / `app_ref`(どちらか 1 つ必須・排他)`record`(object)`records`(object の配列、**1〜100 件**)。`record` と `records` はちょうど一方。`POST /k/v1/records.json` に `{app, records:[...]}`(`record` は 1 件の配列にする)。出力 `{ids:[数値], revisions:[数値]}`(kintone は文字列で返すので数値化。数として読めなければ、書き込みは済んでいるので **retryable でない** `api_error`)。リトライしない。冪等キャッシュあり(下記)
- **`kintone_record_update`(§5.5、WriteIdempotent、toolset `record`)**: 引数 `app` / `app_ref`、`id`(正の整数)または `update_key_field` / `update_key_field_ref`(排他)+ `update_key_value`(文字列)、`revision`(整数、-1 は検証なし)、`record`(必須・空オブジェクトは `argument_error`)。`id` と `update_key_*` はちょうど一方。`update_key_field_ref` は `resolveField(appId, ref, {mode: "write"})`(フィールドコード完全一致 → label 完全一致。部分一致のみなら `resolver_field_exact_match_required`)。`PUT /k/v1/record.json` に `{app, id | updateKey:{field,value}, revision?, record}`。出力 `{revision: 数値}`。リトライしない
- **`kintone_record_delete`(§5.6、Destructive、toolset `record`)**: 引数 `app` / `app_ref`、`ids`(**1〜100 件**、各要素は正の整数、必須)、`revisions`(任意の整数配列。指定時は `ids` と同じ長さ)。`DELETE /k/v1/records.json` に **JSON 本文** `{app, ids, revisions?}`。出力 `{deleted: ids.length}`。リトライしない
- **annotation(4 区分)**: create = WriteNonIdempotent(`readOnlyHint=false` `destructiveHint=false` `idempotentHint=false`)、update = WriteIdempotent(`idempotentHint=true`)、delete = Destructive(`destructiveHint=true`)。`openWorldHint=true`、`title` は日本語。書き込みツールに `max_response_tokens` は付けない
- **description**: 書き込みでは `app_ref` はアプリ ID・コード・名前の**完全一致だけ**で、部分一致の候補が返ったら ID で再実行すること(ADR 0007)。update の `update_key_field_ref` も同様。create は同じ引数の再送が 5 分間は前回の結果を返すこと
- **write モード**: 3 ツールとも `withResolvedApp(args, ctx, {mode: "write"}, ...)`。`app_ref` が ID・コード完全一致・名前完全一致なら採用、部分一致のみなら `resolver_app_exact_match_required`(`details.candidates` = 部分一致の候補、`kind: "app"`、`ref`)で**書き込み API を呼ばない**(unit と E2E の両方)。名前解決の GET(`apps.json`)は呼んでよい
- **入力の送信前検査**(いずれも kintone を呼ぶ前、名前解決の API も呼ぶ前に `argument_error`): `records` 0 件・101 件、`record` と `records` の両方・どちらもなし、`ids` 0 件・101 件・0 以下・小数、`revisions` の長さ不一致、`id` と `update_key_*` の両方・どちらもなし、`update_key_field` と `update_key_field_ref` の両方・どちらもなし(`id` なしのとき)、`update_key_value` なし(`update_key_*` を使うとき)、`record` が空、`app` と `app_ref` の排他違反。未知の引数も `argument_error`
- **書き込みはリトライされない**(ツール経由でテストで固定): POST / PUT / DELETE が 429・503 を返しても kintone への書き込みは 1 回だけで、`rate_limited`(429)/ `api_error`(503)をそのまま返す。名前解決の GET は読み取りなので 429 でリトライされる(書き込みの前段のリトライは許す)
- **冪等キャッシュ(`record_create` だけ。§10 の `idem:`、ADR 0009)**: 下記「冪等キャッシュの設計」。同じ冪等キーの再送は kintone を一度も呼ばず(名前解決も書き込みも)前回の結果をそのまま返す。キーに生の email・トークンを入れない。エラーは保存しない。KV の失敗は止めない
- **`maxApiCalls`(ADR 0008 の最悪ケースの式)**: 解決の GET は読み取りなのでリトライ込みで × 3、書き込み本体は × 1。`A = 11`、`F = 1`:

  | ツール | 論理の式(読み取り + 書き込み) | 試行数の最悪 | `maxApiCalls` |
  |---|---|---|---|
  | `kintone_record_create` | A + 書き込み 1 = 12 | A × 3 + 1 = 33 + 1 | **34** |
  | `kintone_record_update` | A + F + 書き込み 1 = 13 | (A + F) × 3 + 1 = 36 + 1 | **37** |
  | `kintone_record_delete` | A + 書き込み 1 = 12 | A × 3 + 1 = 33 + 1 | **34** |

  すべて 50 以内。unit(`call-budget.test.ts`)で、(a) 宣言値が上の値であること、(b) 全ページ満杯のモックで数えた論理呼び出し数が 12 / 13 / 12 であること、(c) 解決のすべての GET が 429 を 2 回返してから成功し、書き込みは成功する最悪のリトライで、試行数が宣言値ちょうど(34 / 37 / 34)で成功すること、(d) 名前検索が 10 ページ満杯なら 11 回の GET で `resolver_app_list_too_large` になり書き込みを呼ばないこと、(e) キャッシュの取り直しを含めても解決が A = 11 以内であること、を固定する
- **`read_only=true` で隠れる**: `tools/list` から 3 ツールが消え(`?read_only=true` は読み取り 3 つ、`?toolsets=record&read_only=true` は `kintone_records_query` だけ)、`tools/call` で呼んでも実行されず kintone を呼ばない(unit と E2E)
- **`?toolsets=record`**: M03 では `kintone_records_query` の 1 つだったが、M04 で record の 4 ツール(`kintone_records_query` / `record_create` / `record_update` / `record_delete`)になる。`?toolsets=app` は 2 つのまま。tools/list の期待値と E2E を更新する
- **tools/list のスナップショットを意図して更新する**(`mise run test:update-snapshots` → `git diff` で引数・description・annotation を確認してからコミット)
- **M03 の検証の低優先の指摘**:
  1. `records_query` で `limit` が 500 を超える、または `offset` が 10,000 を超えるクエリを kintone が拒否したとき、そのエラーが正しい分類(400 + `CB_VA01` → `argument_error`、`retryable: false`、`details.http_status` / `kintone_code`)で返ることを、E2E で 1 件固定する(コードは変えない。分類は M02 で実装済みの特性テスト)
  2. kintone が存在しないアプリに 404 でなく 520 を返すかもしれず、その場合 `app_ref` のキャッシュが TTL(600 秒)まで残る。`docs/manual-verification.md` の V07 に確認項目を足す(コードは変えない)
- **E2E**(Worker 全体への HTTP。`test/helpers/oauth-flow.ts`): `/register` → `/authorize` → `/callback` → `/token` → `/mcp` で 3 ツールの `tools/call` を通す(kintone は fetch のモック。書き込みの本文・メソッド・`Content-Type` を確かめる)
  - create: `record` / `records`、`ids` の数値化、冪等キャッシュ(2 回目は kintone を呼ばない。KV のキーが `idem:<userId>:<grantId>:` で始まり、email・トークンを含まない。別 grant とは共有しない)
  - update: `id` 指定、`update_key_field_ref`(label 完全一致で `updateKey.field` がコードになる)
  - delete: `ids` / `revisions` の JSON 本文
  - 部分一致のみの `app_ref` を 3 ツールすべてに渡すと、書き込みの API(POST / PUT / DELETE)が呼ばれず `resolver_app_exact_match_required` + `details.candidates`。部分一致のみの `update_key_field_ref` も `resolver_field_exact_match_required` で PUT を呼ばない
  - 書き込みの 429 が 1 回で打ち切られる(リトライされない)
  - `read_only=true` で 3 ツールが見えず、呼べない
- ドキュメント: ロードマップの M04 を `[x]`、ADR 0006 / 0007 / 0008 / 0009 に M04 で確定した点、新しい ADR 0011(冪等キャッシュの設計)、仕様書(状態・§5.4〜5.6・§8 の表・§10 の `idem:`)、`docs/manual-verification.md`(V07 の追記・M04 で増えた前提)、CLAUDE.md の構成・ツールの節。本計画の末尾に「実装後の記録(計画との差分)」を書く
- **実機確認(V07・V08・V24 ほか)は完了条件に含めない**(ユーザー実施)。実 kintone・実 Cloudflare との通信、デプロイ、secret の登録はしない

## 冪等キャッシュの設計(`record_create` だけ。logbutler ADR 0005 を kintone 向けに変える)

MCP クライアントは接続断のあとに同じ呼び出しを再送しうる。Workers はリクエストごとに別 isolate になりうるので、メモリには置けず KV に置く。

- **対象**: `WriteNonIdempotent` の区分のツール(今は `kintone_record_create` だけ)。`record_update`(同じ内容で上書き = 結果が同じ)と `record_delete`(二重に消しても結果は同じ)は対象外(ADR 0009 の決定どおり)
- **置き場とキー**: `OAUTH_KV`。`idem:<userId>:<grantId>:<toolName>:<kind>:<値>`。`<userId>:<grantId>` は MCP トークンから取り出した grant(M03 の `grantRefFromAccessToken`。grant 単位。ADR 0010)。`userId` は Access の email の SHA-256 なので、キーに生の email は入らない。トークン(`<secret>` 部分)も入れない。**grant が分からない・KV がないときは冪等キャッシュを使わない**(通常の書き込みとして実行)
  - **明示キー**(`kind = explicit`): `params._meta.idempotency_key` → なければ引数 `idempotency_key`(`inputSchema` には宣言しない。logbutler と同じ。自前の検証の前に取り出して引数から外す)。空文字・文字列でない値は無いものとして扱う。UTF-8 で 200 バイトを超えたら `argument_error`。**キーに入れる値は明示キーそのものでなく SHA-256(16 進)**(クライアントが好きな文字列を KV のキーに載せられないように、また長さを一定にするため)
  - **引数ハッシュ**(`kind = hash`): 明示キーがないとき、検証済みの引数を canonical JSON(キーをソート)にした SHA-256(16 進)
- **保存する値**: `{args: <引数ハッシュ>, result: <成功した結果の JSON 文字列>}`。エラーは保存しない(一時障害のあとに再送できなくなるため)。TTL **300 秒**(KV の最小 60 秒以上。logbutler と同じ)
- **同じ明示キーで引数が違う場合**: 前回の結果を黙って返すと別の書き込みが捨てられるので、`argument_error`(「idempotency_key は別の引数で使用済み」)を返し、kintone は呼ばない(logbutler からの意図した差分)。引数ハッシュのキーは引数が同じときだけ当たる
- **重複だったとき**: 初回と同じ結果をそのまま返す(印は付けない)。kintone は名前解決を含めて一度も呼ばない
- **KV の失敗**: get が失敗したら実行する。put が失敗しても結果は返す。ログには例外名だけ(キー・値は出さない)
- **限界(受け入れる)**: KV は結果整合で排他制御もないので、**ほぼ同時の 2 回の呼び出しは両方実行されうる**(Durable Objects を使わない方針)。別のロケーションからは保存から最大 60 秒ほど古い値が見え、直後の再送で重複しうる(V25)。引数ハッシュ方式では、**同じ引数で意図的に 2 件作る**ときも 5 分間は 2 回目が前回の結果になる(区別したいときは別の `idempotency_key` を渡す。description に書く)。通信失敗・タイムアウト・5xx のときは結果が分からないので保存されず、再送は重複しうる(`api_error` は `retryable` のまま。AI に委ねる。ADR 0006)
- 実装は `src/mcp/idempotency.ts`。`registerTools` が、引数の検証が済んだあと(kintone クライアントを作る前)にハンドラを包む

## 対象範囲

### 構成(M04 で足す・変えるもの)
```
src/
  resolver/
    budget.ts               writeBudget(読み取りの論理回数, 書き込み回数 = 1) = readBudget(...) + 書き込み回数 を足す
  mcp/
    registry.ts             WriteNonIdempotent のツールを冪等キャッシュで包む(_meta の明示キー・引数 idempotency_key の取り出し)。
                            ToolContext は変えない
    idempotency.ts          新規。明示キー・引数ハッシュ・キー生成・withIdempotency(KV の失敗は止めない)
    tools/
      record-write.ts       新規。書き込み 3 ツールの共通部(レコード形・件数上限・数値化・書き込みの結果の読み取り)
      record-create.ts      新規
      record-update.ts      新規
      record-delete.ts      新規
      index.ts              ALL_TOOLS = [appsSearch, appDescribe, recordsQuery, recordCreate, recordUpdate, recordDelete]
test/
  unit/    record-create / record-update / record-delete(tool-*.test.ts)/ idempotency / write-no-retry / call-budget(拡張)/
           tools-list-snapshot(6 ツール・annotation・read_only)/ mcp-api-handler(公開ツールの更新)/ tool-config(実ツールの toolset)
  e2e/     write-tools.test.ts(新規)、read-tools.test.ts(records_query の limit / offset 拒否を足す)、mcp-tools.test.ts・oauth-flow.test.ts・verify-oauth.test.ts(公開ツールの一覧の更新)
  helpers/ tool-harness.ts(書き込み = post / put / delete の記録を足す)
  snapshots/tools-list.json
```

### 1. 共通部(`src/mcp/tools/record-write.ts`)
- `recordShape` = `z.record(z.string(), z.unknown())`(`{"<フィールドコード>":{"value":...}}`)。中身の形は検査しない(kintone が 400 `CB_VA01` を返す。そのまま `argument_error`)
- `MAX_WRITE_RECORDS = 100`(`records` / `ids` の上限。公式 add-records / delete-records)
- `toSafeInteger(raw, label)`: kintone が文字列で返す `ids` / `revisions` / `revision` を数値にする。数として読めなければ、書き込み済みの可能性があるので `retryable: false` の `api_error`(「書き込みは成功した可能性がある。再送せず内容を確認する」)

### 2. 各ツール
- すべて `withResolvedApp(args, ctx, { mode: "write" }, run)`。`app` / `app_ref` の排他検査は `withResolvedApp` が名前解決より先に行う。ほかの入力検査(件数・排他・必須)は `inputSchema` の `superRefine` に置き、`registerTools` の自前の検証(`safeParse`)で kintone クライアントを作る前に行う(`superRefine` は zod 4 では `ZodObject` のまま)
- `record_update` は `run` の中で、`update_key_field_ref` があれば `ctx.resolver.resolveField(appId, ref, {mode: "write"})` でコードにしてから PUT する

### 3. 冪等キャッシュ(`src/mcp/idempotency.ts`、`registry.ts`)
- 上記。`registerTools` の `tools/call` ハンドラ: (1) 明示キーを `_meta` / 生の引数から取り出す(`idempotency_key` は引数から外す)、(2) `safeParse`、(3) WriteNonIdempotent かつ KV・grant があれば `withIdempotency`(ヒットなら kintone クライアントを作らずに返す)、(4) なければ通常どおり

### 4. 環境変数・バインディング
変更なし(`OAUTH_KV` を冪等キャッシュにも使う)。

## TDD の進め方(小さい順。各段階で失敗を確認してから実装)

1. **`writeBudget` と `maxApiCalls`(Red から)**: `call-budget.test.ts` に 3 ツールの宣言値・論理回数・最悪のリトライ・10 ページ満杯・取り直しを足す。**期待する失敗**: 書き込みツールが `ALL_TOOLS` にない(宣言値が `undefined`、`tools/call` が未知のツール)
2. **ツールの unit(Red から)**: `tool-record-create.test.ts` / `tool-record-update.test.ts` / `tool-record-delete.test.ts`。リクエストの形・出力の数値化・排他・100 件上限(kintone を呼ばない)・write モード(部分一致のみで書き込みを呼ばない)・kintone のエラーの分類。ハーネス(`tool-harness.ts`)に書き込みの記録を足す。**期待する失敗**: ツールがない
3. **リトライしない**: `write-no-retry.test.ts`。実クライアント + `mockOutboundFetch` で、3 ツールの書き込みが 429・503 で 1 回だけ。解決の GET は 429 でリトライされる。**期待する失敗**: ツールがない
4. **冪等キャッシュ**: `idempotency.test.ts`。キー形式(`idem:<userId>:<grantId>:...`)、生の email・トークン・明示キーを含まない、TTL 300、エラーを保存しない、明示キー(`_meta` と引数)・引数ハッシュ、引数が違う明示キーの再利用は `argument_error`、KV の get / put の失敗で続行、grant・KV なしは何もしない、`record_update` / `record_delete` は対象外、別 grant とは共有しない。**期待する失敗**: ツールがない / モジュールがない
5. **スナップショットと公開ツール**: `tools-list-snapshot.test.ts` に 6 ツール・annotation の 4 区分・`read_only` で書き込みが消えること・`toolset record` が 4 ツールであることを足す。`mcp-api-handler.test.ts` の公開ツールの一覧を更新。**期待する失敗**: 3 ツールのまま
6. **E2E(Red から)**: `write-tools.test.ts`、`read-tools.test.ts` の limit / offset 拒否、`mcp-tools.test.ts` の公開ツール一覧
7. 実装(`budget.ts` → `idempotency.ts` → `registry.ts` → `record-write.ts` → 3 ツール → `index.ts`)→ `mise run test:update-snapshots` でスナップショットを更新して差分をレビュー → 全体(`mise run test` / `lint` / `build`)→ ドキュメント更新

## 作るテストの一覧

| 種別 | ファイル | 内容 |
|---|---|---|
| unit | `tool-record-create.test.ts` | 引数の検証(排他・1〜100 件・未知の引数)、`{app, records}` の本文、`record` の 1 件配列化、`ids` / `revisions` の数値化と不正な応答、`app_ref` の write モード(コード・名前の完全一致で採用、部分一致のみ 1 件・複数件で書き込みを呼ばない、完全一致 2 件は ambiguous)、kintone のエラーの分類、`max_response_tokens` を持たない |
| unit | `tool-record-update.test.ts` | `id` / `update_key_*` の排他と必須、`record` 必須・空は `argument_error`、本文(`id` / `updateKey` / `revision`)、`revision: -1`、`update_key_field_ref` の write モード(コード・label 完全一致で採用、部分一致のみで PUT を呼ばない、ambiguous、not_found)、出力 `{revision}` の数値化、フィールドの解決は `fields.json` を 1 回だけ |
| unit | `tool-record-delete.test.ts` | `ids` 1〜100・正の整数、`revisions` の長さ、DELETE の JSON 本文(`revisions` なしならキーを出さない)、出力 `{deleted}`、write モード、100 件超で kintone を呼ばない |
| unit | `idempotency.test.ts` | 冪等キャッシュの全項目(上記)。ツール単位(`record_create`)と関数単位 |
| unit | `write-no-retry.test.ts` | 3 ツールの書き込みが 429・503・5xx・通信失敗で 1 回だけ。解決の GET の 429 はリトライ。書き込みの本文は 1 回分 |
| unit | `call-budget.test.ts` | 宣言値 34 / 37 / 34、論理回数 12 / 13 / 12、最悪のリトライ(試行数 = 宣言値)、10 ページ満杯で書き込みを呼ばない、取り直しでも解決は 11 回以内。式の部品 `writeBudget` |
| unit | `tools-list-snapshot.test.ts` / `mcp-api-handler.test.ts` / `tool-config.test.ts` | 6 ツール、annotation の 4 区分(delete は `destructiveHint=true`)、`read_only` で書き込みが消える、toolset `record` の 4 ツール、公開ツールの一覧の更新 |
| e2e | `write-tools.test.ts` | 3 ツールの `tools/call`、冪等キャッシュ、完全一致のみ(部分一致で書き込みを呼ばない)、書き込みがリトライされない、`read_only=true` で隠れて呼べない、`?toolsets=record` |
| e2e | `read-tools.test.ts` | `records_query` の limit > 500 / offset > 10,000 の kintone の拒否が `argument_error` で返る |
| e2e | `mcp-tools.test.ts` / `oauth-flow.test.ts` / `verify-oauth.test.ts` | 公開ツールの一覧を 6 ツールに更新 |

## やってはいけないこと
- 書き込み(POST / PUT / DELETE)のリトライ、並列リクエスト、名前検索のページ上限を 10 より増やすこと(増やすなら `maxApiCalls` の式から再計算する)
- 書き込みツールで部分一致の `app_ref` / `update_key_field_ref` を採用すること。write モードの分岐をツール側で緩めること
- 冪等キャッシュのキーを email 単位にすること、生の email・トークン・明示キーそのものをキーに入れること、エラーを保存すること、`record_update` / `record_delete` に冪等キャッシュを付けること
- 書き込みツールに `max_response_tokens` を付けること(成功した書き込みを失敗に見せない)
- 書き込みが成功したあとの応答の解釈の失敗を、再送を促す(retryable な)エラーにすること
- トークン・クライアントシークレットをログ・エラー・応答・スナップショット・KV のキーに出す
- テストを弱める・skip する・消す。スナップショットを `--update` で書き換えて差分をレビューせずにコミットする
- 実 kintone・実 Cloudflare への通信、デプロイ、secret の登録、依存の追加、Durable Objects / McpAgent、`structuredContent` の使用
- M05 の範囲(デプロイ設定・runbook・README・CI の deploy)

## 未検証の前提(実機でしか分からないもの。ブロッカーにせず、`docs/manual-verification.md` に追記して進む)
- 書き込み API の実際の応答形(`ids` / `revisions` / `revision` が文字列か)、DELETE が JSON 本文を受け付けるか、`updateKey` に使えるフィールドの条件、`revision` の不一致のステータス(V24)
- 書き込みの 429 が未処理か(V08。現状は保守的にリトライしない)
- KV の結果整合が冪等キャッシュの重複防止に与える影響(V25)
- 存在しないアプリに 404 でなく 520 が返る場合のキャッシュの残り方(V07 に追記)
- 書き込みの `form/fields.json`(`update_key_field_ref`)が一般の利用者で `permission_error` にならないか(V09)
- KV の読み書きが subrequest の 50 に数えられるか(V12)。書き込みツールの最悪は kintone 37 + KV(冪等の get / put、名前解決のキャッシュの get / put)

## 成果物
- 上記のコードとテスト、`test/snapshots/tools-list.json`
- ADR 0006 / 0007 / 0008 / 0009 に M04 で確定した点を追記、新しい ADR 0011(冪等キャッシュ)、仕様書(状態・§5.4〜5.6・§8・§10)
- `docs/manual-verification.md` の更新(V07 の追記、V24・V25)
- ロードマップの M04 を `[x]` にする。CLAUDE.md の構成・ツールの節を更新
- 本ファイルの末尾に「実装後の記録(計画との差分)」

## 実装後の記録(計画との差分)

計画から外れた点と、実装中に決めた細部。ADR 0006 / 0007 / 0008 / 0009 / 0011 と仕様書の §5・§8・§10、CLAUDE.md にも反映した。

- **書き込みツールの `maxApiCalls` は、仕様書 §8 の表(M02 時点の 12 / 13 / 12)でなく 34 / 37 / 34**。表は「書き込みはリトライしない」ことだけを見て、名前解決・フィールド解決の GET が読み取りとしてリトライされる(最大 2 回)ことを落としていた。12 のままだと、`apps.json` が 1 回 429 を返しただけで上限に達し、書き込みの前に `api_error` で止まる。式は「解決の読み取り × 3 + 書き込み × 1」(`writeBudget`)。`call-budget.test.ts` が宣言値・論理回数(12 / 13 / 12)・最悪のリトライ(試行数 = 宣言値ちょうど)・10 ページ満杯・取り直しを固定する。計画の段階でこの式にしていたので、計画との差分ではなく**仕様書・ADR 0008 の表の訂正**(両方を直した)
- **冪等キャッシュは logbutler ADR 0005 から 3 点変えた**(ADR 0011): (1) キーは grant 単位(`idem:<userId>:<grantId>:...`)、(2) 明示キーは生で KV のキーに入れず SHA-256 にする(200 バイトの上限は入力の検査として残した)、(3) 同じ明示キーで別の引数は前回の結果を返さず `argument_error`(保存値に引数ハッシュを持つ)。計画の段階から決めていたが、実装で `{args, result}` の保存形式にした
- **既存テストの変更(アサーションは同等以上を残した)**:
  - `tools-list-snapshot.test.ts`: 「3 つの読み取りツールが公開される」を 6 ツールの一覧に更新、toolset `record` の期待値を 4 ツールに更新、`max_response_tokens` の `minimum: 0` の検査は書き込みツール(この引数を持たない)を除く読み取りの 3 ツールが対象(読み取りの 3 ツールの名前も検査に加えた)
  - `mcp-api-handler.test.ts`: 有効なクエリの `tools/list`(`""` `?read_only=false` `?toolsets=all` `?x=1` は 6 ツール、`?read_only=true` `?toolsets=all&read_only=true` は読み取りの 3 つ)、`?toolsets=record` は 4 ツールに更新
  - `e2e/read-tools.test.ts`: `tools/list` の期待値(6 / `?toolsets=record` 4 / `?read_only=true` は読み取りの 3 つ)、`minimum: 0` の検査は読み取りのツールだけ
  - `e2e/mcp-tools.test.ts`: クエリごとの `tools/list` の期待値(`?toolsets=record&read_only=true` を足した)
  - `e2e/oauth-flow.test.ts`: `tools/list` のツール名を 6 つに、`e2e/verify-oauth.test.ts`: `toolCount` を 3 から 6 に
  - `test/helpers/tool-harness.ts`: `get` を任意にし、書き込みの偽物(`write` オプション。記録は `writes`)・`call` の第 3 引数 `_meta`・`writeBody()` を足した(読み取りの既存の使い方は変えない。`write` を渡さなければ書き込みは呼ばれたら例外のまま)。`test/helpers/oauth-flow.ts`: `RpcResponse` の tools に `description?` を足した(型だけ)
- **`records_query` の limit / offset 拒否のテスト(M03 の指摘)は、実装済みの分類の特性テストなので、足した時点で緑**(Red はない。コードは変えていない)。400 + `CB_VA01` → `argument_error`(retryable でない、`details.http_status` / `kintone_code`)・リトライなし・クエリがそのまま kintone へ渡ることを E2E で固定
- **`read_only=true` の `tools/call` が書き込みツールを呼べないことの unit(`mcp-api-handler.test.ts`)は、実装前もツールがないために緑**(Red の根拠は `tools/list` の期待値と E2E の `tools/list`)。E2E と合わせて、実装後は「ツールは存在するが隠れて呼べない」ことを確かめている
- **`superRefine` で入力の排他・必須・上限を検査**(`record_create` / `record_update` / `record_delete`)。zod 4 では `superRefine` が `ZodObject` のまま返るので `defineTool` の型を変えていない。公開スキーマ(`tools/list`)は refinement を出さないため、排他は引数の `description` にも書いた
- `ToolContext`・`RegisterDeps`・`defineTool` は変えていない。`registerTools` の `tools/call` ハンドラが SDK の第 2 引数(`ServerContext`)から `_meta` を読む
- **依存の追加なし**。`wrangler deploy --dry-run` でバンドルできることを確認した

### Red / Green

| 段階 | Red(失敗の理由) | Green |
|---|---|---|
| 計画 | - | `plans/kintone-mcp-m04-write-tools.md` |
| テスト一式 | 新規 `tool-record-create` / `update` / `delete`・`write-no-retry`・`call-budget`(書き込み)・`e2e/write-tools` は書き込みツールが `ALL_TOOLS` にない(`tools/call` の応答が空で `JSON.parse` に失敗、`maxApiCalls` が `undefined`)。`idempotency.test.ts` は `src/mcp/idempotency.ts` と `writeBudget` が存在せず import で失敗。更新分は期待どおりの理由で失敗(`tools-list-snapshot` が 3 ツールのまま、`mcp-api-handler` の `?toolsets=record` が 1 ツール、E2E の公開ツール 3 つ・`toolCount` 3)。unit 122 件・e2e 27 件が赤 | 実装後に unit 787 / e2e 158 が緑 |
| 実装 | - | `src/resolver/budget.ts`(`writeBudget`)、`src/mcp/idempotency.ts`、`src/mcp/registry.ts`(冪等キャッシュの組み込み)、`src/mcp/tools/{record-write,record-create,record-update,record-delete,index,app-target}.ts` |
| スナップショット | `tools-list.json` が古い(3 ツール) | `--update` のあと差分を確認(書き込み 3 ツールの追加だけ。既存の 3 ツールの行は変わらない) |
| 既存テストの更新 | `max_response_tokens` の `minimum` 検査が全ツールを見て、書き込みツールで失敗(unit 1 件・e2e 1 件) | 読み取りのツールだけに絞って緑 |

### テストの件数
- unit(`--project unit --project node`): 640 → **787**(+147。新規 5 ファイル 125 件(`tool-record-create` 27・`tool-record-update` 31・`tool-record-delete` 21・`idempotency` 29・`write-no-retry` 17。`it.each` の展開後)+ 既存ファイルへの追加 22 件(`call-budget`・`tools-list-snapshot`・`mcp-api-handler`))
- e2e(`--project e2e`): 133 → **158**(+25。`write-tools` 22 + `read-tools` に limit / offset 拒否 2 + tools/list の更新分)

### 未検証の前提(追加)
- V24(書き込み API の応答形と制約: `ids` / `revisions` の型、DELETE の JSON 本文と応答本文、`updateKey` の条件、`revision` 不一致)と V25(冪等キャッシュと KV の結果整合)を `docs/manual-verification.md` に追加した。V07 に「存在しないアプリの実ステータス(520 の可能性とキャッシュの残り)」と「limit / offset の拒否の実ステータス」を追記した(コードは変えていない)。V08(書き込みの 429 が未処理か)・V09(`update_key_field_ref` の `form/fields.json` 権限)・V12(KV と subrequest。書き込みの最悪は kintone 37 + KV)は M04 のツールが実際に使うので、実機確認の優先度が上がった
- 書き込みでもキャッシュ(TTL 600 秒)で `app_ref` を解決する。アプリ名が付け替わった直後は別のアプリに当たりうる(特に `record_delete`)。ADR 0007 の「M04 の実装で確定した点」に既知のリスクとして書いた
