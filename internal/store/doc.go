// Package store は kintone CLI / MCP の永続化層（Tokens / Cache / SigningKey / IDProxy）
// を 4 backend（memory / sqlite / redis / dynamodb）で抽象化する統合 Storage 層。
//
// # 層構造
//
// アプリ層は [Container] interface のみに依存する。
//   - [TokenStore]     — OAuth / API トークンの保存
//   - [CacheStore]     — apps / fields / list_apps の TTL 付き KV キャッシュ
//   - [SigningKeyStore] — idproxy ES256 署名鍵の永続保管
//   - [IDProxyStore]   — idproxy（OIDC AS）が必要とする session / authCode / refresh 等
//
// # backend 選定基準
//
//   - memory  : 単発 CLI / 単体テスト / dev 限定（auth=oidc では使用禁止）
//   - sqlite  : ローカル CLI default（XDG パス）
//   - redis   : 単一リージョンの multi-instance MCP
//   - dynamodb: マルチリージョン / フルマネージド運用
//
// Phase 1 では memory backend のみを実装する。
//
// # Backend の登録
//
// 各 backend サブパッケージ（memory/sqlite/redis/dynamodb）は
// init() で RegisterOpener を呼び、factory に自身を登録する。
// caller は使用する backend をプログラム main の早期で blank import する必要がある。
//
//	import (
//	    _ "github.com/youyo/kintone/internal/store/memory"
//	    _ "github.com/youyo/kintone/internal/store/sqlite"
//	)
//
// blank import を忘れると OpenFromConfig が ErrUnsupportedBackend を返す。
package store
