package store

import (
	"os"
	"strings"
)

// Backend 名定数。KINTONE_STORE_BACKEND の値および Container 内部判定に共通利用する。
const (
	BackendMemory   = "memory"
	BackendSQLite   = "sqlite"
	BackendRedis    = "redis"
	BackendDynamoDB = "dynamodb"
)

// Config は store パッケージの構成パラメータ。
//
// 環境変数とのマッピングは [LoadFromEnv] を参照。
// CLI フラグから上書きする経路は Phase 7+ で配線する。
type Config struct {
	Backend        string
	SQLiteDir      string
	RedisURL       string
	RedisTLS       bool
	RedisPassword  string
	DynamoDBTable  string
	DynamoDBRegion string
	CacheBypass    bool
	SigningKeyPEM  string
	// SigningKeyAutoGenerate は KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE=1 のオプトインで、
	// SigningKey が env / Storage に未保存のとき新規生成・保存を許可するフラグ。
	// 未設定（false）かつ env も未設定の場合、auth=oidc では fail-fast する。
	SigningKeyAutoGenerate bool
	// RedisInsecurePlaintext は KINTONE_STORE_REDIS_INSECURE_PLAINTEXT=1 のオプトインで、
	// 非 localhost への redis:// 平文接続を許可するフラグ。
	// 未設定（false）の場合、loopback 以外への redis:// 接続は ErrPlaintextForbidden で reject される。
	RedisInsecurePlaintext bool
}

// LoadFromEnv は os 環境変数から Config を構築する。
// 既定 backend は sqlite だが、Phase 1 では memory のみ実装されているため、
// 呼び出し側は明示的に Backend = BackendMemory を指定するか KINTONE_STORE_BACKEND=memory を設定する必要がある。
func LoadFromEnv() *Config {
	return &Config{
		Backend:                getenvDefault("KINTONE_STORE_BACKEND", BackendSQLite),
		SQLiteDir:              os.Getenv("KINTONE_STORE_SQLITE_DIR"),
		RedisURL:               os.Getenv("KINTONE_STORE_REDIS_URL"),
		RedisTLS:               boolEnv("KINTONE_STORE_REDIS_TLS"),
		RedisPassword:          os.Getenv("KINTONE_STORE_REDIS_PASSWORD"),
		DynamoDBTable:          os.Getenv("KINTONE_STORE_DYNAMODB_TABLE"),
		DynamoDBRegion:         os.Getenv("KINTONE_STORE_DYNAMODB_REGION"),
		CacheBypass:            boolEnv("KINTONE_STORE_CACHE_BYPASS"),
		SigningKeyPEM:          os.Getenv("KINTONE_MCP_SIGNING_KEY_PEM"),
		SigningKeyAutoGenerate: boolEnv("KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE"),
		RedisInsecurePlaintext: boolEnv("KINTONE_STORE_REDIS_INSECURE_PLAINTEXT"),
	}
}

func getenvDefault(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func boolEnv(k string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
	return v == "1" || v == "true" || v == "yes"
}
