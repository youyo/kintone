import type { OAuthHelpers } from "@cloudflare/workers-oauth-provider";

/** Worker の束縛・環境変数。wrangler.jsonc と .dev.vars.example に対応する。 */
export interface Env {
  /** OAuth プロバイダ(grant・トークン・クライアント登録)の保存先 + stale 印。 */
  OAUTH_KV: KVNamespace;
  /** 接続先 kintone のホスト名(例 example.cybozu.com)。1 デプロイ = 1 ホスト(ADR 0003)。 */
  KINTONE_HOST: string;
  /** この Worker の公開 URL(オリジンのみ)。issuer・リソース識別子・kintone のリダイレクト URI の元になる。 */
  PUBLIC_BASE_URL: string;
  /** Cloudflare Access のチームドメイン(例 example-team.cloudflareaccess.com)。必須(ADR 0010)。 */
  ACCESS_TEAM_DOMAIN: string;
  /** /authorize と /callback を守る Access の self-hosted アプリの AUD タグ。必須(ADR 0010)。 */
  ACCESS_AUD: string;
  /** kintone OAuth クライアント ID(secret)。 */
  KINTONE_CLIENT_ID: string;
  /** kintone OAuth クライアントシークレット(secret)。 */
  KINTONE_CLIENT_SECRET: string;
  /** `s256`(既定)または `off`。kintone が PKCE パラメーターを拒否した場合の逃げ道(ADR 0004、V04)。 */
  KINTONE_OAUTH_PKCE?: string;
  /** OAuthProvider が defaultHandler に渡すヘルパー。 */
  OAUTH_PROVIDER: OAuthHelpers;
}

type KintoneMcpEnv = Env;

// cloudflare:workers の `env` と `Cloudflare.Env` にも同じ型を反映する。
declare global {
  namespace Cloudflare {
    interface Env extends KintoneMcpEnv {}
  }
}
