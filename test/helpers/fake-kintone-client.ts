import type { KintoneClient } from "../../src/kintone/client";

/**
 * ツールの unit テスト用の KintoneClient。渡したメソッドだけが動き、それ以外(書き込みなど)は
 * 呼ばれたら例外にする(読み取りのテストが意図せず書き込みを呼ばないことの保証)。
 */
export function fakeKintoneClient(impl: Partial<KintoneClient> = {}): KintoneClient {
  const unexpected = (name: string) => async () => {
    throw new Error(`fakeKintoneClient: 想定外の呼び出し ${name}`);
  };
  return {
    get: impl.get ?? unexpected("get"),
    post: impl.post ?? unexpected("post"),
    put: impl.put ?? unexpected("put"),
    delete: impl.delete ?? unexpected("delete"),
    redact: impl.redact ?? ((text) => text),
  };
}
