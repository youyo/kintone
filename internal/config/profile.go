package config

// SelectProfile は CLI / ENV / FileConfig.DefaultProfile から
// 最終的な profile 名を決定する。
//
// 優先順位: cli.Profile > env.Profile > file.DefaultProfile.Name > "default"
//
// file が nil の場合は default にフォールバックする。
func SelectProfile(cli CLIConfig, env EnvConfig, file *FileConfig) string {
	if cli.Profile != "" {
		return cli.Profile
	}
	if env.Profile != "" {
		return env.Profile
	}
	if file != nil && file.DefaultProfile.Name != "" {
		return file.DefaultProfile.Name
	}
	return "default"
}
