import type { ToolDefinition } from "../registry";
import { appDescribe } from "./app-describe";
import { appsSearch } from "./apps-search";
import { recordCreate } from "./record-create";
import { recordDelete } from "./record-delete";
import { recordUpdate } from "./record-update";
import { recordsQuery } from "./records-query";

/**
 * 公開するツールの一覧。読み取りの 3 つ(`kintone_apps_search` / `kintone_app_describe` / `kintone_records_query`)と
 * 書き込みの 3 つ(M04: `kintone_record_create` / `kintone_record_update` / `kintone_record_delete`)。
 * ツールを足したら `mise run test:update-snapshots` で tools/list のスナップショットを更新し、差分を確認する。
 */
export const ALL_TOOLS: readonly ToolDefinition[] = [
  appsSearch,
  appDescribe,
  recordsQuery,
  recordCreate,
  recordUpdate,
  recordDelete,
];
