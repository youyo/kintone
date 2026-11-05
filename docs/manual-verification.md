# 実機確認リスト

kintone Remote MCP(Workers 版)はまだ実装・デプロイしていない。ADR と仕様書に「未検証の前提」として書いたものを、ここに集約する。
ユーザーが検証用の環境を用意したら、この表の順に実施し、**結果の欄に記入する**(実施はユーザーの指示を受けてから)。
デプロイ手順は M05 の `docs/runbook.md`(まだない)。M01 時点では `wrangler dev` / 手動 `wrangler deploy` で足りる範囲を使う。

## 使い方

- 表記: `<host>` は kintone のホスト(例 `example.cybozu.com`)、`<domain>` はこの Worker の公開ドメイン、`BASE=https://<domain>`。
- **検証用の kintone 環境(テスト用サブドメイン、または本番に影響しない専用アプリ・専用ユーザー)を使う**。書き込みを伴う項目は専用アプリでだけ行う。
- トークン・クライアントシークレット・`.verify-oauth.local.json` の中身を、この表・Issue・チャットに**貼らない**。値の代わりに「一致 / 不一致」「あり / なし」を書く。
- 結果は `OK` / `NG` / `要対応` のどれかで記入し、`メモ` に日付・環境・気づきを書く。NG は該当 ADR の「未検証の前提」を更新する。
- 実施前の準備: ロードマップの手順 U2(OAuth クライアント登録。リダイレクト URI は `https://<domain>/callback`)・U3(利用者の設定)・U4(Access の self-hosted アプリ。`/authorize` と `/callback` のパス。AUD とチームドメインの取得)・U5(KV と Variables / Secrets。`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` を含む)を終え、`curl -s $BASE/healthz` が `{"status":"ok"}` を返すこと。

## 実環境の OAuth 確認ツール `mise run verify:oauth`(M01 で実装済み)

V01(一部)・V02・V04・V05・V15・V19(a)・P01〜P04 の確認に使う、テスト用の小さな MCP クライアント。

```sh
mise run verify:oauth -- $BASE/mcp                 # フル検証(ブラウザで kintone のログインと同意を 1 回する)
mise run verify:oauth -- $BASE/mcp --refresh-only  # 保存済みの状態で更新だけ(kintone のトークン期限切れ後に使う)
```

- ツールは `scripts/verify-oauth/`(core.ts は Web 標準 API だけで、E2E では Worker とモック kintone に対して通しで動かしている)。実環境では `loopback.ts` が `127.0.0.1` の空きポートで待ち受け、その URL を DCR の `redirect_uri` に登録して、`main.ts` が認可 URL をブラウザで開く(開けなければ URL を表示するので手で開く)。
- 人が操作するのは、ブラウザでの Access のログイン(セッションがなければ)、同意画面、kintone のログインと同意の 1 回だけ。同意画面には Access のログイン email が出る。次の kintone のログインでは、同じ本人のアカウントを選ぶこと(Worker は選んだアカウントを確かめられない。ADR 0010)。`/authorize` と `/callback` は Access の JWT がないと 403 になるので、**ツールの `authorize` 段階はブラウザ経由が必須**(コマンドラインから `/authorize` を直接叩いても通らない)。
- 各段階(`metadata` `register` `authorize` `token` `tools_list` `apps_search` `refresh` `apps_search_after_refresh`)の成否と、`expires_in`、リフレッシュでトークンが変わったかが出る。
- 状態ファイル `.verify-oauth.local.json` はリフレッシュトークンを含む(gitignore)。検証が終わったら削除する。
- このツールが見られるのは MCP 側のトークン(Worker 発行)。**kintone 側のリフレッシュトークンの挙動は見えない**(V01 は Worker の診断ログまたは別手順で見る)。

---

## A. kintone との OAuth(ADR 0001 / 0004 / 0005)

### V01 更新のローテーションと、期限内の更新で同じトークンが返るか
- 出典: ADR 0005 の未検証、公式 add-client ページ(更新応答に `refresh_token` がない例のみ)
- 目的: (a) 更新のたびに `refresh_token` が変わるか、(b) 有効期限内の更新が同じアクセストークンを返すか(`expires_in` は残り秒数か)。実装はどちらでも動く(ADR 0005)が、前提の確認と診断ログの読み方のため。
- 手順(Worker を通さず kintone に直接。検証用クライアントの ID / シークレットを使う):
  1. ブラウザで `https://<host>/oauth2/authorization?response_type=code&client_id=<クライアントID>&redirect_uri=https://<domain>/callback&scope=k%3Aapp_record%3Aread%20k%3Aapp_settings%3Aread&state=x` を開き、ログイン・同意する。
  2. 移動先 `https://<domain>/callback?code=...&state=x` は Access のログイン(未ログインなら)のあと Worker が拒否する(state が不正)が、アドレスバーの `code=` の値を控える(10 分以内、1 回限り)。
  3. シェル(トークンは変数にだけ入れ、画面に出さない):
     ```sh
     read -rs CLIENT_SECRET; read -rs CODE
     BASIC=$(printf '%s:%s' '<クライアントID>' "$CLIENT_SECRET" | base64)
     T1=$(curl -s -X POST https://<host>/oauth2/token -H "Authorization: Basic $BASIC" \
       -d grant_type=authorization_code -d redirect_uri=https://<domain>/callback -d code=$CODE)
     echo "最初の expires_in: $(echo "$T1" | jq .expires_in)"
     R1=$(echo "$T1" | jq -r .refresh_token); A1=$(echo "$T1" | jq -r .access_token)
     T2=$(curl -s -X POST https://<host>/oauth2/token -H "Authorization: Basic $BASIC" -d grant_type=refresh_token -d refresh_token=$R1)
     echo "更新応答に refresh_token があるか: $(echo "$T2" | jq 'has("refresh_token")')"
     echo "更新後の expires_in: $(echo "$T2" | jq .expires_in)"
     echo "アクセストークンが変わったか: $([ "$(echo "$T2" | jq -r .access_token)" != "$A1" ] && echo はい || echo いいえ)"
     T3=$(curl -s -X POST https://<host>/oauth2/token -H "Authorization: Basic $BASIC" -d grant_type=refresh_token -d refresh_token=$R1)
     echo "古いリフレッシュトークンで再更新できるか: $(echo "$T3" | jq 'has("access_token")')"
     unset CLIENT_SECRET CODE BASIC T1 T2 T3 R1 A1
     ```
  4. 失効後(約 1 時間後)にもう一度、同じリフレッシュトークンで更新して新しいアクセストークンが出ること、`expires_in` が約 3600 であることを確かめる。
- 結果: ローテーション [ 未実施 ] ／ 期限内の更新で同じトークンか [ 未実施 ] ／ 失効後の更新 [ 未実施 ] ／ 判定 [ ] ／ メモ:
- 運用中のログでも確認できる: `{"event":"kintone_token","grant_type":"refresh_token","expires_in":...,"refresh_token_rotated":true|false}`、`token_exchange` の `access_token_changed`(値は出ない)。

### V02 初回の `expires_in` と実際の寿命
- 出典: 公式(1 時間)、ADR 0005
- 目的: コード交換直後の `expires_in` が 3600 か、実際に約 1 時間で 401 になるか(logbutler の Backlog で、保存期限より先に失効した観測がある=logbutler ADR 0001 の未検証 5)。
- 手順: `mise run verify:oauth -- $BASE/mcp` の直後と、認可の約 1 時間後に `--refresh-only` を実行する。Worker のログに `kintone_unauthorized` が出ていないか、`apps_search_after_refresh` が OK かを見る。
- 期待結果: 保存期限どおりに失効し、失効後の最初のリフレッシュで回復する。`kintone_unauthorized`(`kintone_remaining_seconds` が正)が出るなら、期限前に失効している。ADR 0005 の stale 印で回復するが、結果を記録して ADR を更新する。
- 結果: 初回 expires_in [ ] ／ 実際の失効時刻との差 [ ] ／ 判定 [ ] ／ メモ:

### V03 利用者の許可がない利用者の認可
- 出典: 公式 add-client ページ(STEP2「利用者の設定」)、ADR 0001
- 目的: 「利用者の設定」に入っていない kintone ユーザーが認可しようとしたとき、どこで・どう失敗するか(kintone の認可画面でエラーか、コード交換か、`apps.json` の疎通確認か)。Worker の `/callback` のエラー処理と、runbook の案内文を決める。
- 手順: 許可していないユーザーで `verify:oauth` を実行し、クライアントに返るエラー(`server_error` / `access_denied` など)と Worker のログ(`kintone_*` イベント、値なし)を記録する。
- 結果: 失敗する段階 [ ] ／ kintone のエラー(コード・メッセージ) [ ] ／ 判定 [ ] ／ メモ:

### V04 PKCE の検証有無・拒否の有無
- 出典: ADR 0004。公式に PKCE の記載なし。Go 版は常に `code_challenge` を送って運用された(Go `handler.go:282`)
- 目的: (a) `code_challenge` / `code_verifier` を付けても拒否されないこと(Go の実績の再確認)、(b) kintone が verifier を検証しているか(誤った verifier でコード交換が失敗するか)。
- 手順: (a) `verify:oauth`(PKCE 既定 `s256`)が成功すること。(b) V01 の手順 1 の認可 URL に `code_challenge=<任意の値>&code_challenge_method=S256` を付けて認可し、手順 3 のコード交換に **違う** `code_verifier` を付けて送る。成功すれば kintone は検証していない。
- 期待結果: (a) 成功。(b) は結果を記録するだけ(検証されていなくても害はない)。(a) が失敗したら `KINTONE_OAUTH_PKCE=off` を設定して再試行し、ADR 0004 を更新する。
- 結果: (a) [ ] ／ (b) 検証される [ はい / いいえ ] ／ 判定 [ ] ／ メモ:

### V05 `redirect_uri` の条件
- 出典: ADR 0004。公式に条件の記載なし。Go 版の運用では https 必須・loopback の http は成立しなかった(Go `CHANGELOG.md` v0.4.0、README)。したがって実機確認は公開 https の URL が前提
- 目的: (a) `https://<domain>/callback` が登録できて受理されること、(b) `http://localhost:8787/callback` を登録・使用できるか(ローカル開発で認可フローを実機に通せるか)、(c) 1 つのクライアントに複数のリダイレクト URI を登録できるか。
- 手順: cybozu.com 共通管理の OAuth クライアント登録画面で試す。
- 結果: (a) [ ] ／ (b) [ ] ／ (c) [ ] ／ 判定 [ ] ／ メモ:

### V06 リフレッシュトークン 10 個上限を超えたとき
- 出典: 公式 add-client ページ(1 クライアント・1 ユーザーあたり 10 個まで)、ADR 0001 / 0004 / 0005
- 目的: 11 回目の認可で、古いリフレッシュトークンが失効するのか新規が拒否されるのか。失効するなら、失効した grant が次の更新で `invalid_grant` になり再認可になる(MCP クライアントを 10 個超つなぐ利用者への影響)。
- 注意: 同じ email・同じ MCP クライアントの再認可は、Worker が旧 grant を失効させる(ADR 0010)。そのため `verify:oauth` が同じクライアント登録を使い回すと、最初の状態の更新は Worker 側で `invalid_grant` になり、kintone の挙動を見たことにならない。kintone 側だけを見るには、V01 の手順 1〜3 のコード交換(Worker を通さない curl)を 11 回繰り返して 11 個のリフレッシュトークンを得て、最初のものを更新する。
- 手順: 検証用ユーザーで、上の注意に従って 11 個目の認可を行い、最初のリフレッシュトークンで更新する。
- 結果: 11 回目の認可 [ ] ／ 最初の状態の更新 [ ] ／ 判定 [ ] ／ メモ:

### V07 トークンエンドポイントと REST のエラーの実形・ステータス
- 出典: ADR 0005 / 0006。Go の分類(`internal/kintoneapi/errors.go:91-114`)は 401/403/404/429/5xx/422/`CB_VA` 接頭辞
- 目的: 次を実測して仕様書 §6 の表を確定する。
  1. トークンエンドポイント: 失効・取り消し済みのリフレッシュトークン、誤ったクライアントシークレットのときのステータスと本文(`error` フィールドがあるか、`invalid_grant` / `invalid_client` か。本文なし・JSON でない 4xx が返るかも確認する。実装は `error` が明示的に `invalid_grant` のときだけ grant を失効させ、それ以外の読めない 4xx は `rejected` にしている)
  2. REST: 失効したアクセストークン、アプリの権限なし、存在しないアプリ ID、不正なクエリ、必須項目の欠け、リビジョン不一致のときのステータスと `code`(`CB_VA01`・`GAIA_*` など)。HTTP 520 が返るケース
- 手順: curl で各ケースを起こし、ステータス・`code`・`message` を記録する(トークンは貼らない)。
- 結果: (表に記入)

| ケース | ステータス | code | 仕様書 §6 の分類で合うか |
|---|---|---|---|
| 失効したアクセストークン(REST) | | | |
| 取り消し済みのリフレッシュトークン(token) | | | |
| 誤ったクライアントシークレット(token) | | | |
| 権限のないアプリ | | | |
| 存在しないアプリ ID | | | |
| 不正な `query` | | | |
| `CB_VA...` 接頭辞のコードが返るときの HTTP ステータス(400 以外か。M02 は 520 などでも `argument_error` にする) | | | |
| 200 なのに JSON でない・空の本文が返ることがあるか(IP 制限・プロキシの HTML など。M02 は `api_error`・retryable・`http_status: 200` にする) | | | |
| 3xx(リダイレクト)が返ることがあるか(M02 は辿らず `api_error`) | | | |
| `revision` の不一致(update) | | | |

---

## B. kintone API の挙動(ADR 0003 / 0006 / 0007 / 0008)

### V08 429 の `Retry-After` と、書き込みの 429
- 出典: ADR 0008。公式(kintone.dev)に同時接続 100 と 429 の記載はあるが `Retry-After` の記載はない
- 目的: (a) 429 に `Retry-After` が付くか、(b) 429 を受けた書き込みが未処理か(リトライしてよいか)。現状は書き込みをリトライしない。
- 手順: 検証環境で同時接続を 100 超にして 429 を起こす(専用環境でのみ)。ヘッダーを記録する。
- 結果: (a) [ ] ／ (b) [ ] ／ 判定 [ ] ／ メモ:

### V09 `form/fields.json` の必要権限(アプリ管理権限が要るか)
- 出典: ADR 0007。公式ページの本文を取得できなかった(404)。OAuth scope は `k:app_settings:read`(scope ページ)
- 目的: 一般の閲覧者(アプリ管理権限なし)が `GET /k/v1/app/form/fields.json` を OAuth で呼べるか。呼べないなら `kintone_app_describe` と `update_key_field_ref` が `permission_error` になる。
- 手順: アプリ管理権限のないユーザーで認可し、`kintone_app_describe`(M03 で実装済み)または curl で呼ぶ。
- 結果: 呼べる [ はい / いいえ ] ／ 失敗時のステータスとコード [ ] ／ 判定 [ ] ／ メモ:

### V10 IP アドレス制限・他ドメイン
- 出典: ADR 0003、公式 add-client ページ(IP 制限時はクライアントの IP を許可する必要)
- 目的: (a) cybozu.com 共通管理の IP 制限が有効な環境で、Worker からの OAuth(認可・コード交換・更新)と REST が通るか(Workers の送信元 IP は固定でない)、(b) `.cybozu.com` 以外(kintone.com、cybozu.cn)で `/oauth2/authorization` と `/oauth2/token` が同じ形か。
- 手順: (a) IP 制限を有効にした検証環境で `verify:oauth` を実行する。(b) 該当ドメインのアカウントがあれば実施(なければ「未実施」でよい)。
- 結果: (a) [ ] ／ (b) [ ] ／ 判定 [ ] ／ メモ:

### V11 GET の URL 長の上限
- 出典: ADR 0008。公式 get-records ページに URL 長の記載なし
- 目的: `fields` 100 個 + 長い `query`(数 KB)の `GET /k/v1/records.json` が通るか。Workers の `fetch` は GET に本文を付けられないため、クエリ文字列で送る。
- 手順: 検証アプリで、`fields` を 100 個、`query` を 2 KB / 4 KB / 8 KB にして呼び、通る長さの上限を記録する。
- 結果: 通った最大の URL 長 [ ] ／ 超えたときのステータス [ ] ／ 判定 [ ] ／ メモ:

### V12 KV の読み書きは subrequest に数えられるか
- 出典: ADR 0008 / 0007。logbutler ADR 0003 は「Workers 無料プランの subrequest 上限 50」を根拠にしている
- 目的: KV の get/put が subrequest 上限に数えられるか。数えられるなら最悪ケース(論理 13 回 + KV)の見積もりを直す。M03 の `maxApiCalls`(3 / 39 / 36)は kintone への HTTP だけを数え、KV の get/put を含めない。名前解決の KV は最悪で 2 回の get + 2 回の put(+ 取り直し分)、`app_describe` はさらに fields の put 1 回。
- 手順: Cloudflare の公式ドキュメント(Workers の limits)を確認し、無料プラン相当で名前解決の最悪ケース(10 ページ満杯のモック)を `wrangler dev --remote` 等で通す。
- 結果: 数えられる [ はい / いいえ ] ／ 出典 URL [ ] ／ 判定 [ ] ／ メモ:

### V13 ゲストスペースのアプリに対する失敗
- 出典: ADR 0003。公式: 通常ユーザーの `/k/v1/apps.json` は閲覧可能なゲストスペース内のアプリも返す
- 目的: `kintone_apps_search` に出たゲストスペースのアプリを `/k/v1/records.json` に渡したときの実際のステータスとコード。ツール description の文言を決める。
- 手順: ゲストスペースにアプリがある環境で、`apps.json` の結果の ID を `/k/v1/records.json` に渡す。
- 結果: ステータス [ ] ／ code [ ] ／ 判定 [ ] ／ メモ:

### V14 OAuth トークンの持ち主を返す API がないこと
- 状態: **確認済み(文書上)**: 公式の REST API 一覧(レコード・ファイル・アプリ・スペース・プラグイン・API 情報)に、実行ユーザー自身を返す API はない。User API(`/v1/users.json`)の認証方式に OAuth の記載がない。「ログインユーザーの情報を取得する」は JavaScript API(`kintone.getLoginUser()`)でカスタマイズ内でしか使えない。
- 出典: ADR 0004 の表(https://cybozu.dev/ja/kintone/docs/rest-api/ 、https://cybozu.dev/ja/common/docs/user-api/overview/authentication/ 、https://cybozu.dev/ja/kintone/docs/js-api/kintone/get-login-user/ )
- この確認結果が、利用者 ID を Cloudflare Access の email にする根拠(ADR 0010)。
- 任意の追加確認(実機。未実施): OAuth のアクセストークンを `Authorization: Bearer` で `https://<host>/v1/users.json?size=1`、`/v1/user/groups.json` に送り、通らないことを見る(scope は 6 つ)。通るなら ADR 0010 の限界(email は kintone のアカウントと一致しない)を解消できる可能性があるので、結果を記録して ADR 0010 を見直す。
- 結果: 文書上の確認 [ 済 ] ／ 実機の追加確認 [ 未実施 ] ／ メモ:

### V15 KV の結果整合性が stale 印・更新に影響しないか
- 出典: ADR 0005(logbutler ADR 0001 の未検証 2、V02 と同種)
- 目的: `/mcp` と `/token` が別ロケーションで処理されたとき、stale 印の伝播が遅れて回復が最大 60 秒ほど遅れる程度に収まるか。並列更新の挙動。
- 手順: kintone の失効後、`--refresh-only` を間隔をあけず 5 回連続、および同時に 3〜5 本。別ネットワークからも実施。
- 結果: 連続 [ ] ／ 並列 [ ] ／ 判定 [ ] ／ メモ:

### V16 1 日あたりの API リクエスト数の上限
- 出典: ADR 0008。第三者の解説で「アプリあたり 1 日 10,000 回」とあるが、公式ページで確認できていない
- 目的: 公式の上限(ある場合)を確認し、ツールの設計(呼び出し回数)に影響がないか見る。
- 手順: cybozu.dev / kintone.dev の公式ドキュメント(REST API の制限事項)で確認する。
- 結果: 公式の上限 [ ] ／ 出典 URL [ ] ／ 判定 [ ] ／ メモ:

---

### V22 名前検索(`apps.json?name=`)の部分一致の挙動と長さの上限(M03 で追加)
- 出典: 仕様書 §7、ADR 0007。公式 get-apps ページは「`name` は部分一致・大文字小文字を区別しない」「64 文字以内」と書く。実機では未確認
- 目的: (a) `name=` が大文字小文字・全角半角・濁点の違いをどう扱うか(リゾルバーの完全一致の判定は大文字小文字を区別して自前で行うので、kintone が区別しない前提と食い違うと完全一致を取りこぼす)、(b) 64 文字を超える `codes[0]` / `name`(`app_ref`)を渡したときのステータスとコード(`argument_error` になる想定)、(c) `apps.json` の `limit=100` / `offset` が 1,000 件目まで取れるか。
- 手順: 検証環境で、`Sales` / `sales` / `ＳＡＬＥＳ` のようなアプリを作り、`kintone_apps_search` の `name` と `kintone_records_query` の `app_ref` で確かめる。65 文字の `app_ref` を渡す。
- 結果: (a) [ ] ／ (b) [ ] ／ (c) [ ] ／ 判定 [ ] ／ メモ:

### V23 `records.json` の `totalCount` の型(M03 で追加)
- 出典: 仕様書 §5.3。公式は文字列で返すと書く(Go 版も文字列として受けて数値に変換していた)
- 目的: `totalCount=true` のときの実際の型(文字列の想定)。数値で返っても `Number()` で数値になるが、`null` や空のときの挙動を確認する(M03 は `null` は出力に出さず、数として読めない文字列は `api_error`)。
- 手順: 検証アプリで `kintone_records_query` に `total_count: true` を渡す。0 件のアプリでも確認する。
- 結果: 型 [ ] ／ 0 件のとき [ ] ／ 判定 [ ] ／ メモ:

## C. MCP クライアント・Portal(ADR 0002)

### P01 Portal に「OAuth(DCR 自動登録)+ Require user auth」で登録でき、ツールが同期されること
- 目的: Portal が DCR / CIMD のどちらで接続するか、登録時に Worker の `/register` が成功するか。
- 結果: [ ] ／ メモ:

### P02 各ユーザーが Portal から自分の kintone アカウントで認可できること
- 目的: 認可後に `kintone_apps_search` が自分の見えるアプリだけを返すこと(利用者ごとの権限が効くこと)。2 人で確認する。
- 結果: [ ] ／ メモ:

### P03 Portal がクエリ付き URL(`?read_only=true` など)を保持するか
- 出典: logbutler ADR 0008 の P3 と同種。M02 で `/mcp` の `read_only` / `toolsets` を実装済み(不正な値・綴り違いのキーは 400。Portal が付ける別のクエリは無視する)。確認すること: Portal の登録 URL に付けた `?read_only=true` が `tools/list` の取得にも `tools/call` にも保たれ、書き込みツール(M04 以降)が隠れること。Portal が独自のクエリを足しても 400 にならないこと(`read` / `tool` で始まるキーは綴り違いとして 400 になるので、Portal がそのようなキーを足さないこと)
- 結果: [ ] ／ メモ:

### P04 Portal 経由の MCP クライアントで、ブラウザが `/authorize` へ遷移したときに Access の cookie と JWT が付くか
- 出典: ADR 0002 / 0010。Portal が前段の Access とチームを共有する場合、同じブラウザで Access のセッションが再利用されるかは未確認
- 目的: Portal から接続操作をしたとき、(a) ブラウザが開く `/authorize` に `Cf-Access-Jwt-Assertion` が付いて同意画面が出る(Access のログインが再度必要か、シームレスか)、(b) `/callback` でも付く。
- 結果: (a) [ ] ／ (b) [ ] ／ 再ログインの要否 [ ] ／ メモ:

---

## D. Cloudflare Access(ADR 0010)

前提: ロードマップの U4(Access の self-hosted アプリ。パスは `/authorize` と `/callback` だけ)を終えていること。`<team>` は Access のチームドメイン。JWT の中身(email・トークン)をこの表・Issue・チャットに**貼らない**(「email claim あり / なし」だけ書く)。

### V17 JWT の `email` claim が IdP によらず入るか
- 出典: awsbridge `docs/specs/architecture.md`(開発環境の Google で `email` が入ることを観測し「IdP によらず必ず入る」と記すが、他の IdP は未確認。Entra は Email claim の設定で決まるとある)
- 目的: 利用する IdP(複数ある場合はすべて)で、`/authorize` に付く `Cf-Access-Jwt-Assertion` に `email` claim が入ること。入らないと Worker は認可を進められない(UUID へのフォールバックはない)。
- 手順: 各 IdP のユーザーで `/authorize` を開き、同意画面の「Access のログイン」に email が出ること(出なければ 403)を見る。JWT を直接見るなら、ブラウザの開発者ツールで `CF_Authorization` cookie の JWT をデコードし、`email` claim の有無だけ記録する。
- 結果: IdP ごとの email claim [ あり / なし ] ／ 大文字小文字の揺れ [ ] ／ 判定 [ ] ／ メモ:

### V18 self-hosted アプリのパス指定で `/authorize` と `/callback` だけを保護できるか
- 出典: ADR 0010。パス指定の挙動(サブパスを含むか、前方一致か)は未確認
- 目的: (a) `/authorize` と `/callback` が Access を経由する、(b) `/mcp` `/token` `/register` `/.well-known/oauth-authorization-server` `/.well-known/oauth-protected-resource/mcp` `/healthz` が Access を経由せず到達できる(MCP クライアントは Access のログインができないため)。
- 手順: ログインしていない状態で `curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n'` を各パスに送る。`/authorize` と `/callback` は Access のログインへの 302(または 403)、他は Worker の応答(`/healthz` 200、`/mcp` 401、`/token` 400 ほか)。
- 結果: (a) [ ] ／ (b) [ ] ／ `/authorize/` のような末尾やサブパスの扱い [ ] ／ 判定 [ ] ／ メモ:

### V19 `/callback` のリダイレクトで JWT が付くか(kintone のログイン中に Access のセッションが切れたとき)
- 出典: ADR 0010。kintone の認可画面から `https://<domain>/callback?code=...&state=...` へブラウザがリダイレクトされるとき、Access の cookie が付いて `Cf-Access-Jwt-Assertion` が Worker に届くか
- 目的: (a) 通常の認可で `/callback` が 403 にならずに完了する、(b) kintone のログインに時間をかけて Access のセッションが切れた場合に、Access のログイン画面を経由したあとも `code` と `state` が保たれて `/callback` に戻れるか、戻れず失敗するか(失敗するなら runbook に「やり直す」と書く)。
- 手順: (a) `verify:oauth` が完了する。(b) Access のセッション期間を短くした検証用アプリで、kintone のログイン画面で期限を超えるまで待ってから進める。
- 結果: (a) [ ] ／ (b) [ ] ／ 判定 [ ] ／ メモ:

### V20 `workers.dev` のホストで Access の self-hosted アプリを使えるか
- 出典: ADR 0010。公式で確認していない。ロードマップ U2 は公開 https の URL(カスタムドメインまたは `workers.dev`)を前提にしている
- 目的: self-hosted アプリにカスタムドメイン(Cloudflare に載せたゾーン)が必要か、`workers.dev` でパス指定の保護ができるか。
- 手順: Cloudflare の公式ドキュメントを確認し、可能なら `workers.dev` で V18 を実施する。
- 結果: 使える [ はい / いいえ ] ／ 出典 URL [ ] ／ 判定 [ ] ／ メモ:

### V21 kintone のリフレッシュトークンを取り消す口があるか
- 出典: ADR 0010(Worker で grant を失効させても、kintone 側のトークンは残る)。公式の OAuth クライアントのページで取り消し API の記載を確認していない
- 目的: 取り消し(revoke)エンドポイントや、利用者が自分のトークンを一覧・取り消す画面があるか。あれば、旧 grant の失効時に kintone のトークンも取り消す改善を検討できる。
- 手順: cybozu.dev の OAuth クライアント関連ページと cybozu.com の利用者設定画面を確認する。
- 結果: 取り消しの口 [ あり / なし ] ／ 出典 URL [ ] ／ 判定 [ ] ／ メモ:

---

## 一覧(ADR との対応)

| 項目 | 関連 ADR | 確認の優先度 |
|---|---|---|
| V01 ローテーション・同一トークン | 0005 | 高(M01 の後すぐ) |
| V02 初回 expires_in・失効時刻 | 0005 | 高 |
| V03 利用者の許可なし | 0001 | 高 |
| V04 PKCE | 0004 | 高 |
| V05 redirect_uri | 0004 | 高 |
| V06 リフレッシュトークン上限 | 0001 / 0005 | 中 |
| V07 エラーの実形・ステータス(M02 の分類の答え合わせ) | 0005 / 0006 | 高 |
| V08 429 の Retry-After(M02 は付かなくても 1 秒待ち × 2 回で動く) | 0008 | 中 |
| V09 form/fields の権限 | 0007 | 高(M03 の前) |
| V10 IP 制限・他ドメイン | 0003 | 中(利用環境による) |
| V11 GET の URL 長 | 0008 | 中(M03 の前) |
| V12 KV と subrequest | 0007 / 0008 | 中 |
| V13 ゲストスペースの失敗 | 0003 | 低 |
| V14 持ち主を返す API | 0004 / 0010 | 確認済み(文書上)。実機の追加確認は任意 |
| V15 KV の結果整合性 | 0005 | 中 |
| V16 1 日の上限 | 0008 | 低 |
| V17 JWT の email claim | 0010 | 高(M01 の後すぐ) |
| V18 パス指定の保護範囲 | 0010 | 高(M01 の後すぐ) |
| V19 /callback の JWT・セッション切れ | 0010 | 高 |
| V20 workers.dev での Access | 0010 | 中(ドメインの選定前) |
| V21 kintone のトークン取り消し | 0010 | 低 |
| V22 名前検索の部分一致・長さの上限(M03) | 0007 | 中 |
| V23 totalCount の型(M03) | 0006 | 低 |
| P01〜P04 Portal | 0002 / 0010 | M05 以降(P04 は早めに) |
