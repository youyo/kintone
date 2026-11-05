# ADR 0007: 名前解決の仕様とキャッシュ(grant ごとのキー、TTL、無効化)

- 状態: 提案(M03 で採用予定。方針は推奨方針 5)。書き込み系の `app_ref`(読み取り系は Go 版どおり)はユーザー決定により確定(2026-11-05)
- 日付: 2026-11-05
- 関連: ADR 0004、ADR 0010(`userId` は Access の email のハッシュ)、ADR 0008、logbutler ADR 0005(KV の `idem:`)、logbutler ADR 0016(`statuses:<id>` 600 秒)

## 状況

Go 版の名前解決(`internal/resolver/`)は stateless で、`apps.json` / `form/fields.json` の結果は `CachingAPI` が SQLite などに **TTL 1 年・キーはドメインだけ**で保存していた(Go `internal/store/cache.go:10-12`、`internal/service/api/caching.go:74,98,139-186`)。無効化は CLI の `cache clear` だけ。
`apps.json` は実行ユーザーが見えるアプリだけを返す(必要権限: レコード閲覧または追加。公式)ので、ドメイン単位の共有キャッシュは別の利用者に見えないはずのアプリ名を見せうる(Go 版の問題点)。Remote MCP には `cache clear` に当たる手段がない。

## 決定

### 解決の仕様(Go 踏襲。仕様書 §7)
- アプリ: 正の整数 → ID 直接 → コード完全一致 → 名前(完全一致 → 部分一致)。各段階でヒットしたら即採用、フォールバックしない。複数なら `resolver_app_ambiguous`、なければ `resolver_app_not_found`(Go `app.go:27-76`)。
- **書き込み系(`kintone_record_create` / `record_update` / `record_delete`)の `app_ref` は、ID・コード完全一致・名前完全一致だけで解決する**(ユーザー決定)。名前の完全一致が 0 件で部分一致だけが当たったときは、1 件でも複数件でも**採用せずエラー `resolver_app_exact_match_required`** を返し、`details.candidates`(`[{"id","code","name"}]`)に部分一致の候補を入れる(AI は候補の ID を `app` に渡して再実行できる)。完全一致が 2 件以上なら従来どおり `resolver_app_ambiguous`。読み取り系(`app_describe` / `records_query`)は Go 版どおり部分一致の単一ヒットを採用する。
  - 実装: リゾルバーは `mode: "read" | "write"` を受ける。API 呼び出しの回数・順序は両モードで同じ(ADR 0008 の最悪ケースの式は変わらない)。キャッシュには両モードとも同じ値(名前検索の全ページ)を置き、モードは判定時に適用する。
  - `update_key_field_ref`(`kintone_record_update` の更新キーのフィールド参照)も**完全一致(フィールドコード・ラベル)だけ**で解決する(ユーザー決定 2026-11-05)。ラベルの部分一致しか当たらないときは、1 件でも複数件でも採用せずに `resolver_field_exact_match_required` を返す。`details.candidates`(`[{"code","label","type"}]`)に部分一致の候補を入れる。完全一致が 2 件以上なら `resolver_field_ambiguous`。意図しないフィールドを更新キーにして、別のレコードを書き換えるのを防ぐため。
- フィールド: コード完全一致 → ラベル完全一致 → ラベル部分一致(`field.go:27-73`)。トップレベルのみ。
- **Go との意図した差分**: 名前検索のページ上限を 100 ページ(10,000 件)から **10 ページ(1,000 件)**に下げる(`resolver_app_list_too_large` の閾値)。理由は ADR 0008(Workers 無料プランの subrequest 上限 50 に、アプリ解決を収めるため)。

### キャッシュ
- 置き場: `OAUTH_KV`(別 binding は作らない。logbutler の `idem:` / `statuses:` と同じ方針)。接頭辞 `kcache:v1:`(ライブラリの `client:` `grant:` `token:` と衝突しない)。
- キーに `userId` と `grantId` の両方を含める(**grant 単位**): `kcache:v1:<userId>:<grantId>:appcode:<h>` / `appname:<h>` / `fields:<appId>`。`userId` は Access の email のハッシュ(ADR 0010)、`grantId` は MCP トークン(`{userId}:{grantId}:{secret}`)から取り出す。email 単位(`userId` だけ)にしない理由: email が示すのは「Access にログインした人」で、その人が kintone で別のアカウントを認可していても Worker は気づけない(ADR 0010 の限界)。アプリの見え方は kintone アカウントごとに違うので、grant をまたいで共有しない。`<h>` は `ref` の SHA-256 の先頭 16 文字。キー長は KV の上限 512 バイトに十分収まる。
- 値は解決に必要な最小限: アプリ `{appId, code, name}` の配列、フィールド `{code, label, type}` の配列(作成者・更新者などの個人情報を含まない)。KV に平文で置かれる(ライブラリの暗号化は props だけ)ので、kintone のアプリ名・フィールド名が KV に載ることを runbook に書く。
- **TTL 600 秒**(logbutler の `statuses:` と同じ)。Go の 1 年は `cache clear` がある前提。アプリ名の変更・フィールドの追加は日常的に起きる。
- 読む: 名前解決(アプリ・フィールド)。`kintone_apps_search` と `kintone_app_describe` は読まない(利用者が「いまの状態」を見るためのツール)。書く: 名前解決で取得したとき、`kintone_app_describe` でフィールドを取得したとき。
- 無効化:
  1. TTL。
  2. キャッシュ由来の解決が `not_found`(アプリ・フィールドとも)になったら、キャッシュを無視して**1 回だけ**取り直す。ヒット(1 件)が古い(別名に変わった)場合は検知できないが、その場合は次の 3 で拾う。
  3. キャッシュで解決したアプリへの kintone 呼び出しが `not_found` を返したら、そのアプリのキャッシュを消してエラーを返す(再解決はしない)。
  4. 手動削除の手段は持たない。
- KV の失敗はツール呼び出しを止めない(キャッシュなしで続行。例外名だけログ。logbutler ADR 0005 と同じ)。

## 理由

- grant ごとのキーなら、アプリの見え方の違い(同じ email で別の kintone アカウントを認可した場合を含む)による漏れが起きない。再認可で grant が変わるとキャッシュは引き継がれないが、10 分で消えるので実害は小さい。
- 10 分の TTL + 取り直しで、日常の変更はほぼ追従し、`cache clear` 相当を作らずに済む。
- `app_describe` が最新を取ってキャッシュを更新するので、AI が「定義を見直す」自然な操作が無効化になる。

## 却下した案

- **共有キャッシュ(ドメインだけのキー)**: 利用者ごとにアプリの見え方が違う。
- **`userId`(email 単位)だけのキー**: 上記の限界のため。
- **書き込み系でも部分一致の単一ヒットを自動採用する(Go 版の挙動)**: `app_ref: "顧客"` が `顧客管理` だけに部分一致すると、そのまま `kintone_record_delete` が実行されうる。ユーザー決定で却下。
- **TTL 1 年 + 手動無効化ツールを足す**: ツール数が増える。変更に気づけない状態が長く続く。
- **キャッシュしない**: 最悪ケースで毎回 11 回の API 呼び出しになる。同じ `app_ref` を何度も使う会話で無駄が大きい。
- **ID → 名前のキャッシュを別に持ち、書き込み系で逆引きして検証する**: 複雑さに見合わない。

## 帰結(書き込み系の部分一致)

- Go 版との挙動差: 書き込み系で部分一致のみの `app_ref` は、Go 版では採用されたが、Workers 版ではエラーになる。ツールの description に「書き込みでは `app_ref` はアプリ ID・コード・名前の完全一致だけ。部分一致の候補が返ったら ID で再実行する」と書く(M04)。
- 新しいエラーコード `resolver_app_exact_match_required`(`retryable: false`、`details.kind = "app"`、`ref`、`candidates`)を仕様書 §6 に追加した。部分一致も 0 件なら `resolver_app_not_found`(従来どおり)。
- テスト: リゾルバーの unit(M03)で write モードの全分岐(ID、コード完全一致、名前完全一致、部分一致のみ 1 件・複数件、完全一致 2 件)、M04 の E2E で `record_delete` / `record_update` / `record_create` に部分一致のみの `app_ref` を渡すと kintone の書き込み API が**呼ばれない**こと。

## 未検証の前提

- `form/fields.json` の必要権限(アプリ管理権限が要る場合、一般利用者の `update_key_field_ref` と `app_describe` が `permission_error` になる。V09)
- KV の読み書きが subrequest 上限に数えられるか(V12)
