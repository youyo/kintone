# ADR 0001: Worker 自身を OAuth 認可サーバーにし、認証を利用者ごとの kintone OAuth に一本化する(API トークン廃止)

- 状態: 提案(M01 で採用予定。ユーザー決定済みの方針を文書化したもの)
- 日付: 2026-11-05
- 関連: logbutler ADR 0001、ADR 0002(構成)、ADR 0004(kintone OAuth の仕様適用)、ADR 0005(トークン更新)、ADR 0010(利用者の識別子と Access)

## 状況

Go 版 v0.5.6 は認証を二系統に分けていた: CLI / stdio は API トークン(`X-Cybozu-API-Token`。Go `internal/auth/apitoken.go`)、Remote MCP は OIDC(idproxy)で利用者を識別してから kintone OAuth(`internal/mcp/oauthcallback/`)。
Remote MCP を Cloudflare Workers に作り直す。Workers 版では idproxy(Go ライブラリ)も OIDC も持たず、利用者の制限は Worker の外側(Cloudflare MCP Portal / Access / WAF)で行い、認可の入口(`/authorize` `/callback`)だけは Access の self-hosted アプリで保護して、Access の email を利用者の識別子にする(ADR 0002、ADR 0010)。
kintone の OAuth は Authorization Code(Confidential Client)で、アクセストークンの寿命は 1 時間、リフレッシュトークンに有効期限はない(公式 https://cybozu.dev/ja/common/docs/oauth-client/add-client/)。
MCP クライアント(Portal を含む)は、上流のトークンが切れたとき自動で更新する手段を持たない(logbutler ADR 0001 の調査)。

## 決定

1. Worker が MCP クライアントに対する OAuth 認可サーバーになる(`@cloudflare/workers-oauth-provider` の `OAuthProvider`)。上流の IdP は kintone OAuth 2.0 で、Worker は kintone に対して OAuth クライアントとして振る舞う。
2. **認証は利用者ごとの kintone OAuth だけ**。MCP の OAuth フロー中に kintone の認可を通し、kintone のトークン(アクセス・リフレッシュ・期限)は grant の `props` にだけ置く(ライブラリが暗号化して KV に保存)。
3. MCP クライアントには Worker 発行の不透明トークン(最大 30 分)を渡す。kintone のトークンはクライアントに渡さない。
4. **API トークン(`X-Cybozu-API-Token`)経路は廃止する。復活させない**:
   - API トークンはアプリ単位の共有秘密で、誰が操作したかが kintone の履歴(作成者・更新者)に「API トークンの Administrator」として残り、利用者ごとの権限制御ができない(公式の認証ページ: https://cybozu.dev/ja/kintone/docs/rest-api/overview/authentication/ 。レコードの言語も API トークンでは「Administrator」の表示言語になる=公式の複数レコード取得ページ)。
   - 全アプリ・全利用者で 1 つの Worker に複数のトークンを登録する設計は、秘密の置き場(Secrets の数・ローテーション)と「どの利用者がどのトークンを使えるか」の管理を Worker に持ち込む。API トークン経路には、Worker が利用者を識別する手段がない(OAuth 経路では Access の email で識別する。ADR 0004、ADR 0010)。
   - 利用者ごとの OAuth にすれば、kintone 側の権限(アプリ・レコード・フィールドのアクセス権)がそのまま効く。
   - 再導入したくなったら、この ADR を置き換える新しい ADR を先に書くこと(理由: 上のとおり、監査性と権限の分離を失う)。
5. Go 版の OIDC(idproxy)・セッション・TokenStore・多バックエンド(SQLite/Redis/DynamoDB)は持ち込まない。

## 理由

- 利用者ごとのトークンを Worker の暗号化 KV に限定でき、Portal 側にトークンが常駐しない。
- MCP トークンの寿命を kintone のアクセストークン(1 時間)より短くすれば、MCP の更新 1 回につき kintone の更新が高々 1 回で済み、並列更新の競合を避けられる(ADR 0005)。
- Go 版でも Remote MCP の本番運用は「OIDC + kintone OAuth」で、API トークンは CLI 用だった(Go README、CHANGELOG v0.3.0)。CLI を廃止するので API トークンの役割もなくなる。

## 却下した案

- **API トークンを Secrets に置いて共有する**: 上記のとおり。
- **Portal の手動 OAuth で kintone のトークンをそのまま流す(passthrough)**: logbutler ADR 0001 と同じ理由で却下(Portal はトークンを自動更新しない。kintone のアクセストークンは 1 時間で切れるため、約 1 時間ごとに再認証になる)。
- **Durable Objects / McpAgent**: 認証状態は KV で足り、MCP はステートレスで済む。

## 帰結

- Go 版の API トークン利用者(CLI・stdio)は移行できない。README で `archive/go` を案内する(M05)。
- OAuth クライアントの登録(cybozu.com 共通管理)と、利用者の許可(「利用者の設定」)が運用者の必須作業になる(ロードマップの手順メモ)。
- 1 クライアント・1 ユーザーあたりのリフレッシュトークンは 10 個まで(公式)。MCP クライアントの接続(認可)ごとに 1 つ消費する可能性がある。同じ email・同じ MCP クライアントの再認可は旧 grant を失効させて置き換える(ADR 0010)が、kintone 側のトークンは取り消されない。超えたときの挙動は未検証(V06)。

## 未検証の前提

- リフレッシュトークンの 10 個上限を超えたとき、古いものが失効するのか新規が拒否されるのか(V06)
- 利用者の許可がない利用者の認可がどこで・どう失敗するか(V03)
