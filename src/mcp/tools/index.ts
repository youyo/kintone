import type { ToolDefinition } from "../registry";
import { appDescribe } from "./app-describe";
import { appsSearch } from "./apps-search";
import { recordsQuery } from "./records-query";

/**
 * 公開するツールの一覧。M03 は読み取りの 3 つ(`kintone_apps_search` / `kintone_app_describe` / `kintone_records_query`)。
 * M04 で書き込みを足す。
 * ツールを足したら `mise run test:update-snapshots` で tools/list のスナップショットを更新し、差分を確認する。
 */
export const ALL_TOOLS: readonly ToolDefinition[] = [appsSearch, appDescribe, recordsQuery];
