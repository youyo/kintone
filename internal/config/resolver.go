package config

import "strings"

// Resolve は profile 確定後に CLI > ENV > FileConfig の優先順位でマージし、
// *Resolved を返す。
//
// profile が file 内に存在しない場合の扱い:
//   - profile == "default" かつ file.Profiles に "default" が無い → エラーにせず空 Resolved を返す
//     （初回起動・config 未生成のユースケースを許容するため）
//   - profile != "default" かつ file.Profiles に該当無し → *ProfileNotFoundError
//
// 各 Source フィールドは値の出所を記録する: "cli" / "env" / "file" / "default"
// （M02 では Domain/Auth に CLI ソースは無いが、API として将来のために残す）
func Resolve(profileName string, cli CLIConfig, env EnvConfig, file FileConfig) (*Resolved, error) {
	// profile lookup
	pb, ok := file.Profiles[profileName]
	if !ok {
		if profileName != "default" {
			return nil, &ProfileNotFoundError{Name: profileName}
		}
		// default かつ未定義 → 空 ProfileBlock として扱う
		pb = ProfileBlock{}
	}

	r := &Resolved{
		ProfileName: profileName,
		APIToken:    env.APIToken,
		CachePath:   env.CachePath,
	}

	// Domain: ENV > file > default
	switch {
	case env.Domain != "":
		r.Domain = env.Domain
		r.Source.Domain = "env"
	case pb.Domain != "":
		r.Domain = pb.Domain
		r.Source.Domain = "file"
	default:
		r.Source.Domain = "default"
	}

	// Auth: ENV > file > default
	switch {
	case env.Auth != "":
		r.Auth = AuthMode(env.Auth)
		r.Source.Auth = "env"
	case pb.Auth != "":
		r.Auth = AuthMode(pb.Auth)
		r.Source.Auth = "file"
	default:
		r.Source.Auth = "default"
	}

	// Profile source: CLI > ENV > file > default
	switch {
	case cli.Profile != "":
		r.Source.Profile = "cli"
	case env.Profile != "":
		r.Source.Profile = "env"
	case file.DefaultProfile.Name != "":
		r.Source.Profile = "file"
	default:
		r.Source.Profile = "default"
	}

	// OAuth 設定（M09）: ENV > file > default
	// ClientID: ENV > file
	if env.OAuthClientID != "" {
		r.OAuthClientID = env.OAuthClientID
	} else if pb.OAuth.ClientID != "" {
		r.OAuthClientID = pb.OAuth.ClientID
	}

	// ClientSecret: ENV > file（環境変数推奨。file への記載は非推奨だが許容）
	if env.OAuthClientSecret != "" {
		r.OAuthClientSecret = env.OAuthClientSecret
	} else if pb.OAuth.ClientSecret != "" {
		r.OAuthClientSecret = pb.OAuth.ClientSecret
	}

	// RedirectURL: ENV > file
	if env.OAuthRedirectURL != "" {
		r.OAuthRedirectURL = env.OAuthRedirectURL
	} else if pb.OAuth.RedirectURL != "" {
		r.OAuthRedirectURL = pb.OAuth.RedirectURL
	}

	// Scopes: ENV（スペース区切り文字列を []string に変換） > file > デフォルト
	switch {
	case env.OAuthScopes != "":
		r.OAuthScopes = splitScopes(env.OAuthScopes)
	case len(pb.OAuth.Scopes) > 0:
		r.OAuthScopes = pb.OAuth.Scopes
	}

	return r, nil
}

// splitScopes はスペース区切りのスコープ文字列を []string に変換する。
// 空白のみのエントリは除去する。
func splitScopes(s string) []string {
	parts := strings.Fields(s)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ProfileNotFoundError は明示指定（--profile / KINTONE_PROFILE）された profile が
// file 内に見つからないことを表す。
// CLI 層で CONFIG_PROFILE_NOT_FOUND コードに変換される。
type ProfileNotFoundError struct {
	Name string
	Path string // file path（参考。Resolve では未設定、Load 経由で補完される）
}

// Error は人間可読なエラーメッセージを返す。
func (e *ProfileNotFoundError) Error() string {
	if e.Path != "" {
		return "config: profile not found: " + e.Name + " (in " + e.Path + ")"
	}
	return "config: profile not found: " + e.Name
}
