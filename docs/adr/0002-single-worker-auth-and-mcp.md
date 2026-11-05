# ADR 0002: 認可サーバーと MCP を 1 つの Worker に同居させる

- 状態: 採用(M01 で実装)。**決定 2(Access 連携を持たない)は ADR 0010 で置き換えた**(2026-11-05)
- 日付: 2026-11-05
- 関連: logbutler ADR 0002、logbutler ADR 0009、ADR 0001、ADR 0010

## 状況

`@cloudflare/workers-oauth-provider` v1 は、認可サーバーと MCP リソースサーバーを別 Worker に置く構成と、`OAuthProvider` 1 つで兼ねる同居構成を提供する(logbutler ADR 0002)。
利用者の制限について、logbutler は当初 Cloudflare Access の JWT を Worker で検証していたが、Access Managed OAuth は「自前の OAuth を持つサーバーには使わない」と公式に明記されているため削除した(logbutler ADR 0009)。本プロジェクトでは、kintone に OAuth トークンの持ち主を返す口がなく、利用者の識別子を Access の email に頼るため、self-hosted アプリでブラウザの 2 パスを守る構成を採る(ADR 0010。Managed OAuth は使わない)。

## 決定

1. `OAuthProvider` による同居構成にする。`apiRoute: "/mcp"` → ステートレスな `createMcpHandler`(リクエストごとに `McpServer` を生成)、それ以外 → Hono の `defaultHandler`(`/authorize` `/callback` `/healthz`)。`createMcpHandler` の戻り値をそのまま default export せず、`{ fetch }` から `OAuthProvider` を呼ぶ(issuer を `PUBLIC_BASE_URL` から作るため、最初のリクエストで生成して isolate 内で使い回す)。
2. **Cloudflare Access 連携は `/authorize` と `/callback` だけに持つ**(ADR 0010: `Cf-Access-Jwt-Assertion` の検証、`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD`、`/callback` での email 一致確認)。`/mcp` `/token` `/register` `/.well-known/*` `/healthz` は Access で保護しない。それ以外の制限(MCP Server Portal のポリシー、WAF、レート制限)は外側で行う。
3. 同意画面(クライアント名・リダイレクト先ホスト・kintone のホストの表示)は残す(confused deputy 対策)。

## 理由

- logbutler と同じ理由: MCP リソースは 1 つ、認可サーバーも 1 つで、分離のメリットがない。Worker・Service Binding・Access アプリが増える運用コストに見合わない。
- kintone のトークンを含む `props` を復号できるのが認可サーバーだけという制約が、同居なら自然に満たされる。
- Access 連携は logbutler の M01 の実装(git 履歴の `src/auth/access-jwt.ts`)を移植できる。ただし logbutler が M06 で外した理由とは別の構成である(ADR 0010)。

## 帰結

- `/register`(DCR)は誰でも匿名で呼べる。匿名の DCR でも KV にクライアント登録が書き込まれる。レート制限は Worker では実装せず、前段の Cloudflare WAF / Rate Limiting で行う(runbook に書く。M05)。
- `/authorize` と `/callback` は Access の self-hosted アプリのポリシーを通った人だけが到達でき、Worker も JWT を検証する(ADR 0010)。有効な grant を作るには、Access のポリシーを通ることと、kintone のログイン(と、cybozu.com 共通管理の「利用者の設定」で許可された利用者だけが OAuth クライアントを使えること。公式)の両方が要る。
- Worker の URL へ直接接続して Portal のポリシーを回避しても、`/authorize` は Access の JWT がなければ 403 になる。ただし `/mcp` は、Worker 発行のトークンを持っていれば Portal を通らずに到達できる。
- 保護リソースメタデータの URL は `/.well-known/oauth-protected-resource/mcp`(resource のパス付き)。

## 却下した案

- 認可サーバーと MCP の分離(`OAuthAuthorizationServer` + `OAuthResourceServer`): 上記。リソースが増えたら移行する(`ctx.props` は両ホストで同じ)。
- Access 連携を Worker に持たない(草案): 利用者の識別子を作れない(ADR 0010 で置き換えた)。

## 未検証の前提

- Cloudflare MCP Portal が DCR / CIMD のどちらで接続してくるか、クエリ付き URL を保持するか(logbutler の P1〜P4 と同種。本リポジトリの P01〜P03)
- Portal 経由のブラウザ遷移で Access の cookie・JWT が付くか(P04。ADR 0010)
