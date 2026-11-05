# ADR 0008: ページング・呼び出し回数の上限・応答サイズ・書き込みのリトライ

- 状態: 採用(M02 でクライアントの制限・`max_response_tokens` を実装。M03 で名前解決のページ上限・読み取りツールの最悪ケースのテスト・入力の上限の検査(読み取り分)を実装。書き込み分は M04)
- 日付: 2026-11-05
- 関連: logbutler ADR 0003 / 0006 / 0007、ADR 0007

## 状況

- Go のクライアントは、読み取りを最大 3 回試行(429・503、バックオフ 0.5〜5 秒)、書き込みは 1 回だけ(`internal/kintoneapi/transport.go:32-36,198-228`)、タイムアウト 30 秒(`client.go:39`)、応答本文 32 MiB まで読む。呼び出し回数の上限はない。名前解決は最大 100 ページ × 100 件(`internal/resolver/resolver.go:12` の `maxAppPagesForResolver = 100`)で、最悪 1 + 100 = 101 回の `apps.json`。
- `records_query` はクエリ文字列を `/k/v1/records.json` に渡す 1 リクエストで、自動ページングも応答サイズの制限もない。kintone 側は 1 回最大 500 件(既定 100)、`offset` 最大 10,000(公式 get-records ページ)。
- Workers 無料プランの subrequest 上限は 50(logbutler ADR 0003 の根拠。KV の読み書きが数えられるかは未検証=V12)。
- kintone の同時接続は 1 ドメイン 100 まで、超えると 429(公式 kintone.dev の API 制限ページ)。`Retry-After` の記載はない(V08)。1 日の API リクエスト数の上限は公式ページで確認できていない(V16)。

## 決定

### クライアント(logbutler ADR 0003 を踏襲)
- 1 回のツール呼び出しにつきクライアント 1 つ。リクエストは直列(同時 1)。
- 読み取りの 429・503 は最大 2 回リトライ。`Retry-After`(秒)があれば従う(10 秒を超えるなら待たずに `rate_limited`)、なければ 1 秒待つ。
- **書き込み(POST/PUT/DELETE)はリトライしない**(Go と同じ。429・503 でも 1 回で打ち切り)。多重作成・多重更新を避けるため。429 は未処理のはずだが、確証がない(V08)間は保守的に。
- タイムアウト 30 秒 / リクエスト。通信失敗・タイムアウトは `api_error`(retryable)。
- 呼び出し回数の上限 50 回 / ツール呼び出し(リトライ含む)。超えたら `api_error`(retryable=false)で止まる。
- 応答本文の読み取りは 10 MiB まで(Go は 32 MiB)。超えたら読むのをやめて `response_too_large`(絞り込みを促す message)。

### ページング
- **名前解決のアプリ検索は 10 ページ × 100 件(= 1,000 件)が上限**。10 ページ全部が満杯なら `resolver_app_list_too_large`。
- `kintone_apps_search` は 1 ページ(`limit` 既定 100、`offset` はクライアント指定)。`kintone_records_query` は 1 リクエスト。自動ページングはしない(AI が `limit`/`offset` で呼び直す)。`truncated` の概念は持たない(複数ページを集めるツールがないため)。複数ページを集める分析系を足すとき、logbutler の `truncated` を常に出す規則を採る。

### 最悪ケースの式(`maxApiCalls`)
`A` = アプリ解決 = コード 1 + 名前 10 = **11**、`F` = フィールド解決 = **1**。論理呼び出し数:

| ツール | 式 | 論理最悪 | 読み取りのリトライ込み(最大 2 回/呼び出し → × 3) |
|---|---|---|---|
| `kintone_apps_search` | 1 | 1 | 3 |
| `kintone_app_describe` | A + 2 | 13 | 39 |
| `kintone_records_query` | A + 1 | 12 | 36 |
| `kintone_record_create` | A + 1 | 12 | 12(書き込みはリトライなし) |
| `kintone_record_update` | A + F + 1 | 13 | 13 |
| `kintone_record_delete` | A + 1 | 12 | 12 |

すべて 50(既定)以内。~~`maxApiCalls` は既定の 50 のまま~~ → **M03 で変更: 読み取りツールは `maxApiCalls` を式から決めて明示する**(下の「M03 の実装で確定した点」)。テスト(unit)で「全ページ満杯を返すモックでの論理呼び出し数 = 上の式」と「リトライ込みの試行数 = 宣言値」を固定する。名前解決のページ上限を 10 より増やすと `13 × 3 = 39` の余裕がなくなる(増やすなら式から再計算する)。
キャッシュを使うと減る一方、「キャッシュ由来の解決が `not_found` → 取り直し」(ADR 0007)でも合計は A 以内に収まる(キャッシュヒットで 0 回、取り直しで A)。

### 入力の上限(kintone へ送る前に `argument_error`)
公式の上限を送信前に検査する(無駄な呼び出しを避け、メッセージを明確にする):
- `records_query` の `fields`: 100 件まで(クエリ文字列では添字 0〜99。公式 get-records)
- `record_create` の `records`: 1〜100 件(公式 add-records)
- `record_delete` の `ids`: 1〜100 件(公式 delete-records)
- `apps_search` の `ids` `codes` `space_ids`: 各 100 件まで、`name` 64 文字以内、`limit` 1〜100(公式 get-apps)

### 応答サイズ(logbutler ADR 0007)
- 読み取りツールに `max_response_tokens`(既定 50000、0 で無効、負は `argument_error`)。推定は ASCII 4 文字 = 1 トークン(切り上げ)、ASCII 以外 1 文字 = 1 トークン。超過は切り詰めず `response_too_large`。
- 書き込みツールは対象外(成功した書き込みを失敗に見せない)。
- `records_query` で 500 件 × 多フィールドは容易に 50000 を超える。message に `fields` の指定、`limit` を下げること、`max_response_tokens` を上げることを案内する。

## 理由
- 最悪ケースを式で決めて機械的に固定すれば、あとから上限を触る人が副作用に気づける(logbutler ADR 0006)。
- 名前解決を 10 ページにするのは、無料プランの subrequest 50 に収めるための差分。1,000 件を超える「名前が部分一致するアプリ」は現実には稀で、超えたらコードか ID を使うよう案内できる。

## 却下した案
- Go の 100 ページを維持: 論理 101 回で無料プランの上限を超える。有料プランなら動くが、無料で動かない仕様は不親切。
- ~~`maxApiCalls` をツールごとに個別の値にする: 最悪でも 39 回で 50 に収まるので不要。~~ → M03 で採用に変更(下記)。
- `records_query` の自動ページング: 応答サイズの安全網との相性が悪く、Go 版にもなかった。拡張は後のマイルストーン(カーソル API 含む)。

## M02 の実装で確定した点

- 実装は `src/kintone/client.ts`。**リトライするのは読み取り(GET)の 429・503 だけ**で、通信失敗・タイムアウト・5xx(503 以外)・本文が壊れた 200 は読み取りでもリトライしない。`Retry-After` は整数秒だけを読み(日付形式・負・小数は「なし」扱い → 1 秒)、**10 秒ちょうどは待ち、11 秒以上は待たずに打ち切る**。待機は注入できる(`sleep`)ので、テストは実際には待たない。
- 呼び出し回数は**リトライも 1 回として数え**、次の試行ができない(上限に達した)ときは待たずに `api_error`(retryable でない)にする。書き込みも同じカウンター。
- 応答本文は 10 MiB(10 × 1024 × 1024)まで。ちょうど 10 MiB は読め、1 バイト超えたら読むのをやめて(`cancel`)`response_too_large`。`Content-Length` が超過を宣言していれば本文を読まない。エラー応答の本文は 64 KiB までしか読まない(超えたら本文なし扱いで、ステータスだけで分類)。
- タイムアウトは `fetch` から本文を読み終えるまでの 1 本のタイマーで、`fetch` が中断に応じなくても(モックでも)`AbortSignal` との競合で打ち切る。止まった本文ストリームでも固まらない。
- リダイレクトは辿らない(`redirect: "manual"`。3xx は `api_error`)。トークンを別の宛先へ送らないため。
- `max_response_tokens` は `defineTool` が ReadOnly のツールにだけ自動で足す(`responseLimit: false` で外せる)。負・非整数は kintone を呼ぶ前に `argument_error`。JSON Schema では整数の安全範囲(±9007199254740991)の `minimum` / `maximum` が付く(zod の `int()` の出力。負の検査は自前)。
- `maxApiCalls` は `defineTool` の任意の宣言で、`getClient({ maxApiCalls })` に渡る。既定の 50 は変えていない。
- `/mcp` の URL クエリ(`toolsets` / `read_only`)の検査は `parseToolConfig`。不正値・綴り違いのキー・0 ツールは HTTP 400 `invalid_query`。**M02 の実ツールは `kintone_apps_search`(toolset `app`)だけなので、`?toolsets=record` は 400 になる**(公開ツールが 0 個。M03 で `kintone_records_query` が入ると通る)。

## 未検証の前提
- 429 の `Retry-After` の有無、書き込みの 429 が「未処理」か(V08)
- KV の読み書きが subrequest に数えられるか、有料プランの上限(V12)
- 1 日あたりの API リクエスト数の上限(V16)
- GET の URL 長の上限(`fields` 100 個 + 長い `query`)(V11)

## M03 の実装で確定した点

- **`maxApiCalls` は読み取りツールごとに「論理最悪 × 3」を明示する**(`src/resolver/budget.ts` の `readBudget(論理回数)`。M02 時点の「既定の 50 のまま」から変更。M03 のタスク指示「式から決めた値を明示し、テストで固定する」による)。`kintone_apps_search` = 1 × 3 = **3**、`kintone_app_describe` = 13 × 3 = **39**、`kintone_records_query` = 12 × 3 = **36**。理由: 読み取りは 1 呼び出しあたり最大 3 試行なので、論理最悪 × 3 が本当の最悪の試行数で、これより緩い上限(50)は、実装の誤りで呼び出しが増えたときに気づけない。式の部品は `APP_RESOLVE_CALLS`(= 1 + `MAX_APP_NAME_PAGES` = 11)と `FIELD_RESOLVE_CALLS`(= 1)。ページ上限を増やすと各値が変わり、`test/unit/call-budget.test.ts` が赤になる。書き込みツール(M04)はリトライしないので × 1(式から決める)。
- `test/unit/call-budget.test.ts` が、実クライアント + `fetch` のモックで次を固定する: (a) 宣言値 3 / 39 / 36、(b) 全ページ満杯(9 ページ満杯 + 10 ページ目が 50 件)で数えた論理呼び出し数 1 / 13 / 12、(c) すべての呼び出しが 429 を 2 回返してから成功する最悪のリトライで試行数 = 3 / 39 / 36(宣言値ちょうどで成功する)、(d) 10 ページ満杯なら 11 回で `resolver_app_list_too_large`、(e) キャッシュの取り直しを含めても解決の API は 11 回以内。
- KV の呼び出しは `maxApiCalls`(kintone への HTTP の回数)に含めない。KV が subrequest 上限の 50 に数えられるか(V12)が未確認のため、数えられる場合は `app_describe` の最悪(kintone 39 + KV の読み書き)が 50 に近づく。確認結果によって式を直す。
- 入力の上限のうち読み取り分は M03 で実装した: `records_query` の `fields` は 100 件まで、`apps_search` の `ids` / `codes` / `space_ids` は各 100 件まで・`codes` の各要素 1〜64 文字・`name` 64 文字まで・`limit` 1〜100・`offset` 0 以上。いずれも kintone を呼ぶ前に `argument_error`。
- `max_response_tokens` の公開スキーマは `minimum: 0` を出す(zod の `.int()` が出す `-9007199254740991` を `.meta({ minimum: 0 })` で上書き。検証は従来どおり `resolveResponseLimit` が負を `argument_error` にする)。
