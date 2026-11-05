/**
 * kintone の apps.json / form/fields.json のふるまいを真似るテスト用のデータ層。
 * `listApps` は ids / codes(完全一致)/ name(部分一致・大文字小文字を区別しない)/ spaceIds / limit / offset を解釈する。
 */
import type { Query } from "../../src/kintone/client";

export interface AppRow {
  appId: string;
  code: string;
  name: string;
  spaceId?: string | null;
}

/** kintone の apps.json の 1 要素(生データの形)。 */
export function appEntry(row: AppRow) {
  return {
    appId: row.appId,
    code: row.code,
    name: row.name,
    description: "",
    spaceId: row.spaceId ?? null,
    threadId: null,
    createdAt: "2024-01-01T00:00:00Z",
    creator: { code: "user1", name: "User 1" },
    modifiedAt: "2024-01-02T00:00:00Z",
    modifier: { code: "user1", name: "User 1" },
  };
}

const asArray = (v: unknown): unknown[] => (Array.isArray(v) ? v : v === undefined ? [] : [v]);

/** `codes[0]=a&codes[1]=b` 形式の URLSearchParams を、クライアントの Query(配列つき)に戻す。 */
export function queryFromSearchParams(params: URLSearchParams): Query {
  const out: Record<string, string | string[]> = {};
  for (const [key, value] of params) {
    const m = /^(.+)\[(\d+)\]$/.exec(key);
    if (m?.[1]) {
      const list = (out[m[1]] as string[] | undefined) ?? [];
      list[Number(m[2])] = value;
      out[m[1]] = list;
    } else {
      out[key] = value;
    }
  }
  return out;
}

export function listApps(all: readonly AppRow[], query: Query = {}): { apps: unknown[] } {
  let rows = [...all];
  const ids = asArray(query.ids).map(String);
  if (ids.length > 0) rows = rows.filter((r) => ids.includes(r.appId));
  const codes = asArray(query.codes).map(String);
  if (codes.length > 0) rows = rows.filter((r) => codes.includes(r.code));
  const name = typeof query.name === "string" ? query.name : undefined;
  if (name) rows = rows.filter((r) => r.name.toLowerCase().includes(name.toLowerCase()));
  const spaceIds = asArray(query.spaceIds).map(String);
  if (spaceIds.length > 0) rows = rows.filter((r) => spaceIds.includes(String(r.spaceId)));
  const offset = Number(query.offset ?? 0);
  const limit = Number(query.limit ?? 100);
  return { apps: rows.slice(offset, offset + limit).map(appEntry) };
}

/** 名前が `<prefix><n>` の満杯のアプリ群(ページング用)。 */
export function manyApps(count: number, prefix = "アプリ", startId = 1000): AppRow[] {
  return Array.from({ length: count }, (_, i) => ({
    appId: String(startId + i),
    code: `${prefix.replace(/[^A-Za-z0-9]/g, "")}${startId + i}`,
    name: `${prefix}${i}`,
  }));
}

export interface FieldProp {
  type: string;
  code: string;
  label: string;
  [key: string]: unknown;
}

/** form/fields.json の応答。`properties` のキーはフィールドコード。 */
export function fieldsResponse(props: FieldProp[], revision = "5") {
  return {
    properties: Object.fromEntries(props.map((p) => [p.code, p])),
    revision,
  };
}
