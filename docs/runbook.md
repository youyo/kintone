# デプロイと運用の手順書

kintone MCP(kintone 用 Remote MCP サーバー。Cloudflare Workers)を、自分の Cloudflare アカウントと cybozu.com で動かす手順。**下流の運用者**(このリポジトリ = upstream を元に、自分のリポジトリを作って運用する人)の視点で、**上から順にたどれる**ように書く。
設計の理由は [ADR 0012](adr/0012-upstream-downstream-deploy.md)(デプロイ設定)と [ADR 0010](adr/0010-access-email-as-user-identity.md)(Access と利用者の識別)。
**upstream の作業では実デプロイをしていない**。そのため、実機で確かめていない手順には **【未検証】** と V 番号を続けて付け(例: 【未検証】V18)、[docs/manual-verification.md](manual-verification.md) の同じ番号の項目に結んである。印のある手順は、書いてあるとおりに進まないことがありうる。実際にやってみた結果で、この手順書と V 番号の欄を直してほしい。

## 全体像と順序

- upstream(このリポジトリ)の追跡ファイルは、下流では**一切編集しない**。だから `git pull` が競合しない。
- アカウント固有の値(Worker 名・KV id・ドメイン・kintone のホスト・Access の値・client id / secret)は、下流の GitHub Actions の **Variables / Secrets** に置く。コミットしない。
- デプロイのたびに、`wrangler.jsonc`(upstream の共有設定。ダミー値)と Variables から、デプロイ用の設定 `wrangler.deploy.jsonc`(gitignore 済み)を生成し、`wrangler deploy -c wrangler.deploy.jsonc` する(`mise run deploy:render`)。

**順序が大事**: 公開 URL が決まらないと OAuth クライアントも Access も作れず、Access の AUD が分からないと Worker は動かない。次の順で進める。

0. 設定値を決める(公開ドメイン、Worker 名)。下流リポジトリを作る(手順 0)
1. cybozu.com で OAuth クライアントを登録する(手順 1。リダイレクト URI は公開 URL から決まる)
2. Cloudflare で Access の self-hosted アプリを作る(手順 2。AUD とチームドメインを控える)
3. KV namespace を作る(手順 3)
4. Cloudflare の API トークンを作る(手順 4)
5. GitHub に Variables / Secrets を登録する(手順 5)
6. デプロイして動作確認する(手順 6)
7. 必要なら WAF / Rate Limiting(手順 7)、MCP Portal への登録(手順 8)

前提: [mise](https://mise.jdx.dev/) 導入済み(手元で操作する場合)、Cloudflare アカウント(Zero Trust = Access が使えること)、cybozu.com の共通管理者(OAuth クライアントの登録に必要)、カスタムドメインを使うなら、その DNS ゾーンが同じ Cloudflare アカウントにあること。

以降の表記: `<domain>` = この Worker の公開ホスト名(例: `kintone-mcp.example.com`。カスタムドメインを使わないなら `<worker>.<account>.workers.dev`)、`<subdomain>` = cybozu.com のサブドメイン(ホストは `<subdomain>.cybozu.com`)、`<team>` = Access のチーム名(チームドメインは `<team>.cloudflareaccess.com`)。

## 設定値の対応表

| 設定 | 置き場所 | 値の例 | 備考 |
|---|---|---|---|
| Worker 名 | Variable `KINTONE_MCP_WORKER_NAME` | `kintone-mcp` | 小文字英数字とハイフン(63 文字以内)。**空なら deploy.yml はジョブごと飛ばされる** |
| KV namespace `OAUTH_KV` の id | Variable `KINTONE_MCP_KV_NAMESPACE_ID` | 32 桁の 16 進 | 手順 3 で 1 回だけ作る |
| `KINTONE_HOST` | Variable `KINTONE_MCP_HOST` | `<subdomain>.cybozu.com` | スキーム・スラッシュ・ポートなし。1 デプロイ = 1 ホスト(ADR 0003) |
| `PUBLIC_BASE_URL` | Variable `KINTONE_MCP_PUBLIC_BASE_URL` | `https://<domain>` | https のオリジン。末尾スラッシュ・パスなし。issuer とトークンの audience の元 |
| `ACCESS_TEAM_DOMAIN` | Variable `KINTONE_MCP_ACCESS_TEAM_DOMAIN` | `<team>.cloudflareaccess.com` | 手順 2 で控える。スキーム・パスなし |
| `ACCESS_AUD` | Variable `KINTONE_MCP_ACCESS_AUD` | Access アプリの AUD タグ | 手順 2 で控える。取り違えると全員が 403 |
| カスタムドメイン(任意) | Variable `KINTONE_MCP_CUSTOM_DOMAIN` | `<domain>` | あれば `custom_domain` の route になり workers.dev は無効。なければ workers.dev で公開 |
| `KINTONE_OAUTH_PKCE`(任意) | Variable `KINTONE_MCP_OAUTH_PKCE` | `s256`(既定)/ `off` | kintone が PKCE パラメーターを拒否したときの逃げ道(V04) |
| `KINTONE_CLIENT_ID` | Secret `KINTONE_CLIENT_ID` | kintone が発行 | Worker の secret としてデプロイ時に投入される |
| `KINTONE_CLIENT_SECRET` | Secret `KINTONE_CLIENT_SECRET` | kintone が発行 | 同上 |
| Cloudflare API トークン | Secret `CLOUDFLARE_API_TOKEN` | 手順 4 | |
| Cloudflare アカウント ID | Secret `CLOUDFLARE_ACCOUNT_ID` | 32 桁の 16 進 | ダッシュボードで確認 |
| kintone のリダイレクト URI | cybozu.com の OAuth クライアント設定 | `https://<domain>/callback` | `PUBLIC_BASE_URL` + `/callback` と完全一致 |
| MCP の接続 URL | MCP クライアント / Portal | `https://<domain>/mcp` | 必要なら `?toolsets=app,record` や `?read_only=true` |

`KINTONE_MCP_*` の値に `<...>` のプレースホルダが残っている、形式が違う、必須値が空、のいずれかだと、生成(`mise run deploy:render`)が項目名と理由を挙げて失敗する(**値は表示されない**)。起動した Worker 側の検査(`checkConfig`)も同じ項目を見ていて、欠けていれば `/healthz` が 503 になる(仕様書 §9)。

## 0. 下流リポジトリを作る

どちらでもよい。いずれも、upstream の追跡ファイルは編集しない。

- **fork**: GitHub の Fork で作る。**fork 先では GitHub Actions が既定で無効**なので、Actions タブで有効化する(有効化しないと `deploy.yml` が動かない)。
- **ミラー**: 空のリポジトリを作り、upstream を remote に足して push する。

  ```sh
  git clone <upstream の URL> kintone-mcp && cd kintone-mcp
  git remote rename origin upstream
  git remote add origin <自分のリポジトリの URL>
  git push -u origin main
  ```

**upstream の取り込み**: 下流の `main` で `git pull upstream main`(fork なら `git remote add upstream <upstream の URL>` を先に)して push する。追跡ファイルを編集していないので競合しない。push すると `deploy.yml` が走り、新しい設定でデプロイされる。

**Variable `KINTONE_MCP_WORKER_NAME` を登録するまで `deploy.yml` は何もしない**(upstream でデプロイが走らないための仕組み)。登録は手順 5。

## 1. cybozu.com 共通管理: OAuth クライアントの登録

kintone の REST API を、利用者ごとの OAuth で呼ぶためのクライアントを、cybozu.com 共通管理で登録する。公式: <https://cybozu.dev/ja/common/docs/oauth-client/add-client/>。画面の項目名は公式の記述から書いたもので、実際の画面での確認はまだ(【未検証】V29)。

1. cybozu.com 共通管理(`https://<subdomain>.cybozu.com/admin/` の「システム管理」)の OAuth クライアントの設定を開き、クライアントを追加する(【未検証】V29。メニューの名前・場所は画面で確認する)。
2. 次を入力して登録する。
   - クライアント名: 任意(kintone の認可画面に出る。例 `kintone MCP`)
   - リダイレクトエンドポイント(リダイレクト URI): **`<PUBLIC_BASE_URL>/callback`**(例 `https://<domain>/callback`)。**`KINTONE_MCP_PUBLIC_BASE_URL` + `/callback` と 1 文字も違わない**こと(末尾スラッシュ・大文字小文字・http / https も含めて完全一致)。違うと kintone が認可を拒否する(切り分けの節の `redirect_uri`)
3. 登録後に表示される **クライアント ID** と **クライアントシークレット** を控える(シークレットはあとで再表示できない可能性がある。【未検証】V29)。手順 5 で Secrets に入れる。
4. **scope は 6 つ**(Worker が認可 URL に載せる。クライアント側で選ぶ画面がある場合は、次の 6 つに許可する。【未検証】V29)。
   - `k:app_record:read` `k:app_record:write`(レコードの読み書き)
   - `k:app_settings:read` `k:app_settings:write`(アプリ情報の読み取り・アプリ設定)
   - `k:file:read` `k:file:write`(ファイル)

   今のツール(6 個)が使うのは `k:app_record:read` / `k:app_record:write` / `k:app_settings:read` の 3 つだけだが、将来のツールのために 6 つを要求する(ADR 0004)。kintone の同意画面には、使わない権限も並ぶ。
5. **STEP2: 利用者を許可する**。同じ OAuth クライアントの **「利用者の設定」(STEP2)で、使う kintone ユーザー(組織・グループでも可)を指定する**。指定のない利用者は認可できない(公式 add-client ページ。【未検証】V03・V29)。利用者を追加・削除するときもここを変える。

**注意**:
- 1 デプロイ = 1 つの cybozu.com ホスト(ADR 0003)。複数のサブドメインを使うなら、サブドメインごとに別の Worker(別の下流リポジトリまたは別の Variables)・別の KV・別のクライアントを用意する。
- 登録できるクライアントは 20 個まで(公式)。検証環境と本番を分けるなら、公開 URL ごとにクライアントを別々に登録する。**1 つのクライアントに複数のリダイレクト URI を登録できるかは未確認**(【未検証】V29)。
- 認可の最後に Worker は `GET /k/v1/apps.json?limit=1` で疎通確認をする。ここで 401 / 403 になる利用者は、アプリの閲覧権限・「利用者の設定」・IP 制限(後述)を確認する。

### IP 制限の注意

**cybozu.com 共通管理の IP アドレス制限を使っている場合は、先に確認する**。OAuth クライアント(= この Worker)の IP を許可する必要があるが(公式 add-client ページ)、Workers の送信元 IP は固定でなく、許可リストに載せにくい。IP 制限下で使えるかは未確認で、**使えない可能性が高い**(【未検証】V10)。運用開始前に、検証環境で `mise run verify:oauth` を通して確かめる。認可時の疎通確認(`apps.json?limit=1`)で 401 / 403 になるなら、IP 制限を疑う。IP 制限を理由に使えない場合の回避策は用意していない(Access の IP 制限や WAF は Worker の前段の話で、cybozu.com 側の制限とは別)。

## 2. Cloudflare Access: self-hosted アプリの作成

利用者の識別子は **Cloudflare Access の email**(必須。ADR 0010)。ブラウザが通る **`/authorize` と `/callback` の 2 つだけ**を Access の self-hosted アプリで守り、Worker が `Cf-Access-Jwt-Assertion` を検証する。Access を設定しない(または `ACCESS_AUD` / `ACCESS_TEAM_DOMAIN` を入れない)Worker は動かない(`/healthz` 以外は 500、`/healthz` は 503)。画面の項目名は公式ドキュメントの記述から書いたもので、実機での確認は V18 で行う(【未検証】V18)。

### 2-1. アプリを作る

1. Cloudflare ダッシュボードの Zero Trust(Access)で、アプリケーションを追加する。種類は **Self-hosted**(【未検証】V18)。
2. アプリケーションの宛先(destination / public hostname)は、**次の 2 つだけ**を登録する(【未検証】V18。パス指定で 2 つだけ守れるか、前方一致・サブパスの扱いは実機で確認する)。
   - `<domain>/authorize`
   - `<domain>/callback`
3. **保護しないパス**(宛先に含めない): `/mcp`・`/token`・`/register`・`/.well-known/*`・`/healthz`。MCP クライアントはブラウザではなく Access にログインできないので、これらを Access で守ると接続できなくなる。`/mcp` は Worker が発行したトークンで守られる。
4. **Access の Managed OAuth は有効にしない**(自前の OAuth 認可サーバーを持つサーバーには使わない。ADR 0010)。
5. **ポリシー**: アクション Allow、Include に利用を許す人(email、email のドメイン、IdP のグループ)を指定する。この Access のポリシーが「誰がこの Worker から kintone を認可できるか」の入口になる。IdP は、JWT に `email` claim が入るものを使う(入らないと Worker は認可を進められない。UUID などへのフォールバックはない。【未検証】V17)。
6. セッションの期間は、kintone のログインに時間がかかっても切れない長さにしておくと安全(切れたときに `/callback` へ正しく戻れるかは未確認。【未検証】V19)。

### 2-2. AUD とチームドメインを控える

- **AUD タグ**: 作成したアプリの Overview(概要)に表示される Application Audience (AUD) Tag(【未検証】V18)。→ Variable `KINTONE_MCP_ACCESS_AUD`
- **チームドメイン**: Zero Trust の設定(Settings)にある Team domain。`<team>.cloudflareaccess.com` の形(スキーム・パスなし)。→ Variable `KINTONE_MCP_ACCESS_TEAM_DOMAIN`

AUD を取り違えると全員が 403 になる。チームドメインが違うと、署名の検証用の鍵を取得できず 503 になる(切り分けの節)。

**`workers.dev` のホストで self-hosted アプリが使えるか(カスタムドメインが要るか)は未確認**(【未検証】V20)。使えない場合は、Cloudflare に載せたゾーンのカスタムドメインを `KINTONE_MCP_CUSTOM_DOMAIN` に入れる。

### 2-3. Access から外した利用者の grant を失効させる

Access のポリシーから外した(または退職した)利用者の**既存の grant は残る**。Access の JWT を使うのは `/authorize` と `/callback`(認可の瞬間)だけで、そのあとの `/mcp` は Worker が発行したトークンで動くため、ポリシーから外しても、すでに認可済みの接続はトークンが生きている限り使える(ADR 0010)。外した人の接続を止めるには、次の 3 つを行う。

1. **Worker の grant を削除する**(これでそのユーザーの MCP トークンは使えなくなり、リフレッシュもできない)。
   1. 利用者の `userId` を計算する(`userId` = 小文字にした email の SHA-256 を base64url にしたもの。43 文字)。

      ```sh
      printf %s '<email>' | tr '[:upper:]' '[:lower:]' | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '='
      ```

      (Worker の `userIdFromEmail` と同じ値になる。macOS・Linux の両方での確認は【未検証】V28)
   2. その利用者の grant とトークンを KV から一覧し、削除する。`wrangler.deploy.jsonc` は手元で `mise run deploy:render`(手順 5-1 の `mise.local.toml` の値が要る)で生成しておく。

      ```sh
      pnpm exec wrangler kv key list --binding OAUTH_KV -c wrangler.deploy.jsonc --remote --prefix "grant:<userId>:"
      pnpm exec wrangler kv key list --binding OAUTH_KV -c wrangler.deploy.jsonc --remote --prefix "token:<userId>:"
      # 一覧に出たキーを 1 つずつ削除する(キー名は一覧の出力から)
      pnpm exec wrangler kv key delete "<キー>" --binding OAUTH_KV -c wrangler.deploy.jsonc --remote
      ```

      (【未検証】V28。実際の KV での一覧・削除はまだ確認していない。削除は元に戻せない。先に一覧だけを実行して、対象が合っているか確かめる)

      `kcache:` `idem:` `kintone-stale:` で始まるキーは、期限(TTL)で消えるので残してよい。
2. **kintone の「利用者の設定」(STEP2)から外す**(手順 1 の 5)。Worker が持つ kintone のトークンは、grant を消せば使われなくなるが、kintone 側のリフレッシュトークンは取り消されない(取り消しの口があるかは未確認。【未検証】V21)。STEP2 から外せば、その利用者は kintone で認可し直せなくなる。
3. 同じ人が使っていた他の MCP クライアント(Portal など)の grant も、上の 1 の接頭辞の一覧に出る(クライアントごとに別の grant)。すべて消す。

全員を止めたいときは、cybozu.com の OAuth クライアントを削除するか、`KINTONE_CLIENT_SECRET` を変更する(以降の kintone のトークン更新はすべて失敗する)。

## 3. Cloudflare: KV namespace の作成(1 回だけ手動)

```sh
mise install && mise run install
pnpm exec wrangler login        # ブラウザで Cloudflare にログイン
pnpm exec wrangler kv namespace create kintone-mcp-oauth
```

出力された `id`(32 桁の 16 進)を、Variable `KINTONE_MCP_KV_NAMESPACE_ID` に登録する。`--update-config` は使わない(リポジトリのファイルを書き換えないため)。(この手順の実機での確認は【未検証】V27)

**KV namespace は作り直さない**。`OAUTH_KV` には、利用者ごとの grant(kintone のトークンを暗号化して含む)、MCP クライアントの DCR 登録、発行済みのトークンの記録が入っている。namespace を作り直す(id が変わる)と、これらがすべて消え、**全利用者が再認可**し、すべての MCP クライアントが登録し直しになる(Portal の登録も含む)。キャッシュ(`kcache:`・`idem:`・`kintone-stale:`)だけなら TTL で自然に消えるので、運用上 KV を消す理由はない。別の Worker に同じ namespace を使い回すのもやめる(grant が混ざる)。

## 4. Cloudflare API トークン

Cloudflare ダッシュボードの My Profile → API Tokens で、**最小権限**のカスタムトークンを作る。

| 権限 | レベル | 用途 |
|---|---|---|
| Workers Scripts : Edit | Account | Worker のアップロードと secrets の投入 |
| Workers KV Storage : Edit | Account | KV を wrangler が参照・操作する(デプロイ時の binding 検証)。対象を作成した namespace だけに絞れる場合は絞る |
| Zone : Read | Zone(対象ゾーンのみ) | カスタムドメインを使う場合だけ |
| DNS : Edit | Zone(対象ゾーンのみ) | カスタムドメインを使う場合だけ(DNS レコードと証明書の作成) |

Account Resources は自分のアカウントだけ、Zone Resources は対象ゾーンだけに限定する。**権限名と必要な組み合わせは公式ドキュメントで確認すること**(【未検証】V27。権限が不足してデプロイが失敗したら、wrangler のエラーに出る権限名を足す)。トークンの値は Secret `CLOUDFLARE_API_TOKEN` に、アカウント ID は `CLOUDFLARE_ACCOUNT_ID` に登録する。

## 5. GitHub に Variables / Secrets を登録する

下流リポジトリの Settings → Secrets and variables → Actions。

| 種別 | 名前 | 必須 |
|---|---|---|
| Variable | `KINTONE_MCP_WORKER_NAME` | 必須(空だと deploy.yml が動かない) |
| Variable | `KINTONE_MCP_KV_NAMESPACE_ID` | 必須 |
| Variable | `KINTONE_MCP_HOST` | 必須 |
| Variable | `KINTONE_MCP_PUBLIC_BASE_URL` | 必須 |
| Variable | `KINTONE_MCP_ACCESS_TEAM_DOMAIN` | 必須 |
| Variable | `KINTONE_MCP_ACCESS_AUD` | 必須 |
| Variable | `KINTONE_MCP_CUSTOM_DOMAIN` | 任意 |
| Variable | `KINTONE_MCP_OAUTH_PKCE` | 任意(`s256` / `off`) |
| Secret | `CLOUDFLARE_API_TOKEN` | 必須 |
| Secret | `CLOUDFLARE_ACCOUNT_ID` | 必須 |
| Secret | `KINTONE_CLIENT_ID` | 必須 |
| Secret | `KINTONE_CLIENT_SECRET` | 必須 |

登録後、`main` への push か、Actions の `Deploy` を手動実行(workflow_dispatch)でデプロイされる。流れは lint → test → 設定の生成(Variables から。`mise run deploy:render`)→ `wrangler deploy -c wrangler.deploy.jsonc --secrets-file <一時ファイル>`。
`KINTONE_CLIENT_ID` / `KINTONE_CLIENT_SECRET` は、ランナーの一時領域に自分だけが読める権限(0600)の JSON として書かれ、ジョブ終了時に消える(ログには出さない。秘密値はそのステップの環境変数だけで、ジョブ全体には置かない)。`--secrets-file` は追加・更新のみで、ほかの secrets を消さない(【未検証】V27)。
secrets を入れる前にデプロイされると、設定チェックにより `/healthz` が 503(`missing` に 2 つの secret)になる。これは想定どおり。

### 5-1. 予備の経路: 手元からの手動デプロイ

Actions が使えないときや、検証環境を手元から更新したいとき。値は `mise.local.toml`(gitignore 済み。コミットしない)の `[env]` に書く。

```toml
[env]
KINTONE_MCP_WORKER_NAME = "kintone-mcp"
KINTONE_MCP_KV_NAMESPACE_ID = "<32 桁の 16 進>"
KINTONE_MCP_HOST = "<subdomain>.cybozu.com"
KINTONE_MCP_PUBLIC_BASE_URL = "https://<domain>"
KINTONE_MCP_ACCESS_TEAM_DOMAIN = "<team>.cloudflareaccess.com"
KINTONE_MCP_ACCESS_AUD = "<AUD タグ>"
# KINTONE_MCP_CUSTOM_DOMAIN = "<domain>"   # カスタムドメインを使う場合だけ
```

```sh
mise run deploy:render      # wrangler.deploy.jsonc を生成(Cloudflare には何もしない。値の検証もここ)
mise run deploy             # 生成 → wrangler deploy -c wrangler.deploy.jsonc
# secrets(初回と更新時だけ。プロンプトに値を貼る。コマンドラインに値を書かない)
pnpm exec wrangler secret put KINTONE_CLIENT_ID -c wrangler.deploy.jsonc
pnpm exec wrangler secret put KINTONE_CLIENT_SECRET -c wrangler.deploy.jsonc
```

`mise run deploy:check` は、環境変数なしでダミー値の設定を生成して `wrangler deploy --dry-run` するだけ(Cloudflare へは何も変更しない)。upstream の CI がこれを使う。

## 6. 初回デプロイと動作確認

デプロイ(`Deploy` を実行)したら、上から順に確かめる。【未検証】V27(初回デプロイの実挙動)。

```sh
BASE=https://<domain>
curl -s $BASE/healthz                                          # {"status":"ok"} と 200
curl -s $BASE/.well-known/oauth-protected-resource/mcp         # resource が https://<domain>/mcp
mise run verify:oauth -- $BASE/mcp                             # OAuth の通し確認(ブラウザで Access のログイン・kintone のログインと同意)
```

1. **`/healthz`**: 200 `{"status":"ok"}` なら設定は揃っている。503 なら、本文の `missing` / `invalid` が直すべき項目(項目名だけ。値は出ない)。
2. **`/.well-known/oauth-protected-resource/mcp`**: JSON の `resource` が `https://<domain>/mcp` と一致する。違えば `KINTONE_MCP_PUBLIC_BASE_URL` が違う。
3. **Access の範囲**(ログインしていない状態で): `/authorize` と `/callback` は Access のログインへの 302 または 403、`/healthz` は 200、`/mcp` は 401、`/token` は 400 など Worker の応答になる。`/mcp` などが Access のログインに飛ぶなら、アプリの宛先が広すぎる(【未検証】V18)。

   ```sh
   curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' $BASE/authorize
   curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' $BASE/healthz
   curl -s -o /dev/null -w '%{http_code}\n' -X POST $BASE/mcp
   ```
4. **`mise run verify:oauth -- $BASE/mcp`**: DCR → ブラウザの認可 → `/token` → MCP(6 ツールの一覧)→ リフレッシュまでを通す。ブラウザでは、Access のログイン(セッションがなければ)→ 同意画面(Access のログイン email が出る)→ kintone のログインと同意、の順。**kintone のログインでは、Access の email と同じ本人のアカウントを選ぶ**こと(Worker は確かめられない。ADR 0010)。詳しい使い方と、続きの確認(V01〜V30)は [docs/manual-verification.md](manual-verification.md)。
5. MCP クライアントに `https://<domain>/mcp` を登録して、`kintone_apps_search` が自分の見えるアプリだけを返すことを確かめる。書き込みを伴う確認は専用のアプリだけで行う(V24・V26)。

## 7. WAF / Rate Limiting

`/register`(DCR)と `/token` は、匿名で呼べる(`/authorize` と `/callback` は Access の JWT が要る)。有効な grant を作るには kintone のログインが要るが、匿名の DCR でも KV にクライアント登録が書き込まれる。**レート制限は Worker に実装していない**。必要なら前段の Cloudflare WAF / Rate Limiting で、`/register` と `/token` を制限する(ADR 0010)。

- 例: ルールの条件を「URI パスが `/register` に等しい」とし、同じ IP から 1 分あたりの回数を絞る。`/token` は MCP クライアントのリフレッシュで使うので、制限は緩めにする(Portal など複数クライアントが同じ IP から使うと、IP 単位の制限に当たりうる)。
- 閾値・プランごとに使える機能・正常な利用を妨げないかは、実機で確かめていない(【未検証】V30)。まず観測(ログ)だけにして、閾値を決めてから拒否にするとよい。
- `/mcp` はトークンで守られる。必要なら、IP 範囲や国で絞る WAF ルールを足す。

## 8. MCP Portal(任意)

前段に Cloudflare MCP Server Portal を置く場合の注意。

- 認証方式は **OAuth(DCR による自動登録)+ Require user auth** にする(【未検証】P01)。各ユーザーが Portal から自分の kintone アカウントで認可する(【未検証】P02)。
- **Access の Managed OAuth は有効にしない**(この Worker は自前の OAuth 認可サーバー)。
- サーバー URL は `https://<domain>/mcp`。`?read_only=true` や `?toolsets=...` を付けると、書き込みツールを隠せる(Portal がクエリを保持するかは【未検証】P03)。
- Portal 経由でも、ブラウザが `/authorize` を開くときは Access のセッションが要る(再ログインが要るか、Portal の Access と共有されるかは【未検証】P04)。

## 9. 運用

### 9-1. ログの見方

`wrangler.jsonc` の `observability` で Workers Logs を有効にしている。ダッシュボードの Workers のログか、`mise run deploy:render` のあと `pnpm exec wrangler tail <Worker 名>` で見る。**トークン・シークレット・email はログに出ない設計**(kintone のトークンは grant の `props`(暗号化して KV)にだけある)。手がかり:

| ログ | 意味 |
|---|---|
| `{"event":"config_invalid","missing":[...],"invalid":[...]}` | 設定チェックの失敗。項目名だけ。`/healthz` にも同じ内容が出る |
| `{"event":"access_jwt_rejected","reason":"missing\|invalid\|jwks_unavailable"}` | `/authorize` `/callback` の Access JWT の拒否。`missing` = ヘッダーなし(Access の宛先から外れている)、`invalid` = 署名・`aud`・`iss`・期限・email の不備、`jwks_unavailable` = 鍵の取得失敗(503) |
| `{"event":"access_email_mismatch"}` | `/callback` で Access の email が `/authorize` と違った(403) |
| `{"event":"kintone_signin_failed","kind":...,"transient":bool}` | 認可の最後(kintone のコード交換・疎通確認)の失敗。`kind` は `invalid_grant` / `invalid_client` / `rejected` / `temporarily_unavailable` / `unauthorized` / `api_error` |
| `{"event":"kintone_unauthorized","kintone_remaining_seconds":N,"stale_marked":bool}` | `/mcp` で kintone が 401。残り秒数が正なら保存期限より前に失効している |
| `{"event":"kintone_token",...}` / `{"event":"token_exchange",...}` | kintone のトークン取得・更新、MCP のトークン発行・更新の判定(更新の実挙動は V01) |
| `tool <名前> failed unexpectedly: ...` | ツールで想定外の例外(トークンは伏せ字) |
| `idempotency cache ... failed` / `resolver cache ... failed` | KV の失敗(冪等・名前解決のキャッシュ)。ツールは止まらない |

### 9-2. kintone の client secret のローテーション

1. cybozu.com 共通管理でクライアントシークレットを再発行できる場合は再発行する(できない場合は新しいクライアントを登録して、client id / secret の両方を差し替える。その場合、既存の grant は旧 client id で発行されたリフレッシュトークンを持つため、**全員が再認可**になる。【未検証】V29)。
2. GitHub の Secrets(`KINTONE_CLIENT_SECRET`、`KINTONE_CLIENT_ID` も変えたなら同様)を更新し、`Deploy` を手動実行する(手元なら `pnpm exec wrangler secret put KINTONE_CLIENT_SECRET -c wrangler.deploy.jsonc`)。
3. 切り替えの間(kintone 側で旧シークレットが無効になってから Worker に新しい値が入るまで)は、kintone のトークン更新が失敗する。grant は残るので、新しい値を入れれば回復する。
4. `mise run verify:oauth -- $BASE/mcp` と `--refresh-only` で更新できることを確認する。

### 9-3. KV に保存されるもの

| 接頭辞 | 内容 |
|---|---|
| `client:<clientId>` | DCR で登録された MCP クライアント |
| `grant:<userId>:<grantId>` | 利用者ごとの認可(**kintone のトークンを含む `props`**。ライブラリが暗号化して保存) |
| `token:<userId>:<grantId>:<id>` | Worker が発行した MCP のトークンの記録(ハッシュ) |
| `kcache:v1:<userId>:<grantId>:...` | アプリ・フィールドの名前解決のキャッシュ(TTL 600 秒) |
| `idem:<userId>:<grantId>:...` | `kintone_record_create` の冪等キャッシュ(TTL 300 秒) |
| `kintone-stale:<userId>:<grantId>` | kintone が 401 を返した grant の印(次の更新で kintone も更新させる) |

`<userId>` は Access の email の SHA-256(生の email は KV にもトークンにも入らない)。

## 10. 障害の切り分け

まず `curl -s $BASE/healthz` で設定の状態を見る。次に症状から。

| 症状 | 原因の見方 |
|---|---|
| **`/healthz` が 503** `{"status":"misconfigured","missing":[...],"invalid":[...]}` | 設定の欠け・不正。本文の項目名を直す(Secrets の投入、Variables の修正、`<...>` の置き忘れ)。`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` もここに出る。直したら再デプロイ(項目名は Worker の名前で、Variable 名は `KINTONE_MCP_*`。対応は設定値の対応表) |
| `/healthz` 以外が 500 `server_misconfigured` | 同上(ログに項目名。`config_invalid`) |
| **`/authorize` / `/callback` が 403** | Access の JWT が拒否された。ログの `access_jwt_rejected` の `reason` を見る。`missing`: Access のアプリの宛先に `/authorize` と `/callback` が含まれていない・Access を通っていない。`invalid`: `ACCESS_AUD` の取り違え、`ACCESS_TEAM_DOMAIN` の違い、JWT に email がない(IdP の設定。V17)。`access_email_mismatch`: `/authorize` と `/callback` で別の Access ユーザーだった(同じブラウザで最初からやり直す) |
| **`/authorize` / `/callback` が 503** | Access の鍵(JWKS)を取得できない(fail closed)。`ACCESS_TEAM_DOMAIN` が違う、または Access / Cloudflare の障害。ログの `reason: jwks_unavailable` |
| `/mcp` `/token` `/register` などが Access のログインに飛ぶ | Access のアプリの宛先が広すぎる。`/authorize` と `/callback` の 2 つだけにする(手順 2-1。V18) |
| **kintone が 401**(MCP クライアントが `/mcp` で 401 を受け、再認可になる) | kintone のトークンの失効。Worker は HTTP 401 + `WWW-Authenticate: Bearer error="invalid_token"` を返し、クライアントにリフレッシュさせる(`kintone_unauthorized` のログ)。リフレッシュが `invalid_grant` なら kintone 側で連携が無効になった(再認可が必要)。再認可でも 401 / 403 なら、OAuth クライアントの STEP2「利用者の設定」にその利用者が入っているか、アプリの閲覧権限、`KINTONE_HOST` の違い、IP 制限(V10)を確認する |
| **kintone の認可画面でエラー / `redirect_uri` の不一致** | cybozu.com に登録したリダイレクト URI が **`<PUBLIC_BASE_URL>/callback` と完全一致していない**(末尾スラッシュ・http と https・サブドメインの違い)。`KINTONE_MCP_PUBLIC_BASE_URL` 自体が実際の公開 URL と違うことも多い(`mise run verify:oauth` の metadata の段階が、`PUBLIC_BASE_URL` と接続先の食い違いを検出する)。登録内容を直すか Variable を直して再デプロイ |
| 認可の最後に失敗(`/callback` が 502 / `temporarily_unavailable` など) | ログの `kintone_signin_failed` の `kind` を見る。`invalid_client`: client id / secret の違い。`rejected`: PKCE を拒否された可能性(`KINTONE_MCP_OAUTH_PKCE=off` を試す。V04)。`unauthorized`: 認可直後の疎通確認が 401(利用者の許可・IP 制限) |
| ツールが `permission_error`(403) / `not_found`(404) | その利用者の kintone 権限・存在しない ID・ゲストスペースのアプリ(対象外。ADR 0003) |
| ツールが `rate_limited`(`retryable: true`) | kintone が 429。読み取りは最大 2 回自動で待つ。それでも 429 ならそのまま返す。書き込みはリトライしない。しばらく待つ |
| ツールが `api_error`(`retryable: true`) | kintone の 5xx、通信失敗、タイムアウト(30 秒)。書き込みは結果が分からないので、**再送する前に内容を確認する**(`kintone_record_create` は同じ引数を 5 分以内に再送しても、前回の結果が返るだけで重複しない場合がある) |
| `response_too_large` | 応答が上限を超えた。絞り込むか `max_response_tokens` を指定する |
| `/mcp` が 400 `invalid_query` | URL クエリの綴り違い・不正な値(`?toolsets=app,record` `?read_only=true` だけ) |

## 11. Terraform などで管理する場合

このリポジトリには Terraform を入れない。Access のアプリや Portal を別のリポジトリ(IaC)で管理する場合は、上の「設定値の対応表」と手順 2 の値(ドメイン、宛先 2 つ、Managed OAuth を使わないこと)を渡せばよい。Worker 自体(KV・ドメイン・vars)は `wrangler.jsonc` と Variables から生成する設定(ADR 0012)が定義しており、`Deploy` ワークフロー(または `mise run deploy`)で反映する。Terraform で Worker や KV も管理する場合は、二重管理にならないよう、どちらか一方に寄せる。
