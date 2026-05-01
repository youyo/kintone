package store

import "testing"

func TestLoadFromEnv_Defaults(t *testing.T) {
	t.Setenv("KINTONE_STORE_BACKEND", "")
	t.Setenv("KINTONE_STORE_CACHE_BYPASS", "")
	t.Setenv("KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE", "")
	t.Setenv("KINTONE_STORE_REDIS_INSECURE_PLAINTEXT", "")
	cfg := LoadFromEnv()
	if cfg.Backend != BackendSQLite {
		t.Fatalf("default backend = %q, want %q", cfg.Backend, BackendSQLite)
	}
	if cfg.CacheBypass {
		t.Fatalf("default cache_bypass should be false")
	}
	if cfg.SigningKeyAutoGenerate {
		t.Fatalf("default signing_key_auto_generate should be false")
	}
	if cfg.RedisInsecurePlaintext {
		t.Fatalf("default redis_insecure_plaintext should be false")
	}
}

func TestLoadFromEnv_Overrides(t *testing.T) {
	t.Setenv("KINTONE_STORE_BACKEND", "memory")
	t.Setenv("KINTONE_STORE_SQLITE_DIR", "/tmp/kintone")
	t.Setenv("KINTONE_STORE_REDIS_URL", "redis://r:6379/0")
	t.Setenv("KINTONE_STORE_REDIS_TLS", "true")
	t.Setenv("KINTONE_STORE_REDIS_PASSWORD", "p")
	t.Setenv("KINTONE_STORE_DYNAMODB_TABLE", "kintone-tbl")
	t.Setenv("KINTONE_STORE_DYNAMODB_REGION", "ap-northeast-1")
	t.Setenv("KINTONE_STORE_CACHE_BYPASS", "1")
	t.Setenv("KINTONE_MCP_SIGNING_KEY_PEM", "PEM-DATA")
	t.Setenv("KINTONE_MCP_SIGNING_KEY_AUTO_GENERATE", "true")
	t.Setenv("KINTONE_STORE_REDIS_INSECURE_PLAINTEXT", "1")

	cfg := LoadFromEnv()
	if cfg.Backend != BackendMemory {
		t.Errorf("Backend got=%q want=%q", cfg.Backend, BackendMemory)
	}
	if cfg.SQLiteDir != "/tmp/kintone" {
		t.Errorf("SQLiteDir got=%q", cfg.SQLiteDir)
	}
	if cfg.RedisURL != "redis://r:6379/0" {
		t.Errorf("RedisURL got=%q", cfg.RedisURL)
	}
	if !cfg.RedisTLS {
		t.Errorf("RedisTLS should be true")
	}
	if cfg.RedisPassword != "p" {
		t.Errorf("RedisPassword got=%q", cfg.RedisPassword)
	}
	if cfg.DynamoDBTable != "kintone-tbl" {
		t.Errorf("DynamoDBTable got=%q", cfg.DynamoDBTable)
	}
	if cfg.DynamoDBRegion != "ap-northeast-1" {
		t.Errorf("DynamoDBRegion got=%q", cfg.DynamoDBRegion)
	}
	if !cfg.CacheBypass {
		t.Errorf("CacheBypass should be true")
	}
	if cfg.SigningKeyPEM != "PEM-DATA" {
		t.Errorf("SigningKeyPEM got=%q", cfg.SigningKeyPEM)
	}
	if !cfg.SigningKeyAutoGenerate {
		t.Errorf("SigningKeyAutoGenerate should be true")
	}
	if !cfg.RedisInsecurePlaintext {
		t.Errorf("RedisInsecurePlaintext should be true")
	}
}

func TestBoolEnv_Variants(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{" yes ", true},
		{"0", false},
		{"false", false},
		{"", false},
		{"no", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.val, func(t *testing.T) {
			t.Setenv("KINTONE_TEST_BOOL", tc.val)
			if got := boolEnv("KINTONE_TEST_BOOL"); got != tc.want {
				t.Fatalf("boolEnv(%q) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}
