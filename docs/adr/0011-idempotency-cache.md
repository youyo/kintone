# ADR 0011: `kintone_record_create` の冪等キャッシュ(KV の `idem:`、grant 単位)

- 状態: 採用(M04 で実装)
- 日付: 2026-11-06
- 関連: ADR 0006(エラー形式)、ADR 0007(キャッシュのキー)、ADR 0008(書き込みはリトライしない)、ADR 0009(`idem:` の採用)、ADR 0010(grant 単位のキー)、logbutler ADR 0005(冪等キャッシュを KV に置く)

## 状況

MCP の stream 再開が廃止され、クライアントが接続断のあとに同じ create 系ツールの呼び出しを再送する可能性が上がった。`kintone_record_create` は再送されると同じレコードを重複して作る(kintone に重複防止の仕組みはない)。クライアント(M02)は書き込みをリトライしないので、重複の原因は MCP クライアント側の再送だけである。Workers はリクエストごとに別 isolate になりうるので、メモリには置けない。

姉妹プロジェクト logbutler は WriteNonIdempotent の 4 ツールを KV の `idem:<backlogUserId>:<toolName>:<explicit|hash>:<値>` で守っている(TTL 300 秒、成功だけ保存)。kintone 版はこれを引き継ぎ、利用者の単位を grant にし、明示キーの扱いを一段安全にする。

## 決定

- **対象は WriteNonIdempotent の区分のツールだけ**(今は `kintone_record_create`)。`record_update`(同じ内容での上書きは結果が同じ)と `record_delete`(二重に消しても結果が同じ)は対象外(ADR 0009)。`registerTools` が、引数の検証が済んだあと・kintone クライアントを作る前にハンドラを包む。実装は `src/mcp/idempotency.ts`。
- **保存先は `OAUTH_KV` の `idem:` 接頭辞**(別 binding は作らない)。ライブラリの接頭辞(`client:` `grant:` `token:`)や `kcache:v1:` `kintone-stale:` と衝突しない。
- **キーは grant 単位**: `idem:<userId>:<grantId>:<toolName>:<explicit|hash>:<SHA-256 16 進>`。`<userId>:<grantId>` は MCP トークンから取り出した値(キャッシュと同じ。ADR 0007 / 0010)。email 単位にしない(同じ email で別の kintone アカウントを認可しうるため)。`userId` は email のハッシュなので、生の email はキーに入らない。MCP トークンの secret 部分も入らない。**grant が分からない・KV がないときは冪等キャッシュを使わず**、通常の書き込みとして実行する。
- **明示キー**: `params._meta.idempotency_key` → なければ引数 `idempotency_key`(logbutler と同じ。`inputSchema` には宣言せず、自前の検証の前に引数から外して kintone へは送らない)。空文字・文字列でない値は無いものとして扱う。UTF-8 で 200 バイトを超えたら `argument_error`。**KV のキーに入れるのは明示キーそのものではなく SHA-256**(クライアントが任意の文字列を KV のキーに載せないように、長さを一定にするため。logbutler は生の値を入れていた)。
- **引数ハッシュ**: 明示キーがなければ、検証済みの引数を canonical JSON(オブジェクトのキーをソート)にした SHA-256。同じ引数の再送だけが当たる。
- **保存する値**: `{args: <引数ハッシュ>, result: <成功した結果の JSON 文字列>}`。TTL **300 秒**(KV の最小 60 秒以上)。**エラーは保存しない**(保存すると一時障害のあとに再送できなくなる)。
- **同じ明示キーで別の引数が来たら、前回の結果を返さず `argument_error`**(「idempotency_key は別の引数で使用済み」)で、kintone を呼ばない。logbutler は同じ明示キーなら引数が違っても前回の結果を返していたが、それだと別のレコードの作成が黙って捨てられる(クライアントがキーを使い回したとき)ので、意図して変えた。
- **重複だったとき**は、初回と同じ結果をそのまま返す(印は付けない)。名前解決の GET も書き込みも呼ばない。
- **KV の失敗は呼び出しを止めない**: get が失敗したら実行し、put が失敗しても結果は返す。保存された値が壊れている・形が違うなら「なし」として実行する。ログには例外名だけ(キー・値・message は出さない)。

## 限界(受け入れる)

- KV は結果整合で、排他制御(ロック)もない。**ほぼ同時の 2 回の呼び出しは、両方実行されうる**(1 回目の保存が 2 回目の読み取りに間に合わない)。Durable Objects は使わない方針なので受け入れる。これは再送(時間をおいた同じ呼び出し)への対策であり、同時実行の排他ではない。
- 別のロケーションからの読み取りは、保存から最大 60 秒ほど古い値を見ることがある。再送が直後に別のロケーションに届くと重複しうる(V25)。
- 通信失敗・タイムアウト・5xx のときは、書き込みが行われたか分からず、結果も保存されない。再送は重複しうる(`api_error` は `retryable: true` のまま。再送の判断は AI に委ねる。ADR 0006)。
- 引数ハッシュ方式では、**同じ引数で意図的に 2 件作る**ときも 5 分間は 2 回目が前回の結果になる。区別したいときは別の `idempotency_key` を渡す(ツールの description に書いた)。
- 明示キーを引数 `idempotency_key` として渡す方法は `inputSchema` に載らない(公開スキーマに出ない)。`_meta` を送れる MCP クライアントは `_meta` を使える。

## 却下した案

- Durable Objects でキーごとに直列化する: 排他は取れるが、DO なしの方針に反する。
- Cache API(`caches.default`): データセンター単位で、保存の保証も弱い。
- 別の KV namespace: 運用手順(namespace の作成と id の差し替え)が増える割に得るものが少ない。
- エラーも保存する: 一時障害のあとに再送できなくなる。
- email 単位(`userId` だけ)のキー: grant をまたいで結果を共有してしまう(ADR 0010)。
- `record_update` / `record_delete` にも付ける: 結果が同じなので不要。
- 明示キーを `inputSchema` に宣言する: 仕様書 §5.4 の引数表と logbutler の baseline に合わせて宣言しない(必要になれば足す)。

## 未検証の前提

- KV の結果整合が、直後の再送の重複防止にどの程度効くか(V25)
- 書き込みの 429 が未処理か(V08。現状はリトライしない)
