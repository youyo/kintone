# ADR 0004: kintone OAuth の仕様適用(クライアント認証・PKCE・scope・利用者の識別子)

- 状態: 採用(M01 で実装)。決定 3(scope)・決定 4(利用者の識別子)はユーザー決定により改訂済み(2026-11-05)
- 日付: 2026-11-05
- 関連: ADR 0001、ADR 0005、ADR 0010、logbutler `src/auth/backlog-oauth.ts`

## 状況

logbutler は Backlog OAuth(client_secret をフォームで送る、`/users/myself` で利用者 ID を得る)を前提にしている。kintone は仕様が異なる。確認できた事実:

| 事実 | 出典 |
|---|---|
| クライアント認証は HTTP Basic(POST body 方式の記載なし) | Go `internal/auth/oauth/token.go:176`、公式 https://cybozu.dev/ja/common/docs/oauth-client/add-client/ |
| 認可コード 10 分、アクセストークン 1 時間、リフレッシュトークン無期限・1 ユーザー 10 個まで | 公式(同上) |
| PKCE の記載なし | 公式(同上)。Go は常に `code_challenge`(S256)を送って本番運用(`handler.go:282`)。`KINTONE_OAUTH_PKCE_DISABLE` は M09 の計画書(`plans/kintone-m09-oauth-auth.md`)にあるが、コードには存在しない(`grep -rn PKCE_DISABLE --include='*.go'` が 0 件) |
| scope は 6 種。records の GET は `k:app_record:read`、POST/PUT/DELETE は `k:app_record:write`、apps.json・app.json・form/fields.json の GET は `k:app_settings:read` | 公式 https://cybozu.dev/ja/common/docs/oauth-client/scope-kintone/ (スコープとAPIの対応は API 名からの読み取り) |
| REST API の一覧(レコード・ファイル・アプリ・スペース・プラグイン・API 情報)に、実行ユーザー自身を返す API はない。ユーザー取得は User API(`/v1/users.json`)だが、User API の認証方式は API トークン・パスワード・セッションで **OAuth の記載がない** | 公式 https://cybozu.dev/ja/kintone/docs/rest-api/ 、https://cybozu.dev/ja/common/docs/user-api/overview/authentication/ |
| `ログインユーザーの情報を取得する` は JavaScript API(`kintone.getLoginUser()`)でカスタマイズ内でしか使えない | 公式 https://cybozu.dev/ja/kintone/docs/js-api/kintone/get-login-user/ (REST 一覧のページ経由) |

## 決定

### 1. クライアント認証は HTTP Basic
コード交換・更新とも `Authorization: Basic base64(client_id:client_secret)` + フォーム(`grant_type` ほか)。logbutler の `requestTokens` を Basic 版に書き換える。

### 2. PKCE(S256)を送る。ただし逃げ道を残す
- `POST /authorize` で `code_verifier` を生成し、`beginUpstream({ data: { verifier } })` で保存、`/callback` の `finishUpstream` で取り出してコード交換に付ける(`beginUpstream` の `data` は暗号化されて KV に置かれ、`/callback` で返る。`node_modules/@cloudflare/workers-oauth-provider/dist/oauth-provider.d.ts` の `beginUpstream` / `ResumedUpstream` と `docs/upstream-sign-in.md`)。
- **kintone が PKCE を検証しているかは未検証**(V04)。Go のリリース実績から「パラメーターは拒否されない」ことは確からしいが、verifier の照合が行われているかは分からない。照合されないなら PKCE は追加の防御にならず、害もない。
- 逃げ道: 任意の環境変数 `KINTONE_OAUTH_PKCE=off` で `code_challenge` と `code_verifier` を送らない(kintone が拒否した場合用)。既定は `s256`。これは Go 版の計画にあって実装されなかった `KINTONE_OAUTH_PKCE_DISABLE` の代替。logbutler ADR 0008「フラグは環境変数にしない」との関係: あれは利用者の好みで変わる設定(`toolsets` / `read_only` / `timezone`)をクライアント側で制御する方針。この変数は上流 IdP との互換性のためのデプロイ単位の設定で、クライアントが決めるものではないので環境変数が適切。

### 3. scope は Go 版と同じ 6 つ
`k:app_record:read k:app_record:write k:app_settings:read k:app_settings:write k:file:read k:file:write`(スペース区切り。Go 版の `handler.go:319-327` と同じ)。ユーザー決定(2026-11-05)。

今のツール(6 個)が使うのは `k:app_record:read`(`records_query`)、`k:app_record:write`(`record_create` / `record_update` / `record_delete`)、`k:app_settings:read`(`apps_search` / `app_describe` と名前解決)の 3 つ。**残り 3 つ(`k:app_settings:write` `k:file:read` `k:file:write`)は今のツールでは使わないが、要求する。**

帰結:
- kintone の同意画面(認可画面)に、使わない権限(アプリ設定の変更、ファイルの読み書き)も並ぶ。
- Worker が持つ kintone のトークン(grant の `props`)は、今のツールが使う範囲より広い権限を持つ。props はライブラリが暗号化して KV に置く(仕様書 §2)が、トークンが漏れた場合の被害は 3 つの場合より大きい。「props 以外に保存しない・ログに出さない」規約の重みが増す。
- 利点: ファイル系・アプリ設定の変更系のツールを M05 以降に足すとき、既存の grant の再認可が要らない。
- 認可 URL の `scope` に 6 つを載せるテストを M01 の E2E に置く(M01 計画の完了条件 2)。

### 4. 利用者の識別子: Cloudflare Access の email(必須。ADR 0010)
- kintone には OAuth トークンで「自分は誰か」を返す REST API が見つからない(上の表)。logbutler のように `getMyself()` の ID を `userId` にできない。
- そこで、`/authorize` と `/callback` を Cloudflare Access の self-hosted アプリで保護し、Worker が `Cf-Access-Jwt-Assertion` を検証して取り出した **email** から `userId` を作る。`userId = base64url(SHA-256(小文字の email))`(`:` を含まない。ライブラリは `userId` 中の `:` を内部の区切りに使うため)。詳細・検証手順・logbutler との違いは **ADR 0010**。
- 草案は認可ごとの `crypto.randomUUID()` だったが、同じ人の再認可を別人として扱う・旧 grant が積み上がる等の欠点があり、ユーザー決定で email 必須に変えた。UUID へのフォールバックは作らない。
- **限界**: email が示すのは「Access にログインした人」で、その人が kintone の認可画面で別のアカウントを選んでも Worker は気づけない。そのため、キャッシュ・冪等キー・stale 印は grant 単位(`<userId>:<grantId>`)のままにする(ADR 0007、ADR 0010)。
- ログに出す識別子は `userId` の先頭 8 文字まで。email は出さない。
- 認可の最後に **疎通確認** `GET /k/v1/apps.json?limit=1` を行う。トークンが使えること、利用者に API 権限があること(利用者の許可・アプリのアクセス権)、IP 制限に阻まれないことを認可時点で確かめる。

### 5. リダイレクト URI は `<PUBLIC_BASE_URL>/callback`
Go 版(`/oauth/kintone/callback`)と異なる。cybozu.com 共通管理で OAuth クライアントを(新しいリダイレクト URI で)登録し直す。Go 版のクライアントを流用する場合は、リダイレクト URI を変更するか追加する必要がある(複数登録できるかは公式に記載なし。クライアントは 20 個まで登録できる=公式)。

## 却下した案

- **User API(`/v1/users.json`)で利用者を特定する**: OAuth での利用が公式ページで確認できず、ログイン名(検索キー)も分からない。
- **リフレッシュトークンのハッシュを `userId` にする**: 更新でトークンが変わる(ローテーションする場合)と別人になる。ローテーションの有無が未検証(V01)で、安定しない。
- **レコードを 1 件作って作成者を読む**: 副作用があり、権限もない可能性がある。
- **PKCE を送らない**: 追加の防御の可能性を捨てる。Go で実績がある。逃げ道を残せば足りる。
- **scope を今のツールに必要な 3 つに絞る(草案)**: ユーザー決定で Go 版と同じ 6 つにした(決定 3)。
- **認可ごとのランダム UUID を `userId` にする(草案)**: ADR 0010 で置き換えた。

## 未検証の前提

- PKCE の検証有無・拒否の有無(V04)
- redirect_uri が https 必須か、`http://localhost` を許すか(V05)。ローカル開発で認可フローを実機に通せない可能性
- 認可コード交換直後の `expires_in`(3600 の想定。V02)、更新のローテーション(V01)
- REST API にも User API にも、OAuth トークンの持ち主を返す口が本当にないか: 公式の REST 一覧では確認済み(V14)。実機で OAuth トークンを使って `/v1/users.json` ほかを叩く追加確認は任意
- Access の JWT・email に関する前提(ADR 0010 の V17〜V21、P04)

## M01 の実装で確定・追加した点

- `src/auth/kintone-oauth.ts` のエラー分類は 5 種: `invalid_grant` / `invalid_client`(`invalid_client`・`unauthorized_client`。運用者の設定ミス)/ `temporarily_unavailable`(5xx・429・通信失敗)/ `rejected`(上記以外の 4xx。`invalid_request` や 403 など、再試行で直らない拒否。PKCE を拒否された場合もここに入る想定で、その場合は `KINTONE_OAUTH_PKCE=off`)/ `invalid_response`(成功応答の形が想定と違う)。エラー本文は `error` コードだけを読み、`error_description` などは message に載せない(トークンを含みうるため)。
- コード交換のフォームは `grant_type` `code` `redirect_uri` `code_verifier` の 4 つ(`client_id` も入れない。クライアント認証は Basic ヘッダーだけ)。`KINTONE_OAUTH_PKCE=off` では `code_verifier` を送らず、`beginUpstream` の `data` にも `verifier` を持たない。
- `/callback` の失敗(コード交換・疎通確認): 通信失敗・5xx・429 は `temporarily_unavailable`、それ以外(`invalid_grant`・疎通確認の 401/403 など)は `server_error` でクライアントへ戻す。ログは失敗の種別だけ。
- 認可の疎通確認(`GET /k/v1/apps.json?limit=1`)は E2E でコード交換のあとに 1 回呼ばれることを固定した。
