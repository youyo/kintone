/**
 * feedback ツール(ADR 0013)の匿名化ルールのうち、kintone 固有の部分(共通仕様 §7.2)。
 * 共通の部分は guide.ts。ほかの製品のリポジトリへ写すときは、このファイルを製品ごとに差し替える。
 */
import { ERROR_CODES } from "../errors";

/** kintone 固有のプレースホルダ。共通の `<USER>` `<EMAIL>` `<ORG>` `<HOST>` `<URL>` `<ID>` に続けて返す。 */
export const KINTONE_PLACEHOLDERS = ["<SPACE>", "<APP>", "<FIELD>"] as const;

/**
 * kintone 固有の匿名化ルール。共通のルールに続けて返す。プレースホルダはバッククォートで囲む。
 * 残してよい固定のエラーコードは src/errors.ts の ERROR_CODES(ツールのエラーのエンベロープの code)。
 * kintone API 自身のエラーコード(`CB_` で始まるものなど)と message は残さない。
 */
export const KINTONE_ANONYMIZATION_RULES: readonly string[] = [
  "サブドメインと URL（`*.cybozu.com` `*.kintone.com` など）は `<SPACE>` にする",
  "アプリ名・アプリコードは `<APP>` にする",
  "フィールドコード・フィールド名は `<FIELD>` にする（複数あるときは `<FIELD_1>` `<FIELD_2>`）",
  "アプリ ID・レコード ID・リビジョン・スペース ID は `<ID>` にする",
  "kintone のスペース名・スレッド名・ビュー名も伏せる",
  `残してよい公開固定値は、kintone の公開ツール名と、固定のエラーコード（${ERROR_CODES.map((c) => `\`${c}\``).join(" ")}）。kintone API 自身のエラーコード（\`CB_\` で始まるものなど）や message は残さない`,
];
