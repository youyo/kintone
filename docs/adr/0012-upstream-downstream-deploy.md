# ADR 0012: upstream / 下流モデルとデプロイ設定の生成

- 状態: 採用(M05 で実装)。logbutler ADR 0011 と同じ設計
- 日付: 2026-11-06
- 関連: ADR 0003(1 デプロイ = 1 ホスト)、ADR 0009(logbutler の規約の採否)、ADR 0010(Access の必須設定)、`docs/runbook.md`、`scripts/render-wrangler-config/`、`.github/workflows/deploy.yml`、仕様書 §9

## 背景

このリポジトリは OSS の upstream で、利用者はこれを upstream とする下流のリポジトリを作り、そこでデプロイ CI を動かす。下流は以降も upstream の変更を `git pull` で取り込み続ける。`wrangler.jsonc` にアカウント固有の値(Worker 名・KV id・ドメイン・kintone ホスト・Access の AUD とチームドメイン)を書かせる方式では、

- 下流が追跡ファイルを編集するので、upstream が同じ箇所を触るたびに pull が競合する
- アカウント固有の値が、公開される upstream の履歴に入りうる
- `env.production` などの名前付き環境は `vars` / `kv_namespaces` / `observability` を継承しないため、upstream が設定を足しても下流に届かない

## 決定

要件: (1) 下流は upstream の追跡ファイルを編集しない、(2) アカウント固有の値は upstream に置かない、(3) upstream の実行時設定(`compatibility_date`・`main`・`observability`・bindings の追加など)は pull だけで下流に届く、(4) 下流の運用者は GitHub Actions の Variables / Secrets を登録するだけ。

方式: **デプロイのたびに、upstream の `wrangler.jsonc` と環境変数から、デプロイ用の設定を gitignore 済みのファイルに生成し、`wrangler deploy -c <生成物>` する。**

| 何を | どこに置くか |
|---|---|
| `main`・`compatibility_*`・`observability`・ほかの bindings・vars の追加 | upstream の `wrangler.jsonc`(ダミー値のまま。ローカル開発・テストが読む)。生成時にそのまま引き継ぐ |
| Worker 名・KV id・`KINTONE_HOST`・`PUBLIC_BASE_URL`・`ACCESS_TEAM_DOMAIN`・`ACCESS_AUD`・カスタムドメイン・`KINTONE_OAUTH_PKCE` | 環境変数 `KINTONE_MCP_WORKER_NAME` / `_KV_NAMESPACE_ID` / `_HOST` / `_PUBLIC_BASE_URL` / `_ACCESS_TEAM_DOMAIN` / `_ACCESS_AUD` / `_CUSTOM_DOMAIN`(任意)/ `_OAUTH_PKCE`(任意)。CI は GitHub の Variables、手元は `mise.local.toml` の `[env]` |
| `KINTONE_CLIENT_ID` / `KINTONE_CLIENT_SECRET` | CI は GitHub の Secrets から `wrangler deploy --secrets-file`(ランナーの一時ファイル、0600、ジョブ終了で消える)。手元は `wrangler secret put -c <生成物>` |
| Cloudflare の認証 | Secrets の `CLOUDFLARE_API_TOKEN` / `CLOUDFLARE_ACCOUNT_ID` |
| 生成物 | `wrangler.deploy.jsonc`(`.gitignore` の `wrangler.deploy*.jsonc`)。`main` の相対パスが解決されるようリポジトリ直下に置く |

- 生成は `scripts/render-wrangler-config/`(`core.ts` が純粋な変換と検証で Web 標準 API だけ、`main.ts` が入出力で Node 24 の型ストリップで直接実行する)。`wrangler.jsonc` の `env` ブロックと `routes` は持ち越さず、フラットな設定にする。差し替えるのは `name`、`kv_namespaces` の `OAUTH_KV` の id、`vars` の 4〜5 つ、`routes` / `workers_dev` だけ(カスタムドメインがあれば `{pattern, custom_domain: true}` と `workers_dev: false`、なければ `workers_dev: true`)。
- 必須値の欠け・形式不正・`<...>` の残存は、**項目名と理由だけ**を出して非 0 で終了する(値は出さない)。検証は `checkConfig`(仕様書 §9)より厳しくしてよい(`PUBLIC_BASE_URL` は https のオリジンだけ、など)が、緩めない。
- `KINTONE_MCP_OAUTH_PKCE`(任意)は、`wrangler deploy` が設定にない vars を消す(`--keep-vars` なし)ため、PKCE の逃げ道(ADR 0004)を下流が使えるように足した。仕様書 §9 の 7 項目に加える。
- `mise run deploy:check` は `--example` で固定のダミー値から `wrangler.deploy.example.jsonc` を生成し、`wrangler deploy --dry-run` する。upstream の CI は Variables を持たないのでこれを使う。`--example` は親プロセスの環境変数を読まない。
- **`wrangler.deploy.example.jsonc` は追跡する**(logbutler は gitignore のまま)。ダミー値だけで、`--example` の生成物と一致することを `test/node/deploy-config.test.ts` が固定する。生成と dry-run を別々のコマンドで呼ぶ検証(生成が走らない場合を含む)でも、ファイルがなくて落ちないようにするため。実用の `wrangler.deploy.jsonc` は従来どおり gitignore(アカウント固有の値は追跡ファイルに書かない)。
- `.github/workflows/deploy.yml` は、Variable `KINTONE_MCP_WORKER_NAME` が空のとき(= upstream)ジョブごと飛ばす。秘密値はジョブ全体の `env` に置かず、該当ステップの `env` だけに渡す。アクションは `ci.yml` と同じコミット SHA で固定する。

## 却下した案

- **下流が `wrangler.jsonc` を直接編集する**: 上のとおり、pull が競合し、アカウント固有の値が upstream の履歴にも入りうる。
- **CLI フラグだけで渡す(`--name` / `--var` など)**: wrangler 4.151.0 では KV namespace の id をフラグで上書きできない(logbutler で調査済み)。
- **`env.production` を upstream に残し、下流が書き換える / 上書きファイルを足す**: 名前付き環境は継承されず、upstream の実行時設定の追加が届かない。
- **JSONC パーサーの依存を足す**: 必要なのはコメントと末尾カンマの除去だけで、`core.ts` の数十行で済む(文字列内の `//` `/*` は壊さないテストあり)。依存を足さない。
- **`deploy.yml` を `wrangler secret put` で回す**: 値がコマンドラインやシェルの履歴に出うる。`--secrets-file`(一時ファイル)にする。

## 結果と注意

- upstream のトップレベル設定を変えれば、下流は pull して次のデプロイで反映される。KV やドメインを変えたいときは Variables を変える。
- `wrangler deploy` は設定にない `vars` を消す。ダッシュボードで手動追加した vars は消えるので、必要な vars は `wrangler.jsonc` に足す。
- Access の AUD は秘密ではないので Variables(Secrets にしない)。

## 未検証の前提(runbook に【未検証】。`docs/manual-verification.md` V27・V29)

- Cloudflare API トークンの最小権限の厳密な組み合わせ
- OAuth クライアントが複数のリダイレクト URI を持てるか
- カスタムドメイン(`custom_domain: true`)デプロイ時の DNS・証明書の作成(logbutler の運用を前提にしている)
