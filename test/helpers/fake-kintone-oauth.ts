import { jsonResponse, type RecordedCall } from "./mock-fetch";

/**
 * 状態と時刻を持つ kintone OAuth の fake(E2E 用。実 kintone には通信しない)。
 *
 * kintone の更新の実挙動は未検証(V01)なので、2 つのモードで同じテストを回し、どちらでも壊れないことを固定する(ADR 0005):
 * - モード A: 有効期限内の更新は、同じアクセストークンを残り寿命(expires_in = 残り秒数)つきで返すだけで、期限は延びない。
 *   リフレッシュトークンも返さない。失効後の更新は新しいトークン(expires_in = lifetime)を発行する
 * - モード B: 更新のたびに(期限内でも)新しいアクセストークンを発行し、古いトークンは無効になる
 * `rotate` は失効後(モード B は常に)の更新でリフレッシュトークンも新しくする。
 *
 * apps は、現在のトークンで期限内かつ取り消されていないときだけ 200、それ以外は 401。
 * 時刻は Date.now()(vi.useFakeTimers の Date)で判定する。
 */
export interface FakeKintoneOAuthOptions {
  /** 既定は A。 */
  mode?: "A" | "B";
  /** 更新でリフレッシュトークンも新しくする(ローテーションの確認用)。 */
  rotate?: boolean;
  /** リフレッシュに常にこのステータスで失敗する(invalid_grant)。 */
  refreshStatus?: number;
  /** アクセストークンの寿命(秒)。既定 3600。 */
  lifetimeSeconds?: number;
}

export function createFakeKintoneOAuth(opts: FakeKintoneOAuthOptions = {}) {
  const mode = opts.mode ?? "A";
  const lifetime = opts.lifetimeSeconds ?? 3600;
  let accessSeq = 0;
  let refreshSeq = 1;
  let current: { accessToken: string; expiresAt: number } | undefined;
  let refreshToken = "kintone-rt-1";
  let revoked = false;
  let refreshCalls = 0;
  let refreshesReturningSameToken = 0;

  function issue() {
    accessSeq += 1;
    current = { accessToken: `kintone-at-${accessSeq}`, expiresAt: Date.now() + lifetime * 1000 };
    revoked = false;
    return current;
  }

  function isValid(token: string | undefined): boolean {
    return (
      current !== undefined &&
      !revoked &&
      token === current.accessToken &&
      Date.now() < current.expiresAt
    );
  }

  return {
    /** POST /oauth2/token(Basic 認証) */
    token(call: RecordedCall): Response {
      if (!call.headers.get("authorization")?.startsWith("Basic ")) {
        return jsonResponse({ error: "invalid_client" }, 401);
      }
      const form = new URLSearchParams(call.body);
      if (form.get("grant_type") === "authorization_code") {
        const t = issue();
        return jsonResponse({
          access_token: t.accessToken,
          refresh_token: refreshToken,
          token_type: "Bearer",
          expires_in: lifetime,
        });
      }
      refreshCalls += 1;
      if (opts.refreshStatus && opts.refreshStatus !== 200) {
        return jsonResponse({ error: "invalid_grant" }, opts.refreshStatus);
      }
      if (form.get("refresh_token") !== refreshToken) {
        return jsonResponse({ error: "invalid_grant" }, 400);
      }
      const stillValid = current && !revoked && Date.now() < current.expiresAt;
      if (mode === "A" && current && stillValid) {
        // 期限内: 同じトークンと残り秒数だけ
        refreshesReturningSameToken += 1;
        return jsonResponse({
          access_token: current.accessToken,
          token_type: "Bearer",
          expires_in: Math.floor((current.expiresAt - Date.now()) / 1000),
        });
      }
      const t = issue();
      if (opts.rotate) {
        refreshSeq += 1;
        refreshToken = `kintone-rt-${refreshSeq}`;
      }
      return jsonResponse({
        access_token: t.accessToken,
        ...(opts.rotate ? { refresh_token: refreshToken } : {}),
        token_type: "Bearer",
        expires_in: lifetime,
      });
    },
    /** GET /k/v1/apps.json */
    apps(call: RecordedCall): Response {
      const token = call.headers.get("authorization")?.replace(/^Bearer /, "");
      return isValid(token)
        ? jsonResponse({ apps: [{ appId: "1", code: "CRM", name: "顧客管理" }] })
        : jsonResponse({ code: "CB_AU01", message: "unauthorized" }, 401);
    },
    /** 期限前に kintone 側でトークンが取り消された状態にする。次のリフレッシュは新しいトークンを発行する。 */
    revoke() {
      revoked = true;
    },
    get refreshCalls() {
      return refreshCalls;
    },
    get refreshesReturningSameToken() {
      return refreshesReturningSameToken;
    },
    get currentAccessToken() {
      return current?.accessToken;
    },
  };
}
