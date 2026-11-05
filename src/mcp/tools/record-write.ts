/**
 * 書き込みツール(record_create / record_update / record_delete)の共通部(仕様書 §5.4〜5.6)。
 */
import { z } from "zod";
import { KintoneMcpError } from "../../errors";

/** `records`(create)・`ids`(delete)の上限。公式 add-records / delete-records。超えたら kintone を呼ぶ前に argument_error。 */
export const MAX_WRITE_ITEMS = 100;

/** 1 件のレコード: `{"<フィールドコード>":{"value":...}}`。中身の形は検査しない(kintone が 400 CB_VA01 で返す)。 */
export const recordShape = z.record(z.string(), z.unknown());

/** `revision` / `revisions` の要素: 整数(-1 は検証なし。公式)。 */
export const revisionShape = z.number().int().min(-1);

/**
 * kintone が文字列で返す `ids` / `revisions` / `revision` を数値にする。
 * 書き込みはすでに済んでいるので、読めないときのエラーは **retryable でない** api_error
 * (再送を促すと二重に作成・更新してしまう)。
 */
export function toKintoneInteger(value: unknown, what: string): number {
  const n =
    typeof value === "number"
      ? value
      : typeof value === "string" && /^-?\d+$/.test(value)
        ? Number(value)
        : Number.NaN;
  if (!Number.isSafeInteger(n)) {
    throw new KintoneMcpError(
      "api_error",
      `kintone returned an unreadable ${what}. The write may already have been applied: do not resend it blindly; check the records first.`,
      false,
    );
  }
  return n;
}

/** 応答の配列(`ids` / `revisions`)を数値の配列にする。配列でなければ読めないエラー。 */
export function toKintoneIntegers(value: unknown, what: string): number[] {
  if (!Array.isArray(value)) return [toKintoneInteger(undefined, what)];
  return value.map((v) => toKintoneInteger(v, what));
}
