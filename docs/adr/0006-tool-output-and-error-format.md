# ADR 0006: ツール名・引数・出力・エラー形式(logbutler に合わせる)

- 状態: 採用(M02 でエンベロープ・エラー表・`details`・ツール登録の仕組みを実装。リゾルバーのエラーを投げる側は M03 で実装)。コードの小文字 snake_case への統一はユーザー決定により確定(2026-11-05)
- 日付: 2026-11-05
- 関連: logbutler ADR 0004、ADR 0007(名前解決)、ADR 0008(制限値)

## 状況

Go 版は MCP の出力を CLI と共通の envelope にしていた: 成功 `{"ok":true,"data":{...}}`、失敗 `{"ok":false,"error":{"code","message","details"}}` を `CallToolResult.Content[0].Text` に入れ、**`isError` は付けない**(Go `internal/mcp/facade/result.go:14-31`)。コードは `INVALID_PARAMS` / `KINTONE_*` / `RESOLVER_*` / `AUTH_REQUIRED` など大文字(`errors.go:244-261`)。
logbutler は JSON を text content、失敗は `isError: true` + `{"schema_version":"1","error":{code,message,retryable}}`(小文字 snake_case のコード)。

## 決定

### ツール名・引数
- ツール名の接頭辞は `kintone_`: `kintone_apps_search` / `kintone_app_describe` / `kintone_records_query` / `kintone_record_create` / `kintone_record_update` / `kintone_record_delete`。Go 版は接頭辞なし(`apps_search` ほか)だった。クライアントの設定でツール名を指定している場合は変更が必要。
- 引数名は Go 版のスキーマを踏襲(`app` `app_ref` `ids` `codes` `name` `space_ids` `limit` `offset` `lang` `query` `fields` `total_count` `record` `records` `id` `update_key_field` `update_key_field_ref` `update_key_value` `revision` `revisions`)。検証は zod(`registerTools` が自前で行い、失敗は `argument_error`)。
- 追加する引数: 読み取りツールに `max_response_tokens`(ADR 0008)。
- annotation: logbutler の 4 区分。`apps_search` `app_describe` `records_query` = ReadOnly、`record_create` = WriteNonIdempotent、`record_update` = WriteIdempotent、`record_delete` = Destructive。Go 版の annotation は read-only/destructive の 2 値だけだった(`facade/*.go`)。`title` は日本語。
- toolset: `app`(apps_search, app_describe)、`record`(records_query, record_create, record_update, record_delete)。

### 出力
- JSON 文字列を text content 1 件(`structuredContent` なし)。成功時は Go 版の `data` の中身をそのまま返す(`ok`/`data` の包みなし)。
- キー: kintone の生データはそのまま(`apps` の各要素、`fields` の各定義、`records`)、加工した出力は snake_case(`app_describe` の `app`、`records_query` の `total_count`、`record_create` の `ids`/`revisions`、`record_delete` の `deleted`)。これは Go 版の出力形状と同じ。

### エラー
- すべて `isError: true` の text content 1 件に `{"schema_version":"1","error":{"code","message","retryable","details"?}}`。
- `code` は logbutler の列挙(`argument_error` `authentication_error` `permission_error` `not_found` `api_error` `rate_limited` `response_too_large` `generic_error`)に、**kintone 固有を小文字 snake_case で追加**: `resolver_app_not_found` `resolver_app_ambiguous` `resolver_app_exact_match_required`(書き込み系で `app_ref` が部分一致のみ。ADR 0007) `resolver_field_exact_match_required`(`update_key_field_ref` が部分一致のみ。ADR 0007) `resolver_field_not_found` `resolver_field_ambiguous` `resolver_app_list_too_large`。Go 版の大文字のコードは**中身(意味・分類・`details`)を引き継ぎ、表記は logbutler に揃える**。対応表は仕様書 §6。
- `details` は logbutler 形式にない**任意の拡張**: `http_status` `kintone_code` `kintone_id` `retry_after_sec`、リゾルバーの `kind` `ref` `candidates`(Go 版の `details.candidates` と同形: アプリ `[{"id","code","name"}]`、フィールド `[{"code","label"}]`)。`candidates` が AI の再試行(`app` に ID を渡す、`app_ref` を絞る)に必要なので、Go 版から落とさない。
- kintone の 401 は logbutler と同様、エンベロープだけでなく MCP の HTTP 応答も 401 + `WWW-Authenticate: Bearer error="invalid_token"` にしてクライアントに更新させる(ADR 0005)。
- 想定外の例外は固定文 `Unexpected error.`(内部情報を漏らさない)。

## 理由

- AI が `retryable` で再試行の可否を判断でき、`isError` でクライアントが失敗を機械的に検知できる(logbutler の決定)。
- 姉妹プロジェクトと同じ形にすると、クライアント側の扱い・テストヘルパー・ドキュメントを共有できる。
- コードの表記を揃えると、`error.code` を見て分岐するクライアントが 2 つの規則を持たずに済む。

## 却下した案

- Go 版の `{"ok":...}` を維持する: logbutler と非互換になり、`isError` が付かない。
- `RESOLVER_*` を大文字のまま追加: 同じ列挙に 2 つの表記が混在する。
- `details` を作らず `message` に候補を文字列で埋める: AI が候補 ID を機械的に拾えない。

## 帰結(互換性の破壊)

- **Go 版の `{"ok":...}` を使っていたクライアントは壊れる**(成功の包みがなくなり、失敗は `isError: true`・`schema_version: "1"`・小文字のコードになる)。ツール名の接頭辞も変わる。README の移行案内(M05)に明記する。
- `AUTH_REQUIRED` の `details.authorize_url` は廃止(再認可は HTTP 401 で MCP クライアントが行う)。
- Go 版の `STORE_*` / `SIGNING_KEY_REQUIRED` / `RESOLVER_PRINCIPAL_NOT_FOUND` は対象機能ごと廃止。

## 未検証の前提

- kintone の実際のエラー(ステータスとコードの対応: 400/401/403/404/520、`CB_VA01`、`GAIA_*` など)(V07)

## M02 の実装で確定した点

- エラーの型は `src/errors.ts`(`ERROR_CODES` が仕様書 §6 の全コード。`KintoneMcpError` が `details` を運び、`ResolverError` は M03 のリゾルバーが投げる型)と `src/kintone/errors.ts`(HTTP 応答の分類)。`test/unit/error-table.test.ts` が表の全行をテストし、`ERROR_CODES` の全コードがどれかの行に現れることを検査する。
- 分類の優先順位: 401・403・404・429 は**ステータスが先**(kintone コードが `CB_VA...` でも `argument_error` にしない)。そのあと 400・422・`CB_VA` 接頭辞(HTTP ステータスは問わない。520 + `CB_VA01` も `argument_error`)→ その他の 4xx・3xx(`api_error`、retryable でない)→ 5xx(`api_error`、retryable)。
- **200 なのに JSON でない・空の本文**(M01 は 502 を偽装していた)は、通信失敗と同じ `api_error`(retryable)にし、`details.http_status` は実際の 200、message は「not valid JSON」と区別する。リトライはしない(読み取りでもクライアント内では 1 回。AI が `retryable` を見て呼び直す)。書き込みの本文が壊れた場合は実際には処理されている可能性があるので、`retryable` の扱いは AI に委ねる(多重作成の防止は M04 の冪等キャッシュ)。実際に起きるか・原因(IP 制限やプロキシの HTML など)は未検証(V07)。
- エラー応答の `{code, id, message}` は `details.kintone_code` / `kintone_id` と message に載せる(長さは 500 文字まで、アクセストークンと Bearer は伏せる)。429(と 503)の `Retry-After` が整数秒なら `details.retry_after_sec`。
- 想定外の例外のログは `tool <名前> failed unexpectedly: <例外名>: <トークンを伏せた message>`(応答には固定文だけ)。
- ツールの入力検証は `registerTools` が自前の zod `safeParse` で行い、SDK には公開用のスキーマだけを渡す(ADR の決定どおり)。

## M03 の実装で確定した点

- 読み取りツール 3 つを実装(`kintone_apps_search` / `kintone_app_describe` / `kintone_records_query`)。出力の形は仕様書 §5.1〜5.3 のとおり。`app_describe` の `app` は `app_id` `code` `name` `description` を常に出し、そのほかの項目(`space_id` `thread_id` `created_at` `creator` `modified_at` `modifier`)と `revision` は空・null なら省略する(Go の `omitempty` と同じ。`description` は Go でも `omitempty` なし)。`records_query` の `total_count` は kintone の文字列を数値に変換し、数として読めなければ `api_error`(retryable)にする(黙って落とさない)。
- `app` / `app_ref` の排他違反(両方・どちらもなし)・`app_ref` の空文字・範囲外の引数は、kintone を呼ぶ前に `argument_error`。
- リゾルバーのエラー(`resolver_*`)は `ResolverError` で投げ、`details` に `kind` / `ref` / `candidates`(アプリ `{id, code?, name?}`、フィールド `{code, label?}`)を載せる。`resolver_app_not_found` / `resolver_field_not_found` の `candidates` は空配列。`resolver_app_list_too_large` は `kind` / `ref` だけ。

## M04 の実装で確定した点

- 書き込みツール 3 つの出力: `record_create` = `{ids:[数値], revisions:[数値]}`、`record_update` = `{revision:数値}`、`record_delete` = `{deleted:ids.length}`(kintone の文字列を数値化)。書き込みツールに `max_response_tokens` は付けない。
- annotation: create = WriteNonIdempotent、update = WriteIdempotent、delete = Destructive(`destructiveHint: true`、`idempotentHint: true`)。`title` は日本語。
- **書き込みが成功したあとの応答の解釈の失敗は、retryable でない `api_error`**(`ids` / `revisions` / `revision` が数として読めない・欠けている)。message で「書き込みは済んでいる可能性があるので、盲目的に再送せず内容を確認する」ことを案内する。再送を促すと二重に作成・更新してしまうため(M02 の「200 なのに本文が JSON でない」は retryable のまま。こちらは AI に判断を委ねる)。
- `argument_error` の追加の用途: 書き込みツールの排他・必須・上限・`revisions` の長さ、冪等キーの再利用(同じ `idempotency_key` で別の引数)、`idempotency_key` が 200 バイト超(ADR 0011)。新しいエラーコードは足していない(`ERROR_CODES` は変わらず、`error-table.test.ts` もそのまま)。
- M03 の検証の指摘: `records_query` で kintone が `limit` > 500・`offset` > 10,000 を 400 + `CB_VA01` で拒否したら `argument_error`(retryable でない、`details.http_status` / `kintone_code`)になることを E2E で固定した(分類は M02 のまま。実際の応答は V07 で確認)。
