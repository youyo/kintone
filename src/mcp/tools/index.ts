import type { ToolDefinition } from "../registry";
import { appsSearch } from "./apps-search";

/**
 * 公開するツールの一覧。M02 は `kintone_apps_search` だけ(M03 で app_describe / records_query、M04 で書き込みを足す)。
 * ツールを足したら `mise run test:update-snapshots` で tools/list のスナップショットを更新し、差分を確認する。
 */
export const ALL_TOOLS: readonly ToolDefinition[] = [appsSearch];
