/**
 * 最悪ケースの呼び出し回数(ADR 0008、仕様書 §8)。ツールの `maxApiCalls` はここの式から決める。
 *
 *   A = アプリ解決の最悪 = コード 1 + 名前 MAX_APP_NAME_PAGES(10)= 11
 *   F = フィールド解決 = 1
 *   読み取りは 1 呼び出しあたり最大 2 回リトライ(429・503)するので、論理回数 × 3 が試行数の最悪
 *   (書き込みはリトライしないので × 1。M04 で式から決める)
 *
 * 名前検索のページ上限を増やすと A が変わり、各ツールの値も変わる(増やすなら式から再計算し、call-budget.test.ts を直す)。
 */
import { DEFAULT_MAX_RETRIES } from "../kintone/client";

/** 名前検索の最大ページ数(100 件 / ページ)。10 ページ全部が満杯なら `resolver_app_list_too_large`。 */
export const MAX_APP_NAME_PAGES = 10;
/** アプリ名検索 1 ページの件数(kintone の上限)。 */
export const APPS_PAGE_SIZE = 100;
/** アプリ解決の最悪の論理呼び出し数 A(コード 1 + 名前 10)。 */
export const APP_RESOLVE_CALLS = 1 + MAX_APP_NAME_PAGES;
/** フィールド解決の最悪の論理呼び出し数 F。 */
export const FIELD_RESOLVE_CALLS = 1;

/** 読み取りツールの `maxApiCalls`: 論理呼び出し数 × (1 + リトライ回数)。 */
export function readBudget(logicalCalls: number): number {
  return logicalCalls * (1 + DEFAULT_MAX_RETRIES);
}
