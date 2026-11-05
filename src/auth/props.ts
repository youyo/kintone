/**
 * grant の props。kintone のトークンは「ここにだけ」保存する(OAuthProvider が暗号化して KV に置く)。
 * ログ・別の KV キー・エラーメッセージ・MCP の応答に出してはいけない。
 * userId も email も持たない(userId は MCP トークンから取り出す。email は保存しない。ADR 0010)。
 */
export interface GrantProps {
  kintoneAccessToken: string;
  kintoneRefreshToken: string;
  /** kintone アクセストークンの期限(エポックミリ秒)。 */
  kintoneExpiresAt: number;
}
