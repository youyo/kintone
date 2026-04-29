// Package config は kintone CLI/MCP の設定読み込み・解決を提供する。
//
// 優先順位: CLI フラグ > 環境変数 > config.toml
//
// 各レイヤは独立した構造体（FileConfig / EnvConfig / CLIConfig）で
// 受け取り、Resolver が一意に Resolved にマージする。
//
// 後続マイルストーン（auth, kintoneapi）は Load() の戻り値 Resolved のみを参照する。
package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// AuthMode は認証モード。"api-token" / "oauth" / 空文字（未設定）。
// M02 では値の格納のみで使用、消費は M3+。
type AuthMode string

const (
	// AuthModeAPIToken は API Token 認証を表す。
	AuthModeAPIToken AuthMode = "api-token"
	// AuthModeOAuth は OAuth 認証を表す。
	AuthModeOAuth AuthMode = "oauth"
)

// FileConfig は TOML ファイルから読み込んだ生の構造。
//
//	[default_profile]
//	name = "default"
//
//	[profiles.default]
//	domain = "example.cybozu.com"
//	auth   = "api-token"
type FileConfig struct {
	DefaultProfile DefaultProfileBlock     `toml:"default_profile"`
	Profiles       map[string]ProfileBlock `toml:"profiles"`
}

// DefaultProfileBlock は [default_profile] セクション。
type DefaultProfileBlock struct {
	Name string `toml:"name"`
}

// ProfileBlock は 1 プロファイル分の設定。
// M02 で扱うのは Domain / Auth のみ。
type ProfileBlock struct {
	Domain string `toml:"domain"`
	Auth   string `toml:"auth"` // "api-token" / "oauth"
}

// EnvConfig は環境変数から読み取った生の値（未解決）。
// 空文字は「未設定」を意味する。
type EnvConfig struct {
	Profile    string // KINTONE_PROFILE
	ConfigPath string // KINTONE_CONFIG_PATH
	CachePath  string // KINTONE_CACHE_PATH
	Domain     string // KINTONE_DOMAIN
	Auth       string // KINTONE_AUTH
	APIToken   string // KINTONE_API_TOKEN
}

// CLIConfig は CLI フラグから渡された値（未解決）。
// 空文字は「未指定」を意味する。
type CLIConfig struct {
	Profile    string // --profile
	ConfigPath string // --config
}

// Resolved は CLI > ENV > toml の優先順位で確定した最終設定。
// 後続マイルストーン（auth, kintoneapi）はこの構造のみを参照する。
//
// JSON タグは snake_case で統一し、`config show` がこの構造を直接シリアライズできる。
// ただし api_token は機微情報のため、show 時はマスク済みコピーを別途構築する。
type Resolved struct {
	ProfileName string   `json:"profile"`
	Domain      string   `json:"domain"`
	Auth        AuthMode `json:"auth"`
	APIToken    string   `json:"api_token"`
	ConfigPath  string   `json:"config_path"`
	CachePath   string   `json:"cache_path"`
	Source      Sources  `json:"source"`
}

// Sources は Resolved の各フィールドがどのレイヤから来たかを記録する。
// 値は "cli" / "env" / "file" / "default" のいずれか。
type Sources struct {
	Profile string `json:"profile"`
	Domain  string `json:"domain"`
	Auth    string `json:"auth"`
}

// LoadOptions は Load の入力。
//
// 全関数フィールドは未設定（nil）の場合、対応する os パッケージのデフォルトが使用される。
// テスト時はこれらを差し替えてグローバル状態に依存しない unit テストが書ける。
type LoadOptions struct {
	CLI         CLIConfig
	Getenv      func(string) string
	ReadFile    func(path string) ([]byte, error)
	UserHomeDir func() (string, error)
}

// Load は CLI / ENV / file を解決した *Resolved を返す。
//
// 解決順序:
//  1. UserHomeDir で HOME を取得（失敗すれば即エラー）
//  2. config.toml のパスを決定: CLI.ConfigPath > ENV.ConfigPath > $HOME/.config/kintone/config.toml
//  3. ファイル読み取り。明示指定（CLI/ENV）かつ ErrNotExist のときのみ NotFoundError、
//     デフォルトパスで不在なら空 FileConfig として続行
//  4. profile 名を決定: CLI > ENV > FileConfig.DefaultProfile.Name > "default"
//  5. CLI > ENV > FileConfig の優先順位で Resolved にマージ
func Load(opts LoadOptions) (*Resolved, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	readFile := opts.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	homeFn := opts.UserHomeDir
	if homeFn == nil {
		homeFn = os.UserHomeDir
	}

	home, err := homeFn()
	if err != nil {
		return nil, fmt.Errorf("config: resolve home dir: %w", err)
	}

	env := LoadEnv(getenv)

	// config.toml のパス決定
	configPath, explicit := resolveConfigPath(opts.CLI, env, home)

	// ファイル読み取り
	file, fileExists, err := loadFileWithExistence(configPath, readFile)
	if err != nil {
		// loadFileWithExistence はパースエラー / IO エラー（ErrNotExist 以外）を返す
		return nil, err
	}

	// 明示指定（CLI/ENV）かつ ErrNotExist の場合は NotFoundError
	// 明示なし（デフォルトパス）かつ不在は許容（空 FileConfig として続行）
	if explicit && !fileExists {
		return nil, &NotFoundError{Path: configPath}
	}

	profileName := SelectProfile(opts.CLI, env, &file)

	r, err := Resolve(profileName, opts.CLI, env, file)
	if err != nil {
		return nil, err
	}
	r.ConfigPath = configPath

	return r, nil
}

// resolveConfigPath は config.toml の最終パスを決定する。
// 戻り値の explicit は CLI/ENV による明示指定があったかを表す。
func resolveConfigPath(cli CLIConfig, env EnvConfig, home string) (path string, explicit bool) {
	if cli.ConfigPath != "" {
		return cli.ConfigPath, true
	}
	if env.ConfigPath != "" {
		return env.ConfigPath, true
	}
	return filepath.Join(home, ".config", "kintone", "config.toml"), false
}

// loadFileWithExistence は LoadFile を呼び、ファイル不在の事実を呼び出し側に伝える。
// 戻り値:
//
//	fc       : パース済み FileConfig（不在/空時はゼロ値）
//	exists   : ファイルが存在し読み込みに成功したか
//	err      : パースエラーまたは ErrNotExist 以外の IO エラー
func loadFileWithExistence(path string, readFile func(string) ([]byte, error)) (FileConfig, bool, error) {
	bytes, ioErr := readFile(path)
	if ioErr != nil {
		if errIs(ioErr, fs.ErrNotExist) {
			return FileConfig{}, false, nil
		}
		return FileConfig{}, false, ioErr
	}
	if len(bytes) == 0 {
		// 空ファイルは「存在するが内容なし」として扱う
		return FileConfig{}, true, nil
	}
	fc, err := decodeTOML(path, bytes)
	if err != nil {
		return FileConfig{}, true, err
	}
	return fc, true, nil
}
