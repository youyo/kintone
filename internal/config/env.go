package config

// LoadEnv は getenv（または os.Getenv）から EnvConfig を構築する。
// 空文字は未設定として扱い、各フィールドにそのまま格納される。
//
// バリデーション（KINTONE_AUTH の値の妥当性等）は行わない。
// 上位（Resolve）または後続マイルストーン（M3 認証）で実施する。
func LoadEnv(getenv func(string) string) EnvConfig {
	return EnvConfig{
		Profile:    getenv("KINTONE_PROFILE"),
		ConfigPath: getenv("KINTONE_CONFIG_PATH"),
		CachePath:  getenv("KINTONE_CACHE_PATH"),
		Domain:     getenv("KINTONE_DOMAIN"),
		Auth:       getenv("KINTONE_AUTH"),
		APIToken:   getenv("KINTONE_API_TOKEN"),
		// OAuth 関連（M09）
		OAuthClientID:     getenv("KINTONE_OAUTH_CLIENT_ID"),
		OAuthClientSecret: getenv("KINTONE_OAUTH_CLIENT_SECRET"),
		OAuthRedirectURL:  getenv("KINTONE_OAUTH_REDIRECT_URL"),
		OAuthScopes:       getenv("KINTONE_OAUTH_SCOPES"),
	}
}
