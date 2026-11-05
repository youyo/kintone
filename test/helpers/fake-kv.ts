/**
 * メモリ上の KV(unit 用)。呼び出しの記録と、get / put / delete の失敗の注入ができる。
 * TTL は記録するだけで、時間では消さない(期限の挙動は KV 本体の責務)。
 */
export interface KvPut {
  key: string;
  value: string;
  options: KVNamespacePutOptions | undefined;
}

export interface FakeKv {
  kv: KVNamespace;
  store: Map<string, string>;
  puts: KvPut[];
  gets: string[];
  deletes: string[];
  /** true にすると、その操作が例外になる(KV の障害のシミュレーション)。 */
  fail: { get: boolean; put: boolean; delete: boolean };
}

export function fakeKv(initial: Record<string, string> = {}): FakeKv {
  const f: FakeKv = {
    kv: undefined as unknown as KVNamespace,
    store: new Map(Object.entries(initial)),
    puts: [],
    gets: [],
    deletes: [],
    fail: { get: false, put: false, delete: false },
  };
  f.kv = {
    get: async (key: string) => {
      f.gets.push(key);
      if (f.fail.get) throw new Error("kv get failed (secret-kv-detail)");
      return f.store.get(key) ?? null;
    },
    put: async (key: string, value: string, options?: KVNamespacePutOptions) => {
      f.puts.push({ key, value, options });
      if (f.fail.put) throw new Error("kv put failed (secret-kv-detail)");
      f.store.set(key, value);
    },
    delete: async (key: string) => {
      f.deletes.push(key);
      if (f.fail.delete) throw new Error("kv delete failed (secret-kv-detail)");
      f.store.delete(key);
    },
  } as unknown as KVNamespace;
  return f;
}
