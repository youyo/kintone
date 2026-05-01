package store

import "errors"

// store パッケージ全体で利用する sentinel error。
// backend 横断で意味を共有するため、具象実装は errors.Is で判別可能なように
// これらをそのまま、または fmt.Errorf("...: %w", Err...) で wrap して返す。
var (
	// ErrUnsupportedBackend は未対応 backend 名や Phase 未実装 backend に対して返る。
	ErrUnsupportedBackend = errors.New("store: unsupported backend")
	// ErrNotFound は TokenStore.Get / SigningKeyStore でキーが存在しない場合に返る。
	ErrNotFound = errors.New("store: not found")
	// ErrCacheMiss は CacheStore.Get でキー不在 / 期限切れの場合に返る。
	ErrCacheMiss = errors.New("store: cache miss")
	// ErrCacheBypassInvalid は KINTONE_STORE_CACHE_BYPASS の値解釈に失敗した場合に返る。
	ErrCacheBypassInvalid = errors.New("store: cache bypass value invalid")
	// ErrMemoryOIDCForbidden は auth=oidc 環境で memory backend が指定された場合に返る。
	ErrMemoryOIDCForbidden = errors.New("store: memory backend forbidden with auth=oidc")
	// ErrSigningKeyRequired は永続署名鍵が必須なのに未提供の場合に返る。
	ErrSigningKeyRequired = errors.New("store: signing key required")
	// ErrTableNotFound は DynamoDB テーブルが見つからないなど backend 固有の不在を表す。
	ErrTableNotFound = errors.New("store: table not found")
	// ErrGSIMissing は DynamoDB の GSI 未作成を表す。
	ErrGSIMissing = errors.New("store: gsi missing")
	// ErrTTLDisabled は DynamoDB TTL が無効化されているなどの構成不備を表す。
	ErrTTLDisabled = errors.New("store: ttl disabled")
	// ErrConnectionFailed は backend への接続失敗を表す。
	ErrConnectionFailed = errors.New("store: connection failed")
	// ErrPlaintextForbidden は暗号化必須 backend で平文書き込みを試みた場合に返る。
	ErrPlaintextForbidden = errors.New("store: plaintext forbidden")
	// ErrPrincipalNotFound は principal_id 解決に失敗した場合に返る。
	ErrPrincipalNotFound = errors.New("store: principal not found")
)
