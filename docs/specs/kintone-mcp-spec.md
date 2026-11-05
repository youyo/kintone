# kintone Remote MCP サーバー仕様(Cloudflare Workers 版)

- 状態: 計画段階(2026-11-05)。実装は M01 以降。コードはまだ書いていない。同日のユーザー決定(Access の email を利用者 ID に必須化、scope 6 つ、書き込み系の `app_ref` は完全一致のみ、`KINTONE_HOST`、小文字のエラーコード)を反映済み
- 置き換える対象: Go 製 CLI + MCP サーバー v0.5.6(`archive/go` ブランチ・タグ。旧仕様は `docs/specs/kintone_spec.md`。M01 で削除する)
- 姉妹プロジェクト: `logbutler`(Backlog 用 Remote MCP。同じ構成)。この仕様の「logbutler」は `/Users/youyo/src/github.com/youyo/logbutler` を指す
- 設計判断の理由は `docs/adr/`、実機でしか確かめられない前提は `docs/manual-verification.md`(V01〜)、進め方は `plans/kintone-mcp-roadmap.md`
- 表記: 「出典: Go」は Go 版のコード(`path:line`)、「出典: 公式」は cybozu developer network の公式ドキュメント(URL を併記)、「未検証」は実機でまだ確かめていないもの(V 番号を併記)

## 1. 目的と範囲

Cloudflare Workers 上で動く Remote MCP サーバー。MCP クライアント(Cloudflare MCP Portal を想定)が OAuth で接続し、
**利用者ごとの kintone OAuth で認可された身元**で kintone REST API を呼ぶ。利用者の識別子は **Cloudflare Access の email(必須)**で、認可の入口(`/authorize` と `/callback`)を Access の self-hosted アプリで保護し、Worker が `Cf-Access-Jwt-Assertion` を検証する(ADR 0010)。それ以外の制限は Worker の外側(Cloudflare MCP Portal / WAF)で行う(ADR 0002)。

含むもの:
- 6 ツール: `kintone_apps_search` / `kintone_app_describe` / `kintone_records_query` / `kintone_record_create` / `kintone_record_update` / `kintone_record_delete`
- アプリ・フィールドの名前解決(`app` / `app_ref` / `update_key_field_ref`)

含まないもの(後のマイルストーンまたは対象外):
- 他のツール(コメント、ステータス、ファイル、バルク、カーソル API、アプリ設定の変更など)。拡張は M05 以降
- API トークン(`X-Cybozu-API-Token`)認証。**廃止**(ADR 0001)
- ゲストスペースのアプリ(ADR 0003)。1 デプロイ = 1 つの cybozu.com サブドメイン
- CLI、Docker、GoReleaser、Homebrew(新 main では廃止)

## 2. 構成

```
MCP クライアント (Portal / Claude Desktop など)
      │  OAuth 2.1 (DCR, PKCE) と MCP (Streamable HTTP, ステートレス)
      │  ※ブラウザが通る /authorize と /callback だけ Cloudflare Access (self-hosted) を経由する
      ▼
┌─ Cloudflare Worker (1 つ) ───────────────────────────────────────────────┐
│ OAuthProvider (@cloudflare/workers-oauth-provider)                       │
│   /token /register /.well-known/*   : OAuthProvider が処理               │
│   /mcp (apiRoute)  ─▶ apiHandler : createMcpHandler (リクエストごとに    │
│                        McpServer を生成。Durable Objects なし)           │
│   それ以外 ─▶ defaultHandler : Hono                                      │
│        /healthz  /authorize (GET 同意画面, POST 承認)  /callback          │
│        (/authorize と /callback は Access JWT を検証。ADR 0010)           │
│                                                                           │
│ OAUTH_KV : grant・トークン・クライアント登録 (ライブラリ管理)             │
│            + 名前解決キャッシュ `kcache:` + stale 印 `kintone-stale:`      │
│            + 冪等キャッシュ `idem:` (M04)                                   │
└──────────────┬────────────────────────────────────────────────────────────┘
               │ Authorization: Bearer <利用者の kintone アクセストークン> (grant の props から)
               ▼
        https://<KINTONE_HOST>/k/v1/...      (認可: /oauth2/authorization, トークン: /oauth2/token)
```

- 認可サーバーと MCP リソースサーバーは同居(ADR 0002)。リソース識別子は `<PUBLIC_BASE_URL>/mcp`
- Access で保護するのは `/authorize` と `/callback` だけ。`/mcp` `/token` `/register` `/.well-known/*` `/healthz` は保護しない。Access の Managed OAuth は使わない(ADR 0010)
- Durable Objects / McpAgent は使わない。MCP はステートレス
- kintone のトークンは grant の `props` にだけ置く(ライブラリが暗号化して KV に保存)。ログ・別の KV キー・MCP 応答に出さない

## 3. エンドポイント

| パス | 担当 | 内容 |
|---|---|---|
| `GET /healthz` | Hono | 正常時 200 `{"status":"ok"}`。設定不備なら 503 `{"status":"misconfigured","missing":[...],"invalid":[...]}`(項目名のみ。§9) |
| `GET /authorize` | Hono | **Access JWT を検証**(なし・不正は 403、JWKS 取得不能は 503)。同意画面(クライアント名・リダイレクト先ホスト・kintone のホスト・Access のログイン email を表示。クライアント由来の文字列はエスケープ) |
| `POST /authorize` | Hono | **Access JWT を検証** → 承認 → PKCE の verifier を生成し `beginUpstream` で state を発行(`data` に `verifier` と `accessEmail`) → kintone の認可画面へ 302。拒否 → クライアントへ `access_denied` |
| `GET /callback` | Hono | **Access JWT を検証** → `finishUpstream` で state 検証 → **JWT の email が `/authorize` と同じか確認(不一致は 403)** → kintone のコードをトークンに交換 → 疎通確認 → `completeAuthorization` |
| `POST /token`, `POST /register` | OAuthProvider | トークン発行・更新、DCR |
| `GET /.well-known/oauth-authorization-server`, `GET /.well-known/oauth-protected-resource/mcp` | OAuthProvider | メタデータ。`resource` は `<PUBLIC_BASE_URL>/mcp` |
| `POST /mcp`(`GET`/`DELETE` もライブラリ仕様に従う) | apiHandler | MCP。Worker 発行の不透明トークンで保護。クエリ `?toolsets=` `?read_only=` を解釈(§8) |

kintone 側に登録するリダイレクト URI: **`<PUBLIC_BASE_URL>/callback`**(Go 版は `/oauth/kintone/callback` だった。登録し直しが必要。出典: Go `internal/mcp/oauthcallback/handler.go:364`)。

## 4. OAuth フロー

### 4.1 MCP クライアント ↔ Worker(Worker が認可サーバー)

logbutler と同じ(logbutler ADR 0001/0002、`src/auth/authorization-server.ts`)。DCR → `/authorize`(同意) → 上流 → `/callback` → `/token`。
MCP アクセストークンは不透明トークン(`{userId}:{grantId}:{secret}`)。`iss`(RFC 9207)付きリダイレクト。

### 4.2 Worker ↔ kintone(Worker が OAuth クライアント)

| 項目 | 値 | 出典 |
|---|---|---|
| 認可エンドポイント | `https://<KINTONE_HOST>/oauth2/authorization` | Go `handler.go:72`、公式(リクエスト例)https://cybozu.dev/ja/common/docs/oauth-client/add-client/ |
| トークンエンドポイント | `POST https://<KINTONE_HOST>/oauth2/token` | Go `handler.go:75`、同上 |
| クライアント認証 | **HTTP Basic**(`Authorization: Basic base64(client_id:client_secret)`)。フォームで client_secret を送る logbutler(Backlog)と違う | Go `internal/auth/oauth/token.go:176`、公式(同上。POST body 方式の記載なし) |
| 認可リクエストのパラメーター | `response_type=code`, `client_id`, `redirect_uri`, `scope`(スペース区切り), `state`, `code_challenge`, `code_challenge_method=S256` | Go `handler.go:275-285`、公式(scope は「複数指定する場合はスペースで区切る」) |
| 認可コードの有効期間 | 10 分 | 公式(同上) |
| アクセストークンの有効期間 | 1 時間(`expires_in: 3600`) | 公式(同上) |
| リフレッシュトークン | 有効期限なし。1 クライアント・1 ユーザーあたり 10 個まで | 公式(同上) |
| リフレッシュ応答の `refresh_token` | 公式の例には含まれない(ローテーションの明記はない)。実装は両対応(応答にあれば差し替え、なければ維持) | 公式(同上)、Go `internal/auth/oauth/refresh.go:91` |
| PKCE | **公式に記載なし**。Go 版は常に `code_challenge` を送って本番運用された(拒否はされていないが、verifier が検証されているかは不明) | 公式(記載なし)、Go `handler.go:282`。未検証 V04 |
| redirect_uri の条件 | 公式に条件の記載なし。Go 版 README は https 必須としている | Go README、未検証 V05 |
| 利用者の許可 | OAuth クライアントを登録したあと、cybozu.com 共通管理の「利用者の設定」で利用者を指定する必要がある | 公式(STEP2) |
| IP アドレス制限 | cybozu.com 共通管理の IP 制限を使うなら、クライアントアプリ(= Worker)の IP を許可する必要がある。Workers の送信元 IP は固定でない | 公式(同上)。ADR 0003、未検証 V10 |
| scope(本仕様の要求) | **6 つ**: `k:app_record:read` `k:app_record:write` `k:app_settings:read` `k:app_settings:write` `k:file:read` `k:file:write`(Go 版と同じ。`handler.go:319-327`)。今のツールが使うのは先頭の 3 つで、残り 3 つは使わないが要求する(ADR 0004) | Go `handler.go:319-327`、公式 https://cybozu.dev/ja/common/docs/oauth-client/scope-kintone/ |

### 4.3 手順

1. **`POST /authorize`(承認)**: Access JWT を検証し(§4.5)、`crypto.getRandomValues` で 32 バイトの `code_verifier`(base64url)を作り、`beginUpstream(approved.request, { data: { verifier, accessEmail }, headers })` で state を発行する(`data` はライブラリが暗号化して KV に置き、`/callback` で返す。verifier は kintone にもブラウザにも送らない)。`code_challenge = base64url(SHA-256(verifier))`、方式 `S256`。kintone の認可 URL へ 302。
2. **`GET /callback`**: Access JWT を検証し(§4.5)、`finishUpstream` で state・ブラウザの束縛 cookie を検証(1 回限り・10 分)。JWT の email が `data.accessEmail` と一致しなければ 403(コード交換しない)。`error` パラメーターがあれば `access_denied`、`code` がなければ `invalid_request` でクライアントへ戻す。
3. コード交換: `POST /oauth2/token`(Basic 認証)、フォーム `grant_type=authorization_code&code=...&redirect_uri=<PUBLIC_BASE_URL>/callback&code_verifier=<verifier>`。
4. **疎通確認**: 得たアクセストークンで `GET /k/v1/apps.json?limit=1` を 1 回呼ぶ(トークンが使えること、利用者に API 権限があること、IP 制限に阻まれないことを認可の時点で確かめる)。失敗は `server_error`(5xx・通信失敗は `temporarily_unavailable`)でクライアントへ戻す。
5. `completeAuthorization({ request, userId, metadata: {}, scope: request.scope, props })`。
   - **`userId = base64url(SHA-256(trim して小文字にした Access の email))`**(43 文字、`:` を含まない)。kintone には OAuth トークンから「自分は誰か」を返す REST API がないため、Access の email が識別子の供給源(ADR 0004、ADR 0010)。UUID へのフォールバックはない。
   - `revokeExistingGrants: true`(ライブラリ既定。明示する): 同じ `userId` + 同じ MCP クライアントの旧 grant は、新 grant の保存後に失効する。別クライアントの grant は残る(ADR 0010)。
   - `props = { kintoneAccessToken, kintoneRefreshToken, kintoneExpiresAt(エポックミリ秒) }`。email は `props` にも `metadata` にも保存しない。

### 4.4 トークン更新(`tokenExchangeCallback`。ADR 0005)

logbutler ADR 0014 と同じ方式。

- MCP アクセストークンの寿命: 最大 30 分(`MCP_ACCESS_TOKEN_TTL_SECONDS = 1800`)、最小 60 秒(KV の最小 TTL)
- kintone を更新するのは、保存期限(`kintoneExpiresAt`)を過ぎているときと、stale 印(`kintone-stale:<userId>:<grantId>`、TTL 15 分)があるときだけ
- 更新で新しいアクセストークンを得た: MCP の寿命 = `clamp(残り - 60, 60, 1800)`。それ以外: `clamp(残り + 5, 60, 1800)`
- 更新エラー: `invalid_grant`(HTTP 400/401 でボディの `error` が明示的に `invalid_grant`)→ `OAuthError("invalid_grant")` で grant を失効。本文がない・JSON でない・`error` がない 4xx は `rejected` とし、grant を失効させず `server_error`(503)(プロキシや WAF の一時的な拒否で再認可にしないため。実際の kintone の応答形は V07 で確認)。`invalid_client` / `unauthorized_client` は運用者の設定ミスなので grant を失効させず `server_error`(503)。5xx・429・通信失敗 → `temporarily_unavailable`(503)
- `/mcp` で kintone が 401 を返したら、stale 印を置き、MCP の応答も HTTP 401 + `WWW-Authenticate: Bearer ... error="invalid_token" ... resource_metadata="<origin>/.well-known/oauth-protected-resource/mcp"` にしてクライアントに更新させる
- 診断ログ(値は出さない): `kintone_token`(`grant_type`, `expires_in`, `refresh_token_rotated`)、`token_exchange`、`kintone_unauthorized`

### 4.5 Cloudflare Access の身元(ADR 0010)

- 対象: `GET /authorize` `POST /authorize` `GET /callback`。ヘッダー `Cf-Access-Jwt-Assertion`
- JWKS: `https://<ACCESS_TEAM_DOMAIN>/cdn-cgi/access/certs`(isolate 内で 5 分キャッシュ。未知の `kid` は取得から 60 秒以上たっていれば取り直す)
- 検証: RS256 の署名、`iss` = `https://<ACCESS_TEAM_DOMAIN>`、`aud` に `ACCESS_AUD` を含む(`aud` は配列)、`exp` 必須、`email` が空でない文字列、`sub` あり
- 失敗: ヘッダーなし・検証失敗 → 403 `Forbidden`(理由は応答に出さない)。JWKS を取得できない → 503(fail closed)。ログには理由の種別だけを出し、JWT・email は出さない
- 出典: logbutler の git 履歴 `src/auth/access-jwt.ts`(`8dac72c^`)、awsbridge `docs/specs/architecture.md`(`aud` が配列、`email` claim)。Access 側の設定(パス指定、`email` claim が IdP によらず入るか、`/callback` のリダイレクトで JWT が付くか)は未検証(V17〜V20、P04)

## 5. ツール

共通:
- 名前は `kintone_<resource>_<action>`。入力引数のキーは snake_case(Go 版のスキーマを踏襲)。annotation の `title` は日本語、区分は logbutler の 4 種(ReadOnly / WriteIdempotent / WriteNonIdempotent / Destructive)
- 出力は JSON 文字列を text content 1 件で返す(`structuredContent` は使わない)。成功時は **データそのもの**(Go 版の `{"ok":true,"data":...}` の `data` 部分)。エラーは §6
- 出力のキー: kintone の生データはそのまま(camelCase / kintone のフィールドコードのまま)、加工した出力は snake_case(Go 版の出力形状と同じ。下記)
- 読み取りツールには任意引数 `max_response_tokens`(整数。既定 50000、0 で無効、負は `argument_error`)が付く。書き込みツールには付けない(§8、ADR 0008)
- 「`app` と `app_ref` は排他で、どちらか必須」: `app`(正の整数)または `app_ref`(文字列)のちょうど 1 つ。違反は `argument_error`(Go `internal/service/operations/resolver.go:19-35`)
- `app_ref` の解決順序は §7。**書き込みツール(`record_create` / `record_update` / `record_delete`)の `app_ref` は完全一致(ID・コード・名前)だけで解決し、部分一致のみのときは `resolver_app_exact_match_required` で候補を返す**(読み取りツールは部分一致も使う)

### 5.1 `kintone_apps_search`(ReadOnly、toolset `app`)

`GET /k/v1/apps.json`(scope `k:app_settings:read`)。出典: Go `internal/mcp/facade/apps_search.go`、`internal/kintoneapi/apps.go:48-73`。

| 引数 | 型 | 説明 |
|---|---|---|
| `ids` | number[] | アプリ ID(最大 100) |
| `codes` | string[] | アプリコード(1〜64 文字、完全一致・大文字小文字を区別。最大 100) |
| `name` | string | アプリ名の部分一致(64 文字以内、大文字小文字を区別しない) |
| `space_ids` | number[] | スペース ID(最大 100) |
| `limit` | number | 1〜100(省略時 100) |
| `offset` | number | 0 以上(省略時 0) |

制約の出典: 公式 https://cybozu.dev/ja/kintone/docs/rest-api/apps/get-apps/ 。出力: `{"apps":[{"appId","code","name","description","spaceId","threadId","createdAt","creator","modifiedAt","modifier"}, ...]}`(kintone の生データ)。1 回の呼び出しで 1 ページだけ取る。続きは `offset` で呼び直す。

### 5.2 `kintone_app_describe`(ReadOnly、toolset `app`)

`GET /k/v1/app.json` + `GET /k/v1/app/form/fields.json`(scope `k:app_settings:read`)。出典: Go `app_describe.go`(operations)。

| 引数 | 型 | 説明 |
|---|---|---|
| `app` / `app_ref` | | 上記 |
| `lang` | enum | `ja` `en` `zh` `user` `default` |

出力: `{"app":{"app_id","code","name","description","space_id","thread_id","created_at","creator","modified_at","modifier"},"fields":{<フィールドコード>:{...kintone の生定義}},"revision":"..."}`(`app` の中は snake_case に加工、`fields` は生)。空の値は省略(Go の `omitempty`)。常に最新を取り(キャッシュを読まない)、フィールドのキャッシュを更新する(§10)。
`form/fields.json` の必要権限(アプリ管理権限が要るか)は未検証(V09)。

### 5.3 `kintone_records_query`(ReadOnly、toolset `record`)

`GET /k/v1/records.json`(scope `k:app_record:read`)。出典: Go `records_query.go`、`kintoneapi/records.go:26-46`。

| 引数 | 型 | 説明 |
|---|---|---|
| `app` / `app_ref` | | 上記 |
| `query` | string | kintone クエリ言語。`limit`(最大 500、既定 100)と `offset`(最大 10,000)もここに書く |
| `fields` | string[] | 取得するフィールドコード。**最大 100**(クエリ文字列では添字 0〜99 まで) |
| `total_count` | boolean | true で件数も返す |

出力: `{"records":[...kintone の生レコード...],"total_count":N}`(`total_count` は指定時のみ、数値に変換)。1 回の呼び出しで 1 リクエスト。自動ページングはしない(続きは `query` の `offset` で呼び直す)。出典(制限値): 公式 https://cybozu.dev/ja/kintone/docs/rest-api/records/get-records/ 。
GET の URL 長の上限は公式に記載なし(未検証 V11)。

### 5.4 `kintone_record_create`(WriteNonIdempotent、toolset `record`)

`POST /k/v1/records.json`(scope `k:app_record:write`)。出典: Go `record_create.go`、公式 https://cybozu.dev/ja/kintone/docs/rest-api/records/add-records/ 。

| 引数 | 型 | 説明 |
|---|---|---|
| `app` / `app_ref` | | 上記 |
| `record` | object | 1 件。`{"<フィールドコード>":{"value":...}}`。`records` と排他 |
| `records` | object[] | 複数件。**1〜100 件**(超過は kintone へ送る前に `argument_error`)。`record` と排他 |

出力: `{"ids":[数値...],"revisions":[数値...]}`(kintone は文字列で返すので数値化)。全件成功か全件取り消し(公式)。リトライしない(§8)。

### 5.5 `kintone_record_update`(WriteIdempotent、toolset `record`)

`PUT /k/v1/record.json`(scope `k:app_record:write`)。出典: Go `record_update.go`、`kintoneapi/record_write.go:35-81`、公式 https://cybozu.dev/ja/kintone/docs/rest-api/records/update-record/ 。

| 引数 | 型 | 説明 |
|---|---|---|
| `app` / `app_ref` | | 上記 |
| `id` | number | 対象レコード ID。`update_key_*` と排他 |
| `update_key_field` | string | updateKey のフィールドコード(重複禁止の文字列 1 行または数値のみ。公式) |
| `update_key_field_ref` | string | updateKey のフィールド参照(コード / ラベルの完全一致のみ。部分一致のみなら `resolver_field_exact_match_required`。ADR 0007)。`update_key_field` と排他 |
| `update_key_value` | string | updateKey の値 |
| `revision` | number | 楽観ロック。整数(-1 は検証なし。公式) |
| `record` | object | **必須**。更新するフィールド。空オブジェクトは `argument_error` |

`id` と `update_key_*` はちょうど一方。出力: `{"revision":N}`。リトライしない。

### 5.6 `kintone_record_delete`(Destructive、toolset `record`)

`DELETE /k/v1/records.json`(scope `k:app_record:write`)。本文は JSON。出典: Go `record_delete.go`、`kintoneapi/records_write.go:75-99`、公式 https://cybozu.dev/ja/kintone/docs/rest-api/records/delete-records/ 。

| 引数 | 型 | 説明 |
|---|---|---|
| `app` / `app_ref` | | 上記 |
| `ids` | number[] | **必須**。1〜100 件、各要素は正の整数 |
| `revisions` | number[] | 任意。指定時は `ids` と同じ長さ |

出力: `{"deleted":N}`(`N = ids.length`。kintone は全件成功か全件取り消しなので成功時は正確)。リトライしない。

## 6. 出力・エラー形式(ADR 0006)

エラーはすべて `isError: true` の text content 1 件に JSON を入れる。

```json
{"schema_version":"1","error":{"code":"resolver_app_ambiguous","message":"...","retryable":false,"details":{"kind":"app","ref":"顧客","candidates":[{"id":"12","code":"CRM","name":"顧客管理"}]}}}
```

`details` は logbutler 形式への**拡張**(任意)。キーは `http_status` / `kintone_code` / `kintone_id` / `retry_after_sec` / `kind` / `ref` / `candidates`。

| HTTP / 事象 | `code` | `retryable` | Go 版の code |
|---|---|---|---|
| 401 | `authentication_error`(+ 本物の HTTP 401 に変換。§4.4) | false | `KINTONE_UNAUTHORIZED` / `AUTH_REQUIRED` |
| 403 | `permission_error` | false | `KINTONE_FORBIDDEN` |
| 404 | `not_found` | false | `KINTONE_NOT_FOUND` |
| 400, 422, kintone コード接頭辞 `CB_VA` | `argument_error` | false | `KINTONE_VALIDATION` |
| 429(リトライ後) | `rate_limited` | **true** | `KINTONE_RATE_LIMITED` |
| その他の 4xx | `api_error` | false | `KINTONE_VALIDATION`(Go は 4xx をまとめていた) |
| 5xx | `api_error` | **true** | `KINTONE_INTERNAL` |
| 通信失敗・タイムアウト | `api_error` | true | `KINTONE_NETWORK` |
| 呼び出し回数の上限超過 | `api_error` | false | (なし) |
| ツール入力の検証エラー | `argument_error` | false | `INVALID_PARAMS` |
| 応答が上限を超えた(`max_response_tokens`、または kintone の応答本文が 10 MiB 超) | `response_too_large` | false | (なし) |
| アプリ参照が見つからない | `resolver_app_not_found` | false | `RESOLVER_APP_NOT_FOUND` |
| アプリ参照が複数に一致 | `resolver_app_ambiguous`(`details.candidates`) | false | `RESOLVER_APP_AMBIGUOUS` |
| `update_key_field_ref` がラベルの部分一致のみ(1 件でも複数件でも。完全一致なし) | `resolver_field_exact_match_required`(`details.candidates` に部分一致の候補) | false | (なし。Go 版は採用していた) |
| **書き込みツール**でアプリ参照が部分一致のみ(1 件でも複数件でも。完全一致なし) | `resolver_app_exact_match_required`(`details.candidates` に部分一致の候補) | false | (なし。Go 版は採用していた) |
| フィールド参照が見つからない | `resolver_field_not_found` | false | `RESOLVER_FIELD_NOT_FOUND` |
| フィールド参照が複数に一致 | `resolver_field_ambiguous`(`details.candidates`) | false | `RESOLVER_FIELD_AMBIGUOUS` |
| 名前検索の結果が上限(1,000 件)に達した | `resolver_app_list_too_large` | false | `RESOLVER_APP_LIST_TOO_LARGE`(Go は 10,000 件) |
| 想定外の例外 | `generic_error`(メッセージは固定文) | false | `INTERNAL` |

- Access の JWT 不正(`/authorize` `/callback`)はツールのエラーではなく、HTTP の 403 / 503(§4.5)
- Go 版で廃止するコード: `STORE_*`、`SIGNING_KEY_REQUIRED`、`RESOLVER_PRINCIPAL_NOT_FOUND`、`AUTH_REQUIRED` の `authorize_url`(Worker では HTTP 401 で再認可させる)、`USAGE`(CLI)
- Go 版の `{"ok":true,"data":...}` / `{"ok":false,"error":...}` を使っていたクライアントは**壊れる**(成功は `ok`/`data` の包みがなくなり、失敗は `isError: true` と新しい形になる)
- kintone のエラー本文は `{code, id, message}`(出典: Go `kintoneapi/transport.go` の `buildAPIError`)。`message` にトークン・Authorization ヘッダーを含めない。kintone が返す `message` はそのまま載せてよい(利用者向けの文言)
- 400 / 401 / 403 / 404 / 520 などの kintone の実際のステータスとコードの対応は未検証(V07)

## 7. 名前解決

出典: Go `internal/resolver/app.go`、`field.go`、`errors.go`。

### アプリ(`app_ref`)

1. 空文字は `argument_error`。
2. **正の整数として読める文字列**: そのままアプリ ID(存在確認はしない。`app.go:33-35`)。数字だけのアプリコードがあっても ID が優先される。両モード共通。
3. **コードの完全一致**: `GET /k/v1/apps.json?codes[0]=<ref>&limit=100`(コードは大文字小文字を区別して完全一致。公式)。1 件 → 採用、2 件以上 → `resolver_app_ambiguous`、0 件 → 次へ。
4. **名前**: `GET /k/v1/apps.json?name=<ref>&limit=100&offset=...` を 100 件ずつ、**最大 10 ページ**(Go は 100 ページ)。kintone 側は部分一致・大文字小文字を区別しない。10 ページ全部が満杯 → `resolver_app_list_too_large`。
   - 取得結果から、名前が `ref` に**完全一致**(大文字小文字を区別)するものを探す。1 件 → 採用、2 件以上 → `resolver_app_ambiguous`。
   - 完全一致が 0 件なら**部分一致の結果**で判定(モードで分かれる。ADR 0007):
     - **読み取りモード**(`app_describe` / `records_query`): 1 件 → 採用、2 件以上 → `resolver_app_ambiguous`、0 件 → `resolver_app_not_found`
     - **書き込みモード**(`record_create` / `record_update` / `record_delete`): 1 件以上 → **採用せず** `resolver_app_exact_match_required`(`details.candidates` に部分一致の候補)、0 件 → `resolver_app_not_found`。完全一致が 2 件以上のときは両モードとも `resolver_app_ambiguous`
5. 各段階でヒットしたら即採用し、次の段階にはフォールバックしない。
6. `details.candidates` は `[{"id","code","name"}]`(空の項目は省略)。`not_found` の `candidates` は空配列。

### フィールド(`update_key_field_ref`)

`GET /k/v1/app/form/fields.json?app=<id>` の `properties`(トップレベルのみ。サブテーブル内のフィールドは探さない)から:
1. キー(フィールドコード)の完全一致 → 採用
2. `label` の完全一致 → 1 件採用、2 件以上 `resolver_field_ambiguous`
3. `label` の部分一致(大文字小文字を区別) → 1 件採用、2 件以上 `resolver_field_ambiguous`
4. なし → `resolver_field_not_found`

`candidates` は `[{"code","label"}]`(コード順)。

### 注意(ADR 0007)
- 部分一致の単一ヒットの自動採用は読み取り系だけ。書き込み系(特に `kintone_record_delete`)は完全一致のみで、意図しないアプリに当たらない(ユーザー決定、ADR 0007)。フィールド参照(`update_key_field_ref`)も完全一致のみ(ユーザー決定、ADR 0007)。
- 書き込みモードでも API の呼び出し回数・順序は読み取りと同じ(§8 の式は変わらない)。
- `apps.json` は、ゲストスペースを指定しない呼び出しだと閲覧可能なゲストスペース内のアプリも返す(公式)。名前解決で採用したアプリがゲストスペースのものだと、以降の `/k/v1/...` 呼び出しは失敗する(ADR 0003 の既知の限界)。

## 8. 制限値とクライアント動作(ADR 0008)

### kintone クライアント(logbutler ADR 0003 を踏襲)

| 項目 | 値 | 根拠 |
|---|---|---|
| 同時リクエスト | 1(1 回のツール呼び出し内は直列) | kintone の同時接続上限はドメインあたり 100(出典: https://kintone.dev/en/tutorials/development-productivity/how-to-avoid-kintone-rest-api-limits/ 。超えると 429)。直列にして他の利用者を巻き込まない |
| タイムアウト | 30 秒 / リクエスト | Go `kintoneapi/client.go:39` と同じ |
| 読み取りのリトライ | 429・503 を最大 2 回。`Retry-After` があれば従う(上限 10 秒)、なければ 1 秒待つ | Go は 3 回試行・バックオフ 0.5〜5 秒(`transport.go:32-36`)。Workers の実時間を考えて logbutler に合わせる。`Retry-After` の有無は未検証 V08 |
| **書き込みのリトライ** | **しない**(429・503 も 1 回で打ち切り) | Go `transport.go:219,228`。多重作成・多重更新を避ける |
| 呼び出し回数の上限 | 50 回 / ツール呼び出し(リトライ含む) | logbutler ADR 0003。Workers 無料プランの subrequest 上限と同じ |
| 応答本文の読み取り上限 | 10 MiB。超えたら `response_too_large` | Go は 32 MiB(`transport.go`)。Workers のメモリを考えて縮小 |
| ページ上限 | アプリ名検索 10 ページ × 100 件 | 下記の式 |

### 最悪ケースの呼び出し回数(論理呼び出し数。リトライは別に最大 2 回/呼び出し)

`A` = アプリ解決の最悪 = コード 1 + 名前 10 = 11、`F` = フィールド解決 1。

| ツール | 式 | 最悪 | リトライ込み(読み取りは × 3) | `maxApiCalls` |
|---|---|---|---|---|
| `kintone_apps_search` | 1 | 1 | 3 | 50(既定) |
| `kintone_app_describe` | A + app 1 + fields 1 | 13 | 39 | 50 |
| `kintone_records_query` | A + 1 | 12 | 36 | 50 |
| `kintone_record_create` | A + 1(書き込みはリトライなし) | 12 | 12 | 50 |
| `kintone_record_update` | A + F + 1 | 13 | 13 | 50 |
| `kintone_record_delete` | A + 1 | 12 | 12 | 50 |

どのツールも「論理 13 回 × 3 = 39 ≤ 50」に収まる。実装(M02/M03)で、モックが全ページ満杯を返す最悪ケースの呼び出し回数をテストで固定する。
KV の読み書きが subrequest の上限に数えられるかは未検証(V12)。

### 応答サイズ(logbutler ADR 0007)

読み取りツールの応答 JSON の推定トークン数(ASCII は 4 文字 1 トークン切り上げ、ASCII 以外は 1 文字 1 トークン)が `max_response_tokens`(既定 50000)を超えたら、切り詰めず `response_too_large`。書き込み系は対象外(成功した書き込みを失敗に見せないため)。

### URL クエリ(logbutler ADR 0008)

`/mcp?toolsets=app,record&read_only=true`。`toolsets` は `app` `record`(既定 `all`)、`read_only=true` で書き込み・破壊的ツールを隠す。不正値・綴り違いのキー(`readonly` など)・公開ツールが 0 個になる組み合わせは 400 `{"error":"invalid_query","error_description":"..."}`。

## 9. 設定項目(ADR 0003・0004・0009・0010)

| 名前 | 種別 | 内容 |
|---|---|---|
| `OAUTH_KV` | KV binding | OAuthProvider の保存先 + キャッシュ + 印 |
| `KINTONE_HOST` | var | 接続先のホスト名(例 `example.cybozu.com`)。1 デプロイ = 1 ホスト。スキーム・パス不可。名前は確定(ユーザー決定) |
| `PUBLIC_BASE_URL` | var | この Worker の公開 URL(https のオリジンのみ。ローカルのみ http の loopback 可) |
| `KINTONE_CLIENT_ID` | secret | kintone の OAuth クライアント ID |
| `KINTONE_CLIENT_SECRET` | secret | 同シークレット |
| `ACCESS_TEAM_DOMAIN` | var(**必須**) | Cloudflare Access のチームドメイン(例 `example-team.cloudflareaccess.com`)。JWKS と `iss` の検証に使う。スキーム・パス不可(ADR 0010) |
| `ACCESS_AUD` | var(**必須**) | `/authorize` と `/callback` を守る Access の self-hosted アプリの AUD タグ(ADR 0010) |
| `KINTONE_OAUTH_PKCE` | var(任意) | `s256`(既定)または `off`。kintone が PKCE パラメーターを拒否した場合の逃げ道(V04) |

`checkConfig(env)`(logbutler ADR 0010 と同じ。リクエストごと、KV 読み取りなし):

| 項目 | 欠け(`missing`) | 使えない(`invalid`) |
|---|---|---|
| `OAUTH_KV` | KV namespace でない | - |
| `KINTONE_HOST` | 未定義・空・空白のみ | プレースホルダ(`<...>`)が残る。ホスト名の形式でない(スキーム・パス・空白を含む) |
| `PUBLIC_BASE_URL` | 同上 | プレースホルダが残る。https のオリジンでない(パス・クエリ・フラグメント付き。http は loopback のみ可) |
| `KINTONE_CLIENT_ID` / `KINTONE_CLIENT_SECRET` | 同上 | プレースホルダが残る |
| `ACCESS_TEAM_DOMAIN` | 未定義・空・空白のみ | プレースホルダが残る。ホスト名の形式でない(スキーム・パス・空白を含む) |
| `ACCESS_AUD` | 同上 | プレースホルダが残る(形式は検査しない) |
| `KINTONE_OAUTH_PKCE` | (任意) | 設定されていて `s256` / `off` 以外 |

欠け・不正があるとき `/healthz` 以外は 500 `{"error":"server_misconfigured"}`、`/healthz` は 503 + 項目名。値は応答にもログにも出さない。**`ACCESS_TEAM_DOMAIN` / `ACCESS_AUD` が未設定のとき、UUID などで代替して認可を通すことはしない**(ADR 0010)。

デプロイ設定の生成(logbutler ADR 0011): 環境変数 `KINTONE_MCP_WORKER_NAME` / `KINTONE_MCP_KV_NAMESPACE_ID` / `KINTONE_MCP_HOST` / `KINTONE_MCP_PUBLIC_BASE_URL` / `KINTONE_MCP_ACCESS_TEAM_DOMAIN` / `KINTONE_MCP_ACCESS_AUD` / `KINTONE_MCP_CUSTOM_DOMAIN`(任意)から `wrangler.deploy.jsonc`(gitignore)を生成して `wrangler deploy -c`。secrets は `--secrets-file`。

## 10. キャッシュ(ADR 0007)

名前解決のためだけに `OAUTH_KV` に置く。

| 項目 | 値 |
|---|---|
| キー | `kcache:v1:<userId>:<grantId>:appcode:<ハッシュ>`、`kcache:v1:<userId>:<grantId>:appname:<ハッシュ>`、`kcache:v1:<userId>:<grantId>:fields:<appId>`。ハッシュは `ref` の SHA-256 先頭 16 文字(16 進)。`userId` は Access の email のハッシュ、`grantId` は MCP トークンから取り出す。**grant 単位で分離する**(email 単位にしない。同じ email で別の kintone アカウントを認可した grant と共有しないため。ADR 0010) |
| 値 | アプリ: `{appId, code, name}` の配列(名前検索は全ページを連結した最大 1,000 件)。フィールド: `{code, label, type}` の配列 |
| TTL | 600 秒(KV の最小 60 秒以上) |
| 読む | アプリ・フィールドの名前解決。`kintone_apps_search` と `kintone_app_describe` は読まない(常に最新) |
| 書く | 名前解決で取得したとき。`kintone_app_describe` が取得したフィールド定義でフィールドのキャッシュも更新 |
| 無効化 | (1) TTL。(2) キャッシュ由来の解決で `not_found` になったら、**キャッシュを無視して 1 回だけ取り直す**(取り直しも最悪ケースの式の範囲内)。(3) キャッシュで解決したアプリへの呼び出しが `not_found` を返したら、そのキャッシュを消す。(4) 手動の削除手段は持たない |
| 失敗時 | KV の読み書き失敗はツール呼び出しを止めない(キャッシュなしで続行。例外名だけログ) |

Go 版は TTL 1 年・キーにドメインだけ(利用者を含まない。出典: Go `internal/store/cache.go:10-12`、`internal/service/api/caching.go:74,98,139-186`)で、`cache clear` CLI で無効化していた。Remote MCP には無効化手段がなく、アプリの見え方は kintone アカウントごとに違うため、短い TTL と grant 別キーに変える。冪等キャッシュ(`idem:<userId>:<grantId>:...`、M04)と stale 印(`kintone-stale:<userId>:<grantId>`)も grant 単位。

## 11. 既知の限界

- **利用者の識別子(Access の email)が示すのは「Access にログインした人」で、「kintone で認可したアカウント」ではない**。同意画面のあとの kintone の認可画面で別のアカウントでログインしても、Worker は気づけない(kintone に持ち主を返す口がない。ADR 0010)。そのためキャッシュ・冪等キー・stale 印は grant 単位のままで、email 単位にしない。緩和策は kintone 側の「利用者の設定」と同意画面の注意書きだけ
- 同じ email・同じ MCP クライアントの再認可は旧 grant を失効させて置き換える(別クライアントの grant は残る。DCR で毎回登録し直すクライアントの旧 grant は失効しない)。kintone 側のリフレッシュトークンは取り消されない(取り消しの口は未確認。V21)ので、10 個上限(公式)を超えたときの kintone の挙動は未検証(V06)で、置き換えが効くのは「kintone が古いトークンから失効させる」場合だけ
- Access から外した利用者の既存 grant は残る(失効は手動。runbook、M05)。Access の JWT は長寿命になりうる(awsbridge の観測で約 30 日)が、本仕様で JWT を使うのは認可の瞬間だけ
- cybozu.com 共通管理の IP 制限を使う環境では、Workers の送信元 IP を許可できず使えない可能性が高い(V10)
- ゲストスペースのアプリは扱えない(ADR 0003)
- `records_query` は自動ページングなし。`offset` は kintone の上限 10,000 まで(それ以上はカーソル API が必要。M05 以降)
- 並列リフレッシュ時に、同じ MCP リフレッシュトークンを複数プロセスが同時に使うと 1 つだけが有効になる(logbutler ADR 0001 の V02 の観測と同じ。ライブラリの挙動)
