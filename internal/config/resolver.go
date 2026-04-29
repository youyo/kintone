package config

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

	return r, nil
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
