# kintone Remote MCP(Workers 版)ロードマップ

Go 製の kintone CLI + MCP サーバー(v0.5.6、`archive/go`)を、姉妹プロジェクト logbutler(`/Users/youyo/src/github.com/youyo/logbutler`)と同じ構成の
**Cloudflare Workers 上の Remote MCP サーバー**として作り直す。利用者の識別子は **Cloudflare Access の email(必須)**。ブラウザが通る `/authorize` と `/callback` だけを Access の self-hosted アプリで保護し、Worker が JWT を検証する(ADR 0010)。それ以外の制限は外側の Cloudflare MCP Portal / WAF で行う。

- 仕様: `docs/specs/kintone-mcp-spec.md`
- 設計判断: `docs/adr/0001`〜`0010`
- 実機でしか確かめられない前提: `docs/manual-verification.md`(V01〜、P01〜)
- 作業ブランチ: `feat/workers-rewrite`(Go 版は `archive/go` ブランチ・タグ。ローカルのみ。push はユーザー)
- 旧計画書(`plans/kintone-m01-project-skeleton.md` 〜 `kintone-m15-mcp-serve-wiring-hardening.md`、`kintone-roadmap.md`、乱数名の 3 ファイル `binary-imagining-lemur.md` / `graceful-snacking-wilkinson.md` / `wondrous-swinging-locket.md`)と `docs/specs/kintone_spec.md`・`docs/decisions/`・Go 向けの `.github/workflows/{ci,release}.yml`・`Dockerfile`・`.goreleaser.yaml`・`.golangci.yml`・`Makefile`・`README*.md`(Go 版の記述)・現 `mise.toml`(`go = "1.26"` のみ)は Go 版のもの。M01 で Go のコードと一緒に削除または置き換える(`archive/go` に残る)。`plans/kintone-mcp-*.md` と `docs/specs/kintone-mcp-spec.md`・`docs/adr/`・`docs/manual-verification.md` は新規で残す

## ユーザーが行う手順(コーディングエージェントは実行しない)

| # | いつ | 手順 | 備考 |
|---|---|---|---|
| U1 | 今すぐ(いつでも) | `git push origin archive/go` と `git push origin refs/tags/archive/go` | Go 版の保存。M01 でコードを消す前に済ませておくと安全 |
| U2 | M01 の実機確認の前 | cybozu.com 共通管理で **OAuth クライアントを登録**する。リダイレクト URI は `<PUBLIC_BASE_URL>/callback`(Go 版の `/oauth/kintone/callback` とは違う)。控えるもの: クライアント ID・シークレット | 登録上限は 20 個(公式)。https の公開 URL(カスタムドメインまたは `workers.dev`)が先に決まっている必要がある。Go 版の運用では kintone は https のリダイレクト URI を要求した(Go `CHANGELOG.md` v0.4.0・README: loopback の http は成立しない)。**OAuth フローの実機確認には公開 https の URL が要る(`wrangler dev` だけでは通せない)**。公式には条件の記載なし(V05) |
| U3 | U2 の直後 | 同じ OAuth クライアントの **「利用者の設定」で、使う kintone ユーザーを指定**する | これがないと利用者は認可できない(公式 add-client ページの STEP2。V03) |
| U4 | M01 の実機確認の前(U2 と同じく公開 URL が決まってから) | **Cloudflare Access の self-hosted アプリを作成**する。アプリのパス(destination)は `<ドメイン>/authorize` と `<ドメイン>/callback` の **2 つだけ**(`/mcp` `/token` `/register` `/.well-known` `/healthz` は含めない)。Access の Managed OAuth は有効にしない。ポリシーは許可する email・グループ。作成後に **AUD タグ**(アプリの Overview)と **チームドメイン**(`<team>.cloudflareaccess.com`)を控える | Worker の `ACCESS_AUD` / `ACCESS_TEAM_DOMAIN` になる(ADR 0010)。未設定だと Worker は 500/503 で動かない。パス指定で 2 つだけ守れるか、`workers.dev` で使えるかは未検証(V18・V20) |
| U5 | M01 の実機確認の前 | Cloudflare で KV namespace を作成し、Worker に Variables / Secrets(`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` を含む)を登録する(手順書は M05 の runbook。M01 では `.dev.vars` と手動 `wrangler` で足りる) | アカウント固有の値は `mise.local.toml` / `.dev.vars` / GitHub の Variables・Secrets にだけ置く |
| U6 | M01 完了後 | `docs/manual-verification.md` の V01〜V05 と V17〜V19 を実施して結果を記入する(`mise run verify:oauth`) | 実施はユーザーの指示を受けてから |
| U7 | M05 | cybozu.com の IP 制限を使っている場合は、Worker の送信元から使えるか確認する(V10)。MCP Portal へ登録(P01〜P04) | |

## 決定済み事項

| 項目 | 決定 | 理由・却下した案 |
|---|---|---|
| ランタイム | Cloudflare Workers + KV + Hono。Durable Objects は使わない。MCP はステートレス(`agents/mcp/server` の `createMcpHandler` + `@modelcontextprotocol/server` v2 + zod) | logbutler と同じ。ADR 0002 |
| 認証 | **利用者ごとの kintone OAuth**。Worker が OAuth 認可サーバー(`@cloudflare/workers-oauth-provider` v1)。kintone のトークンは grant の `props` にだけ置く。**API トークン(`X-Cybozu-API-Token`)経路は廃止** | ADR 0001。却下: API トークンの共有、Portal の passthrough |
| 利用者の識別と保護 | **Access の email を利用者 ID に必須化**(ユーザー決定)。`/authorize` と `/callback` を Access の self-hosted アプリで保護し、Worker が `Cf-Access-Jwt-Assertion` を検証(JWKS・iss・aud・exp)。email を `beginUpstream` の data に載せ、`/callback` で同じ email か確認。`userId` は email のハッシュ。`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` を必須設定にし、UUID へのフォールバックは作らない。同じ email・同じクライアントの再認可は旧 grant を失効させる。限界: email は「Access にログインした人」で kintone のアカウントとは限らないので、キャッシュ・冪等キー・stale 印は grant 単位 | ADR 0010(ADR 0002 の「Access 連携を持たない」を置き換え)。却下: 認可ごとのランダム UUID(草案)、Managed OAuth |
| 範囲 | 1 デプロイ = 1 つの cybozu.com ホスト(環境変数 `KINTONE_HOST`、ホスト全体で確定)。ゲストスペースは対象外 | ADR 0003 |
| ツール | 現行 6 ツール(`kintone_apps_search` / `app_describe` / `records_query` / `record_create` / `record_update` / `record_delete`)と名前解決(`app` / `app_ref` / `update_key_field_ref`)。拡張は M05 以降 | 仕様書 §5 |
| 出力・エラー | JSON を text content、エラーは `isError` + `{"schema_version":"1","error":{code,message,retryable,details?}}`。コードは小文字 snake_case で確定(ユーザー決定)。Go 版の `{"ok":...}` を使っていたクライアントは壊れる | ADR 0006 |
| ツール名・引数 | 接頭辞 `kintone_`。引数名は Go 版を踏襲 | ADR 0006 |
| kintone OAuth | Basic 認証、PKCE(S256。`KINTONE_OAUTH_PKCE=off` の逃げ道)、**scope は Go 版と同じ 6 つ**(今のツールが使わない権限も要求。ユーザー決定)、`userId` は Access の email のハッシュ、認可時に疎通確認 | ADR 0004、ADR 0010 |
| トークン更新 | logbutler ADR 0014 と同じ(失効後にだけ更新、MCP の寿命を kintone の残り寿命に合わせる、stale 印) | ADR 0005 |
| 名前解決とキャッシュ | Go の解決順序を踏襲。ページ上限 100 → 10。**書き込み系(record_create / update / delete)の `app_ref` は ID・コード完全一致・名前完全一致だけ。部分一致のみなら `resolver_app_exact_match_required` で候補を返す**(ユーザー決定。読み取り系は部分一致も使う)。キャッシュは `OAUTH_KV`、grant 単位のキー、TTL 600 秒、`not_found` で取り直し | ADR 0007 |
| 制限値 | 書き込みはリトライしない。`maxApiCalls` はツールごとに最悪ケースの式から明示(M03 で変更): 読み取り 3 / 39 / 36、書き込みは解決の GET × 3 + 書き込み 1 で 34 / 37 / 34(M04)。`max_response_tokens` 既定 50000 | ADR 0008 |
| logbutler の規約 | 採用/後回しの一覧は ADR 0009 | |
| 廃止 | CLI、Docker、GoReleaser、Homebrew、SQLite/Redis/DynamoDB、idproxy、OIDC | 新 main では持たない |
| ランタイムとタスク | mise(node 24 系と pnpm を固定)。`mise run test / test:unit / test:e2e / lint / fmt / build / dev` | グローバル規約。バージョンは logbutler の `mise.toml` に合わせる |
| テスト | vitest + `@cloudflare/vitest-plugin`(workerd)。kintone への通信はモックのみ(`mock-fetch` は未登録の宛先への通信を例外にする) | logbutler と同じ |

## Go 版の調査で分かった注意点(一次資料)

- **認証**: Go の Remote MCP は OIDC(idproxy)+ kintone OAuth。トークンは SQLite/Redis/DynamoDB の TokenStore(キー = ドメイン + PrincipalID + 認証種別)。kintone に利用者識別 API がないため、Workers 版の `userId` は Access の email のハッシュ(ADR 0004、ADR 0010)
- **Go のキャッシュは利用者に依存しないキー**(`internal/service/api/caching.go:74,98,139-186`)で TTL 1 年。アプリの見え方は利用者ごとに違うので、Workers 版では利用者別に変える(ADR 0007)
- **Go の名前解決は最悪 101 回の `apps.json`**(`internal/resolver/resolver.go:12`)。Workers の無料プラン(subrequest 50)では動かないので 10 ページに下げる(ADR 0008)
- **Go の `CLAUDE.md` は古い**(M11 完了時点の記述で、v0.3.0 以降の変更 = CLI の OAuth ログイン廃止、M12〜M17 を反映していない)。事実の確認は `CHANGELOG.md` と `git log` を正とした。CLAUDE.md の書き換えは M01 で行う
- **PKCE**: `KINTONE_OAUTH_PKCE_DISABLE` は M09 の計画書にだけあり、コードに存在しない。Go 版は常に PKCE を送って v0.3〜v0.5.6 が運用された(拒否されていないことの間接的な証拠。検証有無は不明。V04)
- **MCP の出力に `isError` を付けていない**(`internal/mcp/facade/result.go:14-31`)。Workers 版は付ける(破壊的変更)
- **書き込みはリトライしない**(`internal/kintoneapi/transport.go:219,228`)。そのまま引き継ぐ
- **`apps_search` は resolver を使わず 1 ページだけ**。`records_query` は 1 リクエストで自動ページングなし。出力形状は仕様書 §5 に固定した

## マイルストーン

縦切り。上から順に着手する。各マイルストーンの完了条件は共通で **`mise run test`(unit + e2e)と `mise run lint` が通る**こと(個別条件は追加)。TDD(Red → Green → Refactor)で進める。コミット・push・デプロイはユーザーの指示があるときだけ。

| # | 名前 | 内容 | 完了条件(共通条件に加えて) |
|---|---|---|---|
| M01 | 雛形 + OAuth | Go のコード・Go 向け計画書/仕様書/CI/Dockerfile/GoReleaser 設定の削除(最初のコミット)。mise / pnpm / wrangler / Hono / vitest / Biome の雛形。`/healthz`。DCR → `/authorize`(同意)→ kintone(モック)→ `/callback` → `/token` → `/mcp`。`checkConfig`(`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` を必須に含む)。**Access JWT の検証(`/authorize` `/callback`。logbutler の git 履歴から移植)と email からの `userId`**。トークン更新(`tokenExchangeCallback`・stale 印)。ダミーツール 1 個。`verify:oauth`。CLAUDE.md の書き換え。詳細: `plans/kintone-mcp-m01-bootstrap-auth.md` | 詳細計画書の完了条件。**実機確認(V01〜V05)は完了条件に含めない**(ユーザー実施) |
| M02 | kintone クライアントと基盤 | kintone REST クライアント(直列化・429/503 のリトライ・`Retry-After`・書き込みはリトライなし・呼び出し回数上限・応答 10 MiB 上限・タイムアウト)、エラー分類とエンベロープ(`details` 拡張)、ツール登録の仕組み(`defineTool`・4 区分 annotation・toolset `app`/`record`・`read_only`/`toolsets` の URL クエリ)、`max_response_tokens`、tools/list のスナップショットテスト | クライアントの unit(直列・リトライ・上限・トークンを出さない)。エラー表(仕様書 §6)の全行にテスト。スナップショットテスト |
| M03 | 読み取りツール + 名前解決 | `kintone_apps_search` / `kintone_app_describe` / `kintone_records_query`。リゾルバー(アプリ・フィールド、10 ページ上限、`candidates`、`mode: read\|write`。write モードは M03 で実装し unit で固定、M04 のツールが使う)。KV キャッシュ(grant 別キー `<userId>:<grantId>`・TTL 600 秒・`not_found` の取り直し・失敗しても続行)。`maxApiCalls` の最悪ケースをテストで固定 | 仕様書 §5.1〜5.3・§7・§10 のテスト(最悪ケースの呼び出し回数 = ADR 0008 の式)。E2E(Worker 全体への HTTP で `tools/call`) |
| M04 | 書き込みツール | `kintone_record_create`(冪等キャッシュ `idem:`)/ `record_update`(`update_key_field_ref`)/ `record_delete`。入力上限(100 件)の送信前検査。`read_only=true` で隠れること。書き込み系の `app_ref` と `update_key_field_ref` は完全一致のみ(決定済み。ADR 0007)。部分一致のみなら kintone の書き込み API を呼ばずに `resolver_app_exact_match_required` / `resolver_field_exact_match_required` | 仕様書 §5.4〜5.6 のテスト。書き込みがリトライされないことのテスト。E2E |
| M05 | デプロイ準備 | `.github/workflows/ci.yml` に `deploy:check` を足す(最小版は M01 で作成。SHA 固定)・`deploy.yml`、`render-wrangler-config` と `deploy:check`、`docs/runbook.md`(KV 作成・OAuth クライアント登録・利用者の許可・**Access self-hosted アプリの作成(`/authorize` と `/callback` のパス、AUD・チームドメイン、ポリシー)と、Access から外した利用者の grant の失効手順**・Variables/Secrets・WAF/Rate Limiting・IP 制限の注意)、`README.md`(製品説明、kintone は サイボウズ株式会社の商標で本プロジェクトは無関係である旨、**Go 版利用者への移行のお知らせ = `archive/go` の案内と破壊的変更の一覧**)、CHANGELOG | `mise run deploy:check` が通る。README の移行案内(ツール名・出力形式・認証・設定の変更点)。runbook を第三者が辿れる |

## Progress

- [ ] U1: `archive/go` の push(ユーザー)
- [x] M01: 雛形 + OAuth(詳細計画: `plans/kintone-mcp-m01-bootstrap-auth.md`)
- [ ] U2/U3: OAuth クライアント登録と利用者の設定(ユーザー)
- [ ] U4/U5: Access self-hosted アプリの作成(`/authorize` と `/callback` のパス)と AUD の取得、KV と Variables / Secrets(ユーザー)
- [x] M02: kintone クライアントと基盤(詳細計画: `plans/kintone-mcp-m02-client-and-foundation.md`)
- [x] M03: 読み取りツール + 名前解決(詳細計画: `plans/kintone-mcp-m03-read-tools-resolver.md`)
- [x] ADR 0007 の未決(書き込み系での部分一致): 完全一致のみに決定済み(2026-11-05)
- [x] M04: 書き込みツール(詳細計画: `plans/kintone-mcp-m04-write-tools.md`)
- [x] M05: デプロイ準備(詳細計画: `plans/kintone-mcp-m05-deploy-readiness.md`)
- [ ] U6/U7: 実機確認(`docs/manual-verification.md`)と Portal 登録(ユーザー)

## 未決事項

- 確定済み(2026-11-05、ユーザー決定): scope は 6 つ(ADR 0004)、書き込み系の `app_ref` と `update_key_field_ref` は完全一致のみ(ADR 0007)、環境変数は `KINTONE_HOST`(ADR 0003)、エラーコードは小文字 snake_case(ADR 0006)、利用者 ID は Access の email 必須(ADR 0010)
- ユーザー判断待ち:
  1. 同じ email の再認可による旧 grant の失効範囲(ADR 0010 は「同じ email + 同じ MCP クライアント」=ライブラリ既定)。同じ email の全クライアントを失効させる案は、Portal と Claude Desktop などを併用する人が互いに切断し合うので採っていない
- 実機確認が要る前提は `docs/manual-verification.md` の V01〜V21 / P01〜P04(PKCE、更新の挙動、リフレッシュトークン上限、IP 制限、エラーの実ステータス、`form/fields.json` の権限、Access の JWT・パス保護・Portal 経由の遷移、など)
- 製品名(リポジトリ名は `kintone`。Worker 名の既定は `kintone-mcp` 案)
