// Package completion は kintone CLI のシェル補完スクリプト生成コマンドを提供する。
//
// 出力規約の例外:
//
//	`kintone completion {bash|zsh|fish|powershell}` はシェルが直接 source する
//	プレーンスクリプトを stdout に書き出す。JSON envelope（{"ok":true,...}）には
//	**包まない**。これは仕様 docs/specs/kintone_spec.md の Output Policy 例外として
//	`completion` / `version --short` と同列に扱う。
package completion

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// NewCmd は `kintone completion` サブコマンドを構築する。
//
// 引数 root は補完スクリプト生成元のルートコマンド。NewRootCmd() の戻り値を渡す。
//
// 使用例:
//
//	# bash
//	kintone completion bash > /etc/bash_completion.d/kintone
//	# zsh（fpath にある dir に出力）
//	kintone completion zsh > "${fpath[1]}/_kintone"
//	# fish
//	kintone completion fish > ~/.config/fish/completions/kintone.fish
//	# powershell
//	kintone completion powershell | Out-String | Invoke-Expression
func NewCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "シェル補完スクリプトを生成する",
		Long:      longDescription,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		// completion 出力は JSON envelope の例外として扱う（プレーン出力）。
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generate(root, cmd.OutOrStdout(), args[0])
		},
	}
	return cmd
}

func generate(root *cobra.Command, out io.Writer, shell string) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(out, true)
	case "zsh":
		return root.GenZshCompletion(out)
	case "fish":
		return root.GenFishCompletion(out, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(out)
	default:
		return fmt.Errorf("completion: unsupported shell %q", shell)
	}
}

const longDescription = `kintone CLI のシェル補完スクリプトを stdout に出力します。

出力は **プレーンなシェルスクリプト** であり、kintone CLI の通常コマンドの
JSON envelope 出力規約（{"ok":true,"data":{...}}）には包みません。
シェルが直接 source / Invoke-Expression するため、この出力規約の例外として扱います。

サポートシェル: bash / zsh / fish / powershell

インストール例:

  # bash (Linux)
  kintone completion bash > /etc/bash_completion.d/kintone

  # bash (macOS / Homebrew)
  kintone completion bash > $(brew --prefix)/etc/bash_completion.d/kintone

  # zsh
  kintone completion zsh > "${fpath[1]}/_kintone"

  # fish
  kintone completion fish > ~/.config/fish/completions/kintone.fish

  # powershell（プロファイルに追記）
  kintone completion powershell | Out-String | Invoke-Expression
`
