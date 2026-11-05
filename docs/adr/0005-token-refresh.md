# ADR 0005: kintone トークンの更新方式(logbutler ADR 0014 を踏襲)

- 状態: 採用(M01 で実装。ユーザー決定「logbutler の `token-exchange.ts` / `backlog-stale.ts` / ADR 0014 と同じ方式」)
- 日付: 2026-11-05
- 関連: ADR 0001、ADR 0004、logbutler ADR 0001(未検証 1・3・5)、logbutler ADR 0014

## 状況

logbutler は Backlog が「有効期限内の更新では同じアクセストークンを残り寿命つきで返し、期限は延びない」ことを実機で観測し、ADR 0014 で「失効後にだけ更新し、MCP の寿命を Backlog の残り寿命に合わせる」方式にした。
kintone について確認できた事実: アクセストークン 1 時間、リフレッシュトークン無期限、更新応答に `refresh_token` が含まれない例(公式 add-client ページ)。**期限内の更新で同じトークンが返るかは未検証**。Go 版の `Authenticator` は skew 60 秒前に更新していた(`internal/auth/oauth/provider.go`)ので、Go 版はこの問題を観測しない設計だった。

## 決定

logbutler の方式をそのまま移す。kintone に合わせて変えるのは、トークンエンドポイント(Basic 認証、ADR 0004)と名前(`backlog*` → `kintone*`)だけ。

1. `tokenExchangeCallback` は、MCP のトークン発行・更新(`authorization_code` と `refresh_token` の grant)のたびに呼ばれる。
2. kintone を更新するのは次のときだけ: 保存期限(`kintoneExpiresAt`)を過ぎている(残り 0 以下)、または stale 印がある。期限内は更新しない。
3. MCP アクセストークンの寿命:
   - 新しい kintone トークンを得た: `clamp(残り - 60, 60, 1800)`
   - それ以外: `clamp(残り + 5, 60, 1800)`
4. stale 印: `/mcp` で kintone が 401 を返したら `kintone-stale:<userId>:<grantId>`(値 `1`、TTL 15 分)を KV に置き、MCP の応答も HTTP 401 + `invalid_token` にする。次の MCP トークン更新で期限に関係なく kintone を更新し、成功したら印を消す。
5. エラー分類(`tokenExchangeCallback` 内):
   - トークンエンドポイントの 400/401 で本文の `error` が明示的に `invalid_grant` → `OAuthError("invalid_grant")`(grant を失効。クライアントが再認可)
   - 本文がない・JSON でない・`error` がない 4xx は `rejected` とし、`OAuthError("server_error")`(503。grant は失効させない)。前段のプロキシや WAF の本文なしの 401 で grant が失効し、利用者が再認可になるのを避けるため。kintone の実際の応答形は V07(`docs/manual-verification.md`)で確認する
   - `invalid_client` / `unauthorized_client` → `OAuthError("server_error")`(運用者のシークレット誤り。grant は失効させない)。ライブラリの `OAuthError` が任意のコードを受けるかは M01 で `node_modules/@cloudflare/workers-oauth-provider/dist/oauth-provider.d.ts` を見て決める。受けない場合は `temporarily_unavailable` + 503(grant は残る)で代替する
   - 5xx・429・通信失敗 → `OAuthError("temporarily_unavailable", 503)`(grant は残す)
6. 更新応答の `refresh_token` は、あれば差し替え、なければ元を維持(両対応。Go `refresh.go:91` と同じ)。
7. 診断ログ(値は出さない): `kintone_token`、`token_exchange`(`ttl_basis` を含む)、`kintone_unauthorized`。

## 理由

- kintone が期限内の更新で同じトークンを返す場合でも、返さない場合でも、この方式は壊れない(期限内は呼ばず、失効後に呼ぶ。同じトークンが返っても寿命は残り + 5 秒で次の更新が失効直後に来る)。
- 「実機で観測してから設計を直した」logbutler の経緯(ADR 0001 の未検証 5)を、最初から織り込むことで繰り返さない。
- 更新が高々 1 時間に 1 回になり、トークンエンドポイントへの負荷が小さい。

## 却下した案

- **Go 版のように期限の 60 秒前に更新する**: Backlog 型の挙動なら、同じトークンが返るだけで失効の 401 が出る。kintone の挙動が未検証のうちは採らない。
- **リフレッシュトークンを MCP のリフレッシュのたびに必ず使う**: 無駄な更新が増える。

## 帰結

- 並列リフレッシュ: 同じ MCP リフレッシュトークンで同時に更新すると、新しい MCP リフレッシュトークンは 1 つだけが有効になる(ライブラリの grant の書き込みが後勝ち。logbutler の V02 の実測)。kintone がローテーションしない限り kintone 側の競合は起きない。ローテーションする場合の競合は未検証(V01)。
- リフレッシュトークンの 10 個上限(公式)を超えたときの挙動は未検証(V06)。

## 未検証の前提

- 期限内の更新で同じアクセストークンが返るか、`expires_in` が残り秒数か(V01)
- 初回(コード交換)の `expires_in` が 3600 か(V02)
- 更新応答でリフレッシュトークンがローテーションするか(V01)
- トークンエンドポイントのエラー本文の形(`error` フィールドがあるか。V07)
- kintone REST が失効トークンに 401 を返すか(V07)
- KV の結果整合性が stale 印の伝播に影響する範囲(logbutler の V02 と同種。V15)

## M01 の実装で確定・追加した点

- `OAuthError` は任意の `code` と `statusCode` を受ける(`node_modules/@cloudflare/workers-oauth-provider/dist/oauth-provider.d.ts` の `OAuthError` / `OAuthErrorOptions`)ので、`invalid_client` / `unauthorized_client`(と、再試行で直らない拒否 `rejected`)は `OAuthError("server_error", 503)`(grant は失効させない)にした。代替の `temporarily_unavailable` は不要だった。
- 更新の実挙動が未検証(V01)のため、E2E は `test/helpers/fake-kintone-oauth.ts` の 2 モード(A: 期限内の更新は同じトークンを残り寿命つきで返す / B: 毎回新しいトークン)で同じテストを回す(`test/e2e/refresh.test.ts`)。どちらでも壊れないことを固定した。
- 診断ログのフィールド名: `kintone_token`(`grant_type` `expires_in` `refresh_token_rotated`)、`token_exchange`(`kintone_remaining_seconds` `needs_refresh` `stale_marker` `refreshed` `access_token_changed` `ttl_basis`(`new_token` / `kintone_remaining`)`access_token_ttl`)、`kintone_unauthorized`(`kintone_remaining_seconds` `stale_marked`)。値は出さない。
