# M05: デプロイ準備

ロードマップ: `plans/kintone-mcp-roadmap.md`(M05 の行、ユーザー手順 U1〜U7)
仕様: `docs/specs/kintone-mcp-spec.md`(§9 設定項目と `checkConfig`・デプロイ設定の生成、§11 既知の限界)
設計判断: `docs/adr/0003`(1 デプロイ = 1 ホスト・IP 制限)、`0004`(OAuth の仕様適用・リダイレクト URI)、`0009`(logbutler の規約の採否。upstream / 下流モデルは M05 で採用)、`0010`(Access の email・`/authorize` と `/callback` だけを守る)、`0011`(冪等キャッシュ)、新規 `0012`(upstream / 下流モデルとデプロイ設定の生成)
書式の手本: `plans/kintone-mcp-m04-write-tools.md`
姉妹プロジェクト logbutler(公開リポジトリ `youyo/logbutler`)の参照: `scripts/render-wrangler-config/{core,main}.ts`、`mise.toml` の `deploy:render` / `deploy:check` / `deploy`、`.github/workflows/{ci,deploy}.yml`、`docs/runbook.md`、`docs/adr/0011-upstream-downstream-deploy.md`、`test/node-e2e/render-wrangler-config.test.ts`、`test/unit/readme-tools.test.ts`。同じ設計にする。kintone 固有の差分は下記

## 目的

M01〜M04 でコードは揃った(6 ツール、OAuth、Access、キャッシュ)。M05 は、**第三者が自分の Cloudflare アカウント・cybozu.com で動かせる状態にする**。実際のデプロイ・secret の登録・KV の作成は**しない**(ユーザーが U2〜U7 で行う)。やるのは次の 4 つ。

1. **upstream / 下流モデルのデプロイ設定の生成**: このリポジトリは upstream で、`wrangler.jsonc` はダミー値の共有設定のまま。アカウント固有の値は環境変数 `KINTONE_MCP_*` から、gitignore 済みの `wrangler.deploy.jsonc` に生成する(`scripts/render-wrangler-config/`)。検証は `deploy:check`(ダミー値の dry-run。Cloudflare には何も変更しない)
2. **CI / デプロイのワークフロー**: `ci.yml` に `deploy:check`、`deploy.yml`(下流の Variables / Secrets で動く。Worker 名の Variable が空の upstream では飛ばす)
3. **ドキュメント**: `docs/runbook.md`(第三者が上から順にたどれる手順書)、`README.md`(製品説明・ツール一覧・商標・Go 版からの移行案内)、`CHANGELOG.md`
4. **M04 の検証で出た指摘 3 件**: `kintone_record_create` の description の明確化、書き込みのタイムアウトでリトライしないことのテスト、`docs/manual-verification.md` への DELETE の JSON 本文の確認項目

## 完了条件

- `mise run test`(unit + **node-e2e を含む** e2e)と `mise run lint` と `mise run build` と **`mise run deploy:check`** が通る。検証コマンドは `pnpm exec vitest run --project unit --project node` / `pnpm exec vitest run --project e2e --project node-e2e` / `pnpm exec biome check .` / `pnpm exec tsc --noEmit` / `pnpm exec tsc --noEmit -p scripts/tsconfig.json` / `node scripts/render-wrangler-config/main.ts --example && pnpm exec wrangler deploy --dry-run -c wrangler.deploy.example.jsonc --outdir dist`
- **`scripts/render-wrangler-config/`**
  - `core.ts` は Web 標準 API だけ(`node:*` を import しない)。`renderWranglerConfig(baseText, env)` が結果(`{ok, config, text}` または `{ok:false, errors}`)を返す純粋関数。入出力は `main.ts`(`node:fs` / `process.env` / 終了コード)。`main.ts` は Node 24 の型ストリップで直接実行する(`enum` / parameter property を使わない。`import` は拡張子 `.ts` つき)
  - 入力の検証: 欠けている値(必須の環境変数が未定義・空・空白のみ)、形式が違う値、プレースホルダ `<...>` を拒否する。**エラーには項目名と理由だけを出し、値は出さない**(テストで、入力に秘密らしい文字列を入れて出力に現れないことを確かめる)。複数の誤りは 1 回の実行ですべて報告する
  - `--example`: 固定のダミー値(`EXAMPLE_ENV`)から `wrangler.deploy.example.jsonc` を生成する。`--out <path>` で出力先を変えられる。不明な引数は終了コード 2、検証エラーは 1、成功は 0。エラーのときはファイルを作らない
  - 生成する環境変数(下表)。`wrangler.jsonc` の `env` ブロックは持ち越さず、フラットな設定にする。差し替えるのは `name`、`kv_namespaces` の `OAUTH_KV` の id、`vars`、`routes` / `workers_dev` だけ。それ以外(`main`・`compatibility_*`・`observability` など)は `wrangler.jsonc` のものがそのまま届く。JSONC(コメント・末尾カンマ)は自前で読む(文字列内の `//` `/*` を壊さない。依存を足さない)
- **mise タスク**: `deploy:render`(`node scripts/render-wrangler-config/main.ts`)、`deploy:check`(`--example` で生成 → `wrangler deploy --dry-run -c wrangler.deploy.example.jsonc --outdir dist`。環境変数・秘密情報なしで通る)、`deploy`(生成 → `wrangler deploy -c wrangler.deploy.jsonc`。**定義だけで、実行しない**)。`test:e2e` に `--project node-e2e` を足す(M01 から保留にしていたもの。`test/node-e2e/` が空でなくなる)
- **`test/node-e2e/render-wrangler-config.test.ts`**: 生成スクリプトを**実プロセスで起動**し(終了コード・stderr・生成ファイル)、生成物を実際の `wrangler deploy --dry-run -c` に通す(カスタムドメインあり / なし)。値を出さないこと、エラー時にファイルを作らないこと、`--example` と `--out` 省略時の出力先
- **`.github/workflows/ci.yml`**: `mise run deploy:check` を足す。**`.github/workflows/deploy.yml`**(新規): `on: push(main) + workflow_dispatch`、`permissions: contents: read`、`concurrency`(進行中のデプロイは止めない)、`jobs.deploy.if: ${{ vars.KINTONE_MCP_WORKER_NAME != '' }}`(**空なら = upstream ではジョブごと飛ばす**)。流れ: checkout → mise → install → lint → test → `deploy:render`(Variables から) → デプロイ(`wrangler deploy -c wrangler.deploy.jsonc --secrets-file <一時ファイル>`)。**秘密値をジョブ全体の `env` に置かない**(ステップの `env` だけ。`wrangler secret put` の扱いは logbutler に合わせ、CI では `--secrets-file` を使い、`secret put` は手元の予備経路だけ)。**アクションはコミット SHA で固定**し、`ci.yml` と同じ SHA を使う(`actions/checkout` v7.0.1 = `3d3c42e5…`、`jdx/mise-action` v5.1.2 = `08ae2a09…`、mise 2026.10.9)。新しいアクションは使わない。`test/node/deploy-config.test.ts` が、これらの不変条件(全 `uses:` が 40 桁の SHA、`if` の条件、ジョブ全体の `env` がない、`deploy:check` が ci.yml にある、`test:e2e` に node-e2e がある、`wrangler.deploy*.jsonc` が gitignore)を固定する
- **`docs/runbook.md`**(第三者が上から順にたどれる)。含めるもの: 設定値の対応表、下流リポジトリの作り方、cybozu.com 共通管理での OAuth クライアント登録(リダイレクト URI は `<PUBLIC_BASE_URL>/callback` と完全一致、scope は 6 つ、STEP2 の利用者の許可)、Cloudflare Access の self-hosted アプリ(保護するのは `/authorize` と `/callback` だけ。AUD とチームドメインの取得、ポリシー。`/mcp` `/token` `/register` `/.well-known/*` `/healthz` は保護しない。Managed OAuth は使わない)、Access から外した利用者の grant を失効させる手順、KV の作成(作り直さない理由も)、Variables / Secrets の登録、初回デプロイと動作確認(`/healthz`・`/.well-known/oauth-protected-resource/mcp`・`mise run verify:oauth`)、WAF / Rate Limiting、IP 制限の注意、障害の切り分け(`/healthz` の 503、Access の 403 / 503、kintone の 401、`redirect_uri` の不一致)。**実機で確かめていない手順には【未検証】と付け、`docs/manual-verification.md` の V 番号に結ぶ**(`test/node/deploy-config.test.ts` が、【未検証】の行にある V 番号が実在することと、必須の節の見出しがあることを確かめる)
- **`README.md`**: 製品の説明、**6 ツールの一覧(toolset ごとの表)**、kintone はサイボウズ株式会社の商標で本プロジェクトは無関係である旨、**Go 版の利用者への移行のお知らせ**(CLI は廃止。`archive/go` ブランチ・タグ、v0.5.6 の Homebrew / ghcr はそのまま使える。**破壊的変更の一覧**: ツール名に `kintone_` が付く、出力・エラーの形式、エラーコードが小文字になる、認証が利用者ごとの OAuth と Access に変わる、API トークンの廃止、設定項目)、`test/unit/readme-tools.test.ts`(logbutler と同じ。README の表と `ALL_TOOLS` の突き合わせ)
- `CHANGELOG.md` に今回の作り直しを記録する(先頭に新しい節。Go 版の記録は残す)
- **M04 の検証の指摘**:
  1. `kintone_record_create` の description に、「明示の冪等キーがないと、5 分以内に同じ引数で呼んだものは再送とみなし、前回の結果を返す(2 件目は作られない)」ことと、意図して同じ内容を 2 件作るときの方法(別の `idempotency_key` を引数、または `_meta.idempotency_key` で渡す)を明記する。tools/list のスナップショットを更新し、差分をレビューする
  2. 書き込みツール(3 つ)経由でタイムアウト(`AbortError`)が起きても書き込みをリトライしないことのテストを `write-no-retry.test.ts` に足す(クライアント単体のテストは M02 にあり、ツール経由で固定する)
  3. `docs/manual-verification.md` に「`DELETE /k/v1/records.json` を JSON 本文で送る形を実機で確かめる」項目がなければ足す(V24 の (c) に含まれているが、独立した項目にして、本文を受け付けないときの対処も書く)
- ロードマップの M05 を `[x]`、CLAUDE.md の「構成」と「テストの書き方」(node-e2e)を更新する。仕様書 §9 の生成する環境変数、ADR 0009 の M05 の行、新しい ADR 0012
- **実機確認(U2〜U7・V 番号)は完了条件に含めない**(ユーザー実施)。デプロイ・`wrangler secret put`・KV の作成・実 kintone / 実 Cloudflare との通信はしない(`deploy:check` と node-e2e の `--dry-run` だけ)。依存の追加なし

## 生成する環境変数(`KINTONE_MCP_*`)

仕様書 §9 の設定項目に合わせる。置き場所は、CI = 下流リポジトリの GitHub Variables、手元 = `mise.local.toml` の `[env]`(gitignore 済み)。

| 環境変数 | 入る先(`wrangler.deploy.jsonc`) | 必須 | 検証 |
|---|---|---|---|
| `KINTONE_MCP_WORKER_NAME` | `name` | 必須(空なら `deploy.yml` は飛ばされる) | 小文字英数字とハイフン、63 文字以内 |
| `KINTONE_MCP_KV_NAMESPACE_ID` | `kv_namespaces` の `OAUTH_KV` の `id` | 必須 | 32 桁の 16 進 |
| `KINTONE_MCP_HOST` | `vars.KINTONE_HOST` | 必須 | ホスト名(スキーム・パス・ポートなし) |
| `KINTONE_MCP_PUBLIC_BASE_URL` | `vars.PUBLIC_BASE_URL` | 必須 | https のオリジンだけ(末尾スラッシュ・パス・クエリ・大文字のホストは不可) |
| `KINTONE_MCP_ACCESS_TEAM_DOMAIN` | `vars.ACCESS_TEAM_DOMAIN` | 必須 | ホスト名 |
| `KINTONE_MCP_ACCESS_AUD` | `vars.ACCESS_AUD` | 必須 | 空白を含まない(形式は `checkConfig` と同じく検査しない) |
| `KINTONE_MCP_CUSTOM_DOMAIN` | `routes`(`custom_domain: true`)と `workers_dev: false`。なければ `workers_dev: true` | 任意 | ホスト名 |
| `KINTONE_MCP_OAUTH_PKCE` | `vars.KINTONE_OAUTH_PKCE` | 任意 | `s256` または `off`(`wrangler deploy` は設定にない vars を消すので、PKCE の逃げ道を下流が使えるよう足す。仕様書 §9 は 7 つ + これ) |

全部で共通: 前後の空白は取り除く。`<...>` を含む値は `placeholder`、形式が違えば `invalid`、必須で空なら `missing`。値はエラーに出さない。Secrets(`KINTONE_CLIENT_ID` / `KINTONE_CLIENT_SECRET`)は設定ファイルに入れない(デプロイ時の `--secrets-file` または `wrangler secret put`)。

## 対象範囲

### 構成(M05 で足す・変えるもの)
```
scripts/render-wrangler-config/core.ts   新規。純粋な変換と検証(Web 標準 API だけ)
scripts/render-wrangler-config/main.ts   新規。引数・ファイル入出力・終了コード
scripts/tsconfig.json / tsconfig.json    core.ts を型検査の対象に足す
wrangler.deploy.example.jsonc            新規。--example の生成物(ダミー値だけ。追跡する。理由は ADR 0012)
.gitignore                               wrangler.deploy*.jsonc は無視したまま、example だけ追跡する(!)
mise.toml                                deploy:render / deploy:check / deploy、test:e2e に node-e2e
.github/workflows/ci.yml                 deploy:check
.github/workflows/deploy.yml             新規
docs/runbook.md  README.md  CHANGELOG.md
docs/adr/0012-upstream-downstream-deploy.md  新規
test/unit/render-wrangler-config.test.ts     core の unit
test/unit/readme-tools.test.ts               README とツール定義の突き合わせ
test/node/deploy-config.test.ts              ワークフロー・mise・gitignore・runbook・example の不変条件
test/node-e2e/render-wrangler-config.test.ts 実プロセス + wrangler dry-run
src/mcp/tools/record-create.ts               description
test/unit/write-no-retry.test.ts             タイムアウトでリトライしない
test/unit/tool-record-create.test.ts         description の内容
test/snapshots/tools-list.json               description の更新
```

### 設計の要点(logbutler ADR 0011 との差分)
- 環境変数の接頭辞は `KINTONE_MCP_`、検証対象は kintone 用(`KINTONE_HOST`・Access の 2 値が加わり、Backlog のスペースホストがない)
- `KINTONE_MCP_OAUTH_PKCE`(任意)を足す(上記)
- `wrangler.deploy.example.jsonc` を追跡する(logbutler は gitignore のまま)。ダミー値だけで、`--example` の生成物と一致することを `deploy-config.test.ts` で固定する。理由: 検証コマンド・CI が生成と dry-run を分けて呼んでも、生成物がない状態で落ちないこと。実用の `wrangler.deploy.jsonc` は従来どおり gitignore(アカウント固有の値は追跡ファイルに書かない)
- Secrets は `KINTONE_CLIENT_ID` / `KINTONE_CLIENT_SECRET`。Access の値(AUD・チームドメイン)は秘密ではないので Variables

## TDD の進め方(小さい順。各段階で失敗を確認してから実装)

1. **core の unit(Red から)**: `render-wrangler-config.test.ts`。必須の欠け・空白のみ・形式違い・プレースホルダ・値を出さない・複数の誤りの同時報告・`wrangler.jsonc` の引き継ぎ・`env` の持ち越しなし・KV id の差し替え・カスタムドメインの有無・PKCE・JSONC の読み(文字列内の `//`・末尾カンマ)・EXAMPLE_ENV が検証を通る。**期待する失敗**: `core.ts` がなくて import に失敗
2. **README の突き合わせ(Red から)**: `readme-tools.test.ts`。**期待する失敗**: README に toolset の表・総数がない
3. **不変条件(Red から)**: `deploy-config.test.ts`(ワークフロー・mise・gitignore・runbook の節と【未検証】の V 番号・example の一致・README の商標と移行案内)。**期待する失敗**: `deploy.yml`・`runbook.md` がない、mise にタスクがない、など
4. **node-e2e(Red から)**: 実プロセス。**期待する失敗**: `main.ts` がなくて終了コードが 0 でない
5. **M04 の指摘**: record_create の description のテスト(**期待する失敗**: 「同じ引数で呼んだものは再送とみなす」旨と `_meta.idempotency_key` の言及がない)。書き込みのタイムアウトのテストは**クライアントの実装済みの挙動の特性テストなので、足した時点で緑**(Red はない。ただし、この挙動を変えるとテストが落ちることを確かめる)
6. 実装(`core.ts` → `main.ts` → `wrangler.deploy.example.jsonc` → mise → workflows → description → ドキュメント)→ `mise run test:update-snapshots` で差分をレビュー → 全体(`mise run test` / `lint` / `build` / `deploy:check`)

## 作るテストの一覧

| 種別 | ファイル | 内容 |
|---|---|---|
| unit | `render-wrangler-config.test.ts` | core の検証・変換(上記)。エラーの整形(項目名と理由だけ) |
| unit | `readme-tools.test.ts` | README の toolset ごとの表(件数・ツール名)と `ALL_TOOLS` の一致、総数の記載、実在しないツール名がない |
| unit | `tool-record-create.test.ts` | description に暗黙の再送判定(5 分・同じ引数・前回の結果・2 件目は作られない)と、同じ内容を 2 件作る方法(別の `idempotency_key`)がある |
| unit | `write-no-retry.test.ts` | 3 つの書き込みツールで、タイムアウト(`AbortError`)でも書き込みは 1 回で、`api_error`(retryable)になる |
| node | `deploy-config.test.ts` | ci.yml / deploy.yml / mise.toml / .gitignore / runbook / README / example の不変条件 |
| node-e2e | `render-wrangler-config.test.ts` | 実プロセス・終了コード・stderr・生成物・`wrangler deploy --dry-run -c` |

## やってはいけないこと
- デプロイ(`wrangler deploy` の `--dry-run` 以外)、`wrangler secret put`、KV の作成、実 kintone / 実 Cloudflare への通信。`mise run deploy` を実行すること(定義だけ)
- アカウント固有の値(Worker 名・KV id・ドメイン・kintone ホスト・Access の値・client id / secret)を追跡ファイル(ドキュメントを含む)に書くこと。例は `example.cybozu.com` や `<...>` の形にする
- エラー・ログ・テストの出力に入力値を出すこと
- `deploy.yml` で秘密値をジョブ全体の `env` に置くこと。アクションをタグで指定すること(SHA 固定)
- 依存の追加。`core.ts` で `node:*` を import すること。`main.ts` で `enum` / parameter property を使うこと
- テストを弱める・skip する・消す。スナップショットを差分のレビューなしで更新すること
- 実機で確かめていない手順を、【未検証】なしで断定すること

## 未検証の前提(実機でしか分からないもの。ブロッカーにせず、runbook に【未検証】と書いて進む)
- Cloudflare API トークンの最小権限の厳密な組み合わせ(runbook は公式での確認を促す)
- Access の self-hosted アプリでパス指定の 2 つだけを守れるか(V18)、`workers.dev` で使えるか(V20)、`/callback` で JWT が付くか(V19)
- OAuth クライアントのリダイレクト URI を複数登録できるか(公式に記載なし)。https でない URI の可否(V05)
- IP 制限下で Worker から使えるか(V10)
- KV の作成・`wrangler kv` による grant の削除手順の出力形式(U5。実際に実行して確かめていない)
- カスタムドメイン(`custom_domain: true`)のデプロイ時の DNS・証明書の作成(logbutler で確認済みの挙動を前提にする)

## 成果物
- 上記のコード・テスト・ドキュメント・ワークフロー、`test/snapshots/tools-list.json`
- ロードマップの M05 を `[x]`、CLAUDE.md の更新、ADR 0012
- 本ファイルの末尾に「実装後の記録(計画との差分)」

## 実装後の記録(計画との差分)

(実装後に書く)
