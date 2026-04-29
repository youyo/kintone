package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/output"
)

// configSkeleton は `kintone config init` で書き出される TOML テンプレート。
// 機微情報（API Token / OAuth client secret）は記入しない方針。
const configSkeleton = `# kintone CLI / MCP サーバー設定ファイル
# 詳細: https://github.com/youyo/kintone#config
#
# 注意: API Token / OAuth client secret などの機微情報は
# このファイルに書かないこと。環境変数経由（KINTONE_API_TOKEN 等）で渡す。

[default_profile]
name = "default"

[profiles.default]
domain = ""           # 例: "example.cybozu.com"
auth   = "api-token"  # "api-token" or "oauth"
# API Token を使うときは KINTONE_API_TOKEN 環境変数で渡してください。

# 追加プロファイルの例（コメントアウト）:
# [profiles.dev]
# domain = "dev.cybozu.com"
# auth   = "oauth"
`

// newConfigCmd は config サブコマンド（show / init）を構築する。
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "設定を表示・初期化する",
		Long:  "config show で現在の解決済み設定を JSON 出力、config init でスケルトンを書き出す。",
	}
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigInitCmd())
	return cmd
}

// newConfigShowCmd は `kintone config show` を構築する。
//
// 解決済み設定を JSON 出力する。api_token は機微情報のためマスク（"***"）。
// エラーは return のみ行い、JSON 失敗出力は executeWith に委譲する。
func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "現在の解決済み設定を JSON 出力する",
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCfg := readCLIConfig(cmd)
			r, err := config.Load(config.LoadOptions{CLI: cliCfg})
			if err != nil {
				return err
			}
			payload, err := output.Success(maskedView(r))
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
}

// initResult は `kintone config init` の data 部分。
// struct を使うことで JSON フィールド順序を保証する。
type initResult struct {
	Path        string `json:"path"`
	Created     bool   `json:"created"`
	Overwritten bool   `json:"overwritten,omitempty"`
}

// newConfigInitCmd は `kintone config init` を構築する。
//
// 既定パス（または --config / KINTONE_CONFIG_PATH で指定したパス）に
// configSkeleton を 0o600 で書き出す。既存ファイルがある場合は --force が
// 無い限り CONFIG_ALREADY_EXISTS エラー。
func newConfigInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "config.toml のスケルトンを書き出す",
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCfg := readCLIConfig(cmd)
			path, err := resolveConfigInitPath(cliCfg)
			if err != nil {
				return err
			}

			overwrote := false
			if _, statErr := os.Stat(path); statErr == nil {
				if !force {
					return &config.AlreadyExistsError{Path: path}
				}
				overwrote = true
			}

			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return fmt.Errorf("config: mkdir parent: %w", err)
			}
			if err := writeFileAtomic(path, []byte(configSkeleton), 0o600); err != nil {
				return fmt.Errorf("config: write skeleton: %w", err)
			}

			payload, err := output.Success(initResult{
				Path:        path,
				Created:     true,
				Overwritten: overwrote,
			})
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "既存ファイルを上書きする")
	return cmd
}

// readCLIConfig は cobra 親コマンドの PersistentFlags から CLIConfig を構築する。
func readCLIConfig(cmd *cobra.Command) config.CLIConfig {
	profile, _ := cmd.Flags().GetString("profile")
	configPath, _ := cmd.Flags().GetString("config")
	return config.CLIConfig{Profile: profile, ConfigPath: configPath}
}

// resolveConfigInitPath は config init で書き出すファイルパスを決定する。
// CLI > ENV > $HOME/.config/kintone/config.toml の順。
func resolveConfigInitPath(cli config.CLIConfig) (string, error) {
	if cli.ConfigPath != "" {
		return cli.ConfigPath, nil
	}
	if envPath := os.Getenv("KINTONE_CONFIG_PATH"); envPath != "" {
		return envPath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "kintone", "config.toml"), nil
}

// writeFileAtomic は tmp ファイルに書いてから rename することで atomic な書き込みを保証する。
// 失敗時は tmp ファイルを cleanup する（R-13 対応）。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".kintone-config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// shownConfig は config show の出力 data 部分。
// Resolved の api_token をマスクし、それ以外はそのまま転写する。
type shownConfig struct {
	Profile    string         `json:"profile"`
	Domain     string         `json:"domain"`
	Auth       string         `json:"auth"`
	APIToken   string         `json:"api_token"`
	ConfigPath string         `json:"config_path"`
	CachePath  string         `json:"cache_path"`
	Source     config.Sources `json:"source"`
}

// maskedView は Resolved を機微情報マスク済みの shownConfig に変換する。
// api_token が非空なら "***" に置換、空なら空文字のまま返す。
func maskedView(r *config.Resolved) shownConfig {
	masked := r.APIToken
	if masked != "" {
		masked = "***"
	}
	return shownConfig{
		Profile:    r.ProfileName,
		Domain:     r.Domain,
		Auth:       string(r.Auth),
		APIToken:   masked,
		ConfigPath: r.ConfigPath,
		CachePath:  r.CachePath,
		Source:     r.Source,
	}
}
