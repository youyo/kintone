import {
  type GrantType,
  OAuthError,
  type TokenExchangeCallbackOptions,
  type TokenExchangeCallbackResult,
} from "@cloudflare/workers-oauth-provider";
import type { Env } from "../env";
import { KintoneOAuthError, pkceMode, refreshTokens } from "./kintone-oauth";
import { clearKintoneStale, isKintoneStale } from "./kintone-stale";
import type { GrantProps } from "./props";

/**
 * MCP アクセストークンの寿命。kintone のアクセストークン(約 1 時間)より短くする。
 * こうすると MCP のリフレッシュ 1 回につき kintone のリフレッシュが高々 1 回になり、
 * 並列リフレッシュの競合を避けられる。
 */
export const MCP_ACCESS_TOKEN_TTL_SECONDS = 30 * 60;
/**
 * MCP grant(リフレッシュトークン)の最大寿命 = 7 日の**固定寿命**(ADR 0010)。
 * Access から外した利用者も、この期間を過ぎると /mcp を使えなくなる(再認可で Access の判定を通る)。
 * `refreshTokenIdleTTL` は設定しない(使い続ければ延びる方式では、期限で切る目的を果たせない)。
 * 必ず整数で `OAuthProvider` に渡す(`refreshTokenTTL: undefined` の明示は「無期限」の意味になる)。
 */
export const MCP_REFRESH_TOKEN_TTL_SECONDS = 7 * 24 * 60 * 60;
/** DCR のクライアント登録の寿命(30 日)。整数で渡す(undefined の明示は「無期限」)。 */
export const MCP_CLIENT_REGISTRATION_TTL_SECONDS = 30 * 24 * 60 * 60;
/** kintone から新しいトークンを得たとき、その期限より手前に MCP の次の更新を置く余裕。 */
const SKEW_SECONDS = 60;
/**
 * kintone のトークンが新しくならなかった(呼ばなかった・同じトークンが返った)ときに、MCP の寿命を
 * kintone の残り寿命より少しだけ長くする余裕。次の MCP の更新を kintone の失効「直後」に来させ、
 * そこで kintone から新しいトークンを得る(ADR 0005)。
 * 数秒でよい根拠: 保存期限は「応答を受けた時刻 + expires_in」なので kintone 側の実際の期限以後になる。
 * 残り秒数は floor で切り捨て、ライブラリも自分の now で秒を数えるので ±1 秒ずれる。それを吸収できれば足りる。
 * 長くすると、失効から次の更新までの間に kintone の 401(→ stale 印で回復)が出る時間が延びるだけ。
 */
export const KINTONE_EXPIRY_GRACE_SECONDS = 5;
/** KV の最小 TTL(workers-oauth-provider も 60 秒未満を拒否する)。 */
const MIN_TTL_SECONDS = 60;

export interface TokenExchangeOptions {
  now?: () => number;
}

type Callback = (
  options: TokenExchangeCallbackOptions<Env>,
) => Promise<TokenExchangeCallbackResult | undefined>;

/**
 * MCP のトークンを発行・更新するたびに呼ばれる(ADR 0005)。
 *
 * kintone が有効期限内のリフレッシュで同じアクセストークンを返すかは未検証(V01)。返す場合でも返さない場合でも
 * 壊れないよう、kintone を呼ぶのは次のときだけにする(logbutler の Backlog と同じ方式):
 * - 保存した期限(kintoneExpiresAt)を過ぎている(残り 0 以下)
 * - /mcp で kintone が 401 を返した印(kintone-stale)がある。期限に関係なく更新し、成功したら印を消す
 *
 * MCP アクセストークンの寿命:
 * - kintone から新しいトークンを得た: min(MCP_ACCESS_TOKEN_TTL_SECONDS, 残り - SKEW_SECONDS)
 * - それ以外(呼ばなかった・同じトークンが返った): min(MCP_ACCESS_TOKEN_TTL_SECONDS, 残り + KINTONE_EXPIRY_GRACE_SECONDS)。
 *   次の MCP の更新が kintone の失効直後に来て、そこで新しいトークンを得る
 * どちらも下限は KV の最小 60 秒。失効と次の更新の隙間の 401 は stale 印で回復する。
 *
 * kintone 側で失効していたら invalid_grant(grant を失効させる)、一時障害なら temporarily_unavailable(grant は残す)。
 * 応答に refresh_token があれば差し替え、なければ維持する(kintone-oauth.ts。ローテーションの有無は未検証 V01)。
 */
export function createTokenExchangeCallback(opts: TokenExchangeOptions = {}): Callback {
  const now = opts.now ?? (() => Date.now());

  return async ({ grantType, props, env, userId, grantId }) => {
    if (!isRefreshableGrant(grantType)) return undefined;
    const previous = props as GrantProps;
    let current = previous;
    let refreshed = false;

    const remainingMs = current.kintoneExpiresAt - now();
    // 期限内に更新しても同じトークンが返るだけの可能性があるので、期限切れ(残り 0 以下)のときだけ更新する。
    const needsRefresh = remainingMs <= 0;
    // /mcp で kintone が 401 を返した grant は、期限に関係なく更新する(kintone-stale.ts)。
    // 認可コードの直後は取りたてなので見ない。
    const stale = grantType === "refresh_token" && (await readStale(env.OAUTH_KV, userId, grantId));
    if (needsRefresh || stale) {
      try {
        const tokens = await refreshTokens(
          {
            host: env.KINTONE_HOST,
            clientId: env.KINTONE_CLIENT_ID,
            clientSecret: env.KINTONE_CLIENT_SECRET,
            pkce: pkceMode(env.KINTONE_OAUTH_PKCE),
          },
          current.kintoneRefreshToken,
          now(),
        );
        current = {
          ...current,
          kintoneAccessToken: tokens.accessToken,
          kintoneRefreshToken: tokens.refreshToken,
          kintoneExpiresAt: tokens.expiresAt,
        };
        refreshed = true;
      } catch (e) {
        throw toOAuthError(e);
      }
    }

    if (refreshed && stale) await clearStale(env.OAUTH_KV, userId, grantId);

    const accessTokenChanged =
      refreshed && current.kintoneAccessToken !== previous.kintoneAccessToken;
    const remainingSeconds = Math.floor((current.kintoneExpiresAt - now()) / 1000);
    const ttlBasis = accessTokenChanged ? "new_token" : "kintone_remaining";
    const accessTokenTTL = Math.max(
      MIN_TTL_SECONDS,
      Math.min(
        MCP_ACCESS_TOKEN_TTL_SECONDS,
        accessTokenChanged
          ? remainingSeconds - SKEW_SECONDS
          : remainingSeconds + KINTONE_EXPIRY_GRACE_SECONDS,
      ),
    );

    // 診断ログ(値は出さない)。access_token_changed は kintone が更新で同じトークンを返したか、
    // ttl_basis は MCP の寿命の決め方(new_token: 新しい期限 - SKEW、kintone_remaining: 残り + 余裕)。
    console.log(
      JSON.stringify({
        event: "token_exchange",
        grant_type: grantType,
        kintone_remaining_seconds: Math.floor(remainingMs / 1000),
        needs_refresh: needsRefresh,
        stale_marker: stale,
        refreshed,
        ...(refreshed ? { access_token_changed: accessTokenChanged } : {}),
        ttl_basis: ttlBasis,
        access_token_ttl: accessTokenTTL,
      }),
    );

    return refreshed
      ? { accessTokenProps: current, newProps: current, accessTokenTTL }
      : { accessTokenTTL };
  };
}

/** 印の読み取りに失敗しても MCP の更新は止めない(期限による判定だけで続ける)。 */
async function readStale(kv: KVNamespace, userId: string, grantId: string): Promise<boolean> {
  try {
    return await isKintoneStale(kv, userId, grantId);
  } catch {
    console.warn(JSON.stringify({ event: "kintone_stale_read_failed" }));
    return false;
  }
}

async function clearStale(kv: KVNamespace, userId: string, grantId: string): Promise<void> {
  try {
    await clearKintoneStale(kv, userId, grantId);
  } catch {
    // 消し損ねても、次の更新で kintone をもう一度更新するだけ(TTL で消える)。
    console.warn(JSON.stringify({ event: "kintone_stale_clear_failed" }));
  }
}

function isRefreshableGrant(grantType: GrantType | string): boolean {
  return grantType === "authorization_code" || grantType === "refresh_token";
}

function toOAuthError(e: unknown): OAuthError {
  if (e instanceof KintoneOAuthError && e.kind === "invalid_grant") {
    return new OAuthError("invalid_grant", {
      description: "kintone access was revoked or expired. Re-authorize.",
    });
  }
  // 運用者のクライアント ID / シークレットの誤り、または再試行で直らない拒否。grant は失効させない(ADR 0005)。
  if (e instanceof KintoneOAuthError && (e.kind === "invalid_client" || e.kind === "rejected")) {
    return new OAuthError("server_error", {
      description: "kintone sign-in is misconfigured. Contact the administrator.",
      statusCode: 503,
    });
  }
  return new OAuthError("temporarily_unavailable", {
    description: "kintone is temporarily unavailable. Retry later.",
    statusCode: 503,
  });
}
