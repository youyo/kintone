# ADR 0010: 利用者の識別子に Cloudflare Access の email を必須で使う(`/authorize` と `/callback` を Access で保護する)

- 状態: 採用(M01 で実装。ユーザー決定 2026-11-05)
- 日付: 2026-11-05
- 置き換える: ADR 0002 の「Worker 内に Access 連携を持たない」(決定 2)、ADR 0004 の「利用者の識別子: 認可ごとのランダム UUID」(決定 4)
- 関連: ADR 0001、ADR 0002、ADR 0004、ADR 0005、ADR 0007、logbutler ADR 0009(Access 連携の削除)

## 状況

- kintone には、OAuth のアクセストークンから「トークンの持ち主」を返す口が公式の REST 一覧にない。User API(`/v1/users.json`)の認証方式にも OAuth の記載がない(ADR 0004 の表。出典: 公式 https://cybozu.dev/ja/kintone/docs/rest-api/ 、https://cybozu.dev/ja/common/docs/user-api/overview/authentication/ )。実機で OAuth トークンを使った試行は未実施(V14 は文書上の確認だけ)。
- そのため草案(ADR 0004)は `userId` を認可ごとの `crypto.randomUUID()` にしていた。その欠点:
  - 同じ人が再認可すると別の利用者になり、旧 grant が残る(ライブラリの「同じ user+client の旧 grant を失効する」既定動作が働かない)
  - 同じ人の grant 数を数えられず、リフレッシュトークンの 10 個上限(公式)の消費を Worker 側で扱えない
  - ログ・KV から「誰の grant か」を辿れない
- logbutler は M01 で `/authorize` と `/callback` を Cloudflare Access(self-hosted アプリ)で保護し、Worker でも `Cf-Access-Jwt-Assertion` を検証していたが、M06 で削除した(logbutler ADR 0009)。削除の理由は「Access Managed OAuth は自前の OAuth を持つサーバーには使わない、と公式が明記している」ことだった。**self-hosted アプリでブラウザが通るパスだけを守る構成は、その理由とは別の話**である。

## 決定

### 1. 利用者の識別子は Access の email。必須で、フォールバックを作らない
- `/authorize`(GET・POST)と `/callback` を、Cloudflare Access の **self-hosted アプリ**で保護する。保護するのはブラウザで通るこの 2 パスだけ。`/mcp` `/token` `/register` `/.well-known/*` `/healthz` は保護しない(MCP クライアントはブラウザでなく、Worker 発行のトークンで守る)。
- **Access の Managed OAuth は使わない**(self-hosted アプリの通常のポリシーだけを使う)。
- Worker は `Cf-Access-Jwt-Assertion` を検証して email を取り出す。検証できない・email がない場合は認可を進めない。**ランダム UUID などへのフォールバックは作らない**。

### 2. JWT の検証(Worker 側。多層防御として Access の前段設定に頼らない)
実装は logbutler の git 履歴(`git -C <logbutler> show 8dac72c^:src/auth/access-jwt.ts`)を移植する。変えるのは呼び出し側(email から `userId` を作る)だけ。
- JWKS: `https://<ACCESS_TEAM_DOMAIN>/cdn-cgi/access/certs`。isolate 内で 5 分キャッシュ。未知の `kid` は鍵ローテーションとみなして取り直すが、取得から 60 秒未満なら取り直さない(不正な JWT の連投で Access への通信を増やさない)。取得には 5 秒のタイムアウト(`AbortSignal.timeout`)を付け、超えたら `jwks_unavailable`(503)。実行中の取得 Promise は isolate 内で共有し、同時のリクエストは同じ取得を待つ(失敗したら共有を消し、次のリクエストで取り直す)
- 検証: 署名(RS256)、`iss` = `https://<ACCESS_TEAM_DOMAIN>`、`aud` に `ACCESS_AUD` を含む(`aud` は配列。出典: awsbridge `docs/specs/architecture.md` の観測)、`exp` 必須、`email` が空でない文字列、`sub` あり
- 失敗時の応答(理由は応答に出さない。ログには理由の種別だけ。JWT・email は出さない):
  - ヘッダーなし・検証失敗 → 403
  - JWKS を取得できない → 503(fail closed。logbutler は全て 403 だったが、運用側の障害と利用者側の拒否を分けるため変える)
- `/authorize` の POST でも検証する(GET の同意画面だけを守らない)。

### 3. 認可の流れでの email の扱い
1. `POST /authorize`: JWT を検証し、`beginUpstream` の `data` に `{ verifier, accessEmail }` を載せる(`data` はライブラリが暗号化して KV に置く)。
2. `GET /callback`: `finishUpstream` で state・束縛 cookie を検証したあと、JWT を検証し、**`data.accessEmail` と同じ email か確かめる**(正規化後に比較)。不一致なら 403 でコード交換をしない(Access の別ユーザーが同じブラウザで state を引き継いだ場合を弾く。logbutler が当時やっていた確認)。
3. `completeAuthorization({ userId })`:
   - `userId = base64url(SHA-256(UTF-8(email を trim して小文字にしたもの)))`(43 文字、`:` を含まない)。
   - 生の email でなくハッシュにする理由: workers-oauth-provider は `userId` に `:` を含められず(`node_modules/@cloudflare/workers-oauth-provider` 1.2.3 の `oauth-provider.d.ts` の `CompleteAuthorizationOptions.userId`)、`userId` は MCP トークン(`{userId}:{grantId}:{secret}`)と KV のキーに入る。email をそのまま(エンコードして)入れると、クライアントに渡るトークンと KV のキーに email が平文で載る。ハッシュなら載らず、長さも一定。
   - email は `props` にも `metadata` にも保存しない(`metadata` は暗号化されない)。ログにも出さない。出す識別子が必要なら `userId` の先頭 8 文字まで。
4. 同意画面に「Access のログイン: `<email>`」を表示する(エスケープする。画面に出すだけで保存しない)。さらに「次の kintone のログインでは、同じ本人のアカウントを選ぶこと」を一文添える(限界の緩和策。強制ではない)。

### 4. 同じ email での再認可は、古い grant を失効させて置き換える
- `completeAuthorization` の `revokeExistingGrants: true`(ライブラリ 1.2.3 の既定。**明示して書く**)を使う。新しい grant の保存後に、**同じ `userId` + 同じ MCP クライアント**(+ 同じ resource)の既存 grant を失効させる(出典: 同ライブラリの `docs/authorization-server.md` と `CompleteAuthorizationOptions.revokeExistingGrants` の説明)。
- 別の MCP クライアント(Portal と Claude Desktop など)の grant は失効しない。同じ人が複数のクライアントを同時に使える。DCR で毎回別のクライアントとして登録し直すクライアントの旧 grant も失効しない(このケースは放置する)。
- 狙い: (a) 同じ人・同じクライアントの grant が積み上がらない、(b) kintone のリフレッシュトークン上限(1 クライアント・1 ユーザーあたり 10 個。公式)の対策。
- **(b) の効果には条件がある**: Worker で grant を失効させても、kintone 側のリフレッシュトークンは取り消されない(kintone に取り消しの口があるかは未確認。V21)。したがって kintone の保持数は減らない。効くのは、**上限超過時に kintone が古いトークンから失効させる場合(V06 で確認)**だけで、そのとき失効するのは Worker が既に捨てた古い grant になり、生きている grant が巻き添えにならない。上限超過で新規が拒否される場合は、この置き換えでは防げない。
- 失効のタイミング: 新しい grant の保存後(ライブラリの `docs/authorization-server.md`)。失効が途中で失敗したときの挙動は未確認。M01 の E2E で通常ケース(旧 grant の更新が `invalid_grant` になり、新 grant が使える。別 email・別クライアントの grant は影響を受けない)を固定する。

### 5. 設定
- 追加: `ACCESS_TEAM_DOMAIN`(var。例 `example-team.cloudflareaccess.com`)、`ACCESS_AUD`(var。Access アプリの AUD タグ)。**どちらも必須**。
- `checkConfig`(仕様書 §9)の必須項目。欠け・不正なら `/healthz` 以外は 500 `{"error":"server_misconfigured"}`、`/healthz` は 503 + 項目名。値は出さない。
  - `ACCESS_TEAM_DOMAIN`: 欠け = 未定義・空・空白のみ。不正 = プレースホルダが残る、ホスト名の形式でない(スキーム・パス・空白を含む)
  - `ACCESS_AUD`: 欠け = 同上。不正 = プレースホルダが残る(形式は検査しない。公式の形式を確認していないため)
- テスト用の束縛はダミー値(`ACCESS_TEAM_DOMAIN=kintone-mcp-test.cloudflareaccess.com`、`ACCESS_AUD=test-access-aud`)。

## logbutler と判断が違う理由

1. logbutler が Access 連携を外した理由(Managed OAuth を自前 OAuth のサーバーに使わない)は、本 ADR には当てはまらない。本 ADR は Managed OAuth を使わず、ブラウザの 2 パスを self-hosted アプリで守るだけ。
2. logbutler は Backlog の API で利用者を識別できる(ADR 0004 で比較した `getMyself()`)。そのため Access は「誰が使えるか」の制限にしか使えず、外側の Portal に任せられた。kintone には持ち主を返す口が見つからず、Access の email が**識別子の唯一の供給源**になる。Access は制限でなく機能の一部になるので、Worker 内から外せない。
3. 副次的な利点: `/authorize` を通るには Access のポリシー(許可する email・IdP グループ)を通る必要があるので、ADR 0002 が弱点としていた「Worker の URL へ直接つなげば Portal のポリシーを回避できる」が、認可の入り口では成立しなくなる(`/mcp` はトークンを持っていれば到達できる点は変わらない)。

## 限界(明記する)

- **email が示すのは「Access にログインした人」であって、「kintone で認可したアカウント」ではない。** その人が kintone の認可画面で別のアカウントでログインして同意しても、Worker は気づけない(kintone に持ち主を返す口がないため)。たとえば Access の email が a@example.com で、kintone では b さんのアカウントを使うことができてしまう。kintone 側の「利用者の設定」(OAuth クライアントを使える利用者の指定)と、同意画面の注意書きが緩和策になるが、強制はできない。
- そのため、**キャッシュキー・冪等キー・stale 印は grant 単位のまま**にする(email 単位=`userId` 単位にしない)。同じ email の 2 つの grant が別の kintone アカウントを指している可能性を排除できず、アプリの見え方(名前解決のキャッシュ)や冪等の判定を共有すると、他アカウントのデータが見える方向の事故になりうる。キーは `<userId>:<grantId>` を含める(ADR 0007)。`userId` を使うのは、(a) 旧 grant の置き換え、(b) 診断ログの識別子だけ。
- 同じ人が 2 つの kintone アカウントを同じクライアントで使い分けると、あとの認可が前の grant を置き換える。
- Access の JWT は長寿命になりうる(awsbridge の観測で `exp - iat` が約 30 日。出典: awsbridge `docs/specs/architecture.md`)。ただし本 ADR で JWT を使うのは `/authorize` と `/callback` の認可の瞬間だけで、その後の `/mcp` は Worker 発行のトークンで守られる。Access から外した利用者の既存 grant は残る(失効は手動。runbook に書く。M05)。

## 却下した案

- **認可ごとのランダム UUID(草案)**: 上の欠点。email を必須にしたため不要。フォールバックとしても残さない(Access が効いていない状態で認可が通ってしまうため)。
- **Access の email をそのまま(エンコードして)`userId` にする**: トークンと KV のキーに email が載る。
- **email を `props` に持つ**: 使い道がない(ツールは email を使わない)。使い道ができたら別 ADR。
- **Access Managed OAuth を使う**: 公式が自前 OAuth のサーバーには使わないとしている(logbutler ADR 0009)。
- **リフレッシュトークンのハッシュを `userId` にする**: ADR 0004 のとおり、更新で変わりうる。
- **User API(`/v1/users.json`)や、レコードを作って作成者を読む方法**: ADR 0004 のとおり。

## 帰結

- 運用者の必須作業が増える: Access の self-hosted アプリを作る(パス `/authorize` と `/callback` だけ。ポリシーは許可する email・グループ)、`ACCESS_TEAM_DOMAIN` と `ACCESS_AUD` を控える(ロードマップ U4)。`ACCESS_AUD` を取り違えると全員が 403 になる。
- Access が前段にあるためブラウザは Access のログインを一度通る。MCP Portal 経由の接続でも、`/authorize` を開くブラウザには Access のセッションが必要(P04)。
- 偽の JWT を `/authorize` に送っても 403。Access の設定を誤って保護を外しても、Worker 側の検証で 403(フェイルクローズ)。
- テスト: JWKS は fetch モック、テスト用の鍵で署名した JWT。偽署名・`aud` 違い・期限切れ・`exp` なし・`iss` 違い・email なし・`/callback` での email 不一致を拒否する(M01 詳細計画)。
- `/register`(DCR)は匿名のまま。レート制限は前段の WAF で行う(M05 の runbook)。

## 未検証の前提(V17〜V21、P04。`docs/manual-verification.md`)

- JWT の `email` claim が IdP によらず入るか(awsbridge は開発環境(Google)で観測し「IdP によらず必ず入る」と記しているが、他の IdP は未確認)(V17)
- self-hosted アプリのパス指定で `/authorize` と `/callback` だけを保護でき、他のパスが素通りか(V18)
- `/callback` へのリダイレクトで `Cf-Access-Jwt-Assertion` が付くか。kintone のログイン中に Access のセッションが切れたときの復帰(V19)
- Cloudflare MCP Portal 経由の MCP クライアントでブラウザが `/authorize` へ遷移したとき、Access の cookie と JWT が付くか(P04)
- `workers.dev` のホストで Access の self-hosted アプリを使えるか(カスタムドメインが要るか)(V20)
- kintone のリフレッシュトークンを取り消す口があるか(V21)

## M01 の実装で確定・追加した点

- JWT の検証は RS256 のみ(`alg` の混同は拒否)、`exp` 必須(`requiredClaims`)、`email` は空白だけでも拒否、`sub` 必須。JWKS が JSON でない・`keys` が配列でない・404/5xx・fetch 例外・タイムアウトはすべて `jwks_unavailable`(503)。
- 未知の `kid` で JWKS を取り直すのは、前回の取得から 60 秒以上たっているときだけ(logbutler の M01 実装は時間条件なしで取り直していた。本仕様に合わせて変えた)。キャッシュが空の最初の検証で未知の `kid` でも、取得は 1 回だけ。
- 失敗のログは `{"event":"access_jwt_rejected","reason":"missing|invalid|jwks_unavailable"}` と、email 不一致の `access_email_mismatch` だけ(JWT・email は出さない)。応答の本文は `Forbidden` / `Service Unavailable` の固定文。
- `/callback` の email 比較は trim + 小文字化したもの同士。不一致のときは `finishUpstream` が state を使い切っているため、正しい email で送り直しても進まない(E2E で固定)。
- E2E で、email が MCP トークンにも KV のキー・値にも現れないこと、kintone のトークンが KV に平文で残らないことを固定した(`beginUpstream` の `data` はライブラリが暗号化する)。

## セキュリティ強化: grant の最大寿命を 7 日(固定)にする

- 問題: `refreshTokenTTL` を設定していなかったため、ライブラリの既定の 30 日が使われていた。Access から外した利用者も、最大 30 日は `/mcp` を使えた(JWT を使うのは認可の瞬間だけなので、外しても既存 grant は効く)。
- 決定: `refreshTokenTTL: 604800`(7 日)を明示する(`MCP_REFRESH_TOKEN_TTL_SECONDS`、`src/auth/token-exchange.ts`)。grant の `expiresAt` は認可時に `now + refreshTokenTTL` で決まり、リフレッシュのたびに `Date.now()` と比べて、残りが 60 秒未満なら `invalid_grant`(`oauth-provider.js` のリフレッシュ処理)。`refreshTokenIdleTTL` は**設定しない**(設定すると使うたびに期限が延び、期限で切る目的を果たせない)。`tokenExchangeCallback` も `refreshTokenTTL` / `refreshTokenIdleTTL` を返さない(返すと grant ごとの寿命が変わる)。
- 7 日を過ぎた利用者は再認可になり、そこで Access の判定を通る(外した人は通れない)。すぐ止めたい場合は従来どおり手動で失効させる(runbook)。
- 実装上の注意: `refreshTokenTTL: undefined` を明示的に渡すとライブラリは「無期限」と解釈する。整数の定数を直接渡す。`clientRegistrationTTL` も同じ(30 日 = `MCP_CLIENT_REGISTRATION_TTL_SECONDS`。ライブラリの既定は 90 日)。DCR のクライアント記録には `registrationExpiresAt` と KV の `expirationTtl` が付く。
- 影響: 利用者は最長 7 日ごとにブラウザで再認可する(Access のセッションが生きていれば同意の確認だけ)。調整は定数の変更(下流のポリシーで変えるなら runbook の手順)。テスト: `test/e2e/grant-lifetime.test.ts`(7 日の前後・使い続けても延びないこと・DCR の期限)、`test/unit/token-exchange.test.ts`。
