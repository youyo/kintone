// Package cli は kintone CLI のエントリポイントと全コマンドを提供する。
// cobra を使用してコマンドツリーを構築し、JSON 出力規約に従った出力を行う。
//
// 使用方法:
//
//	func main() {
//	    if err := cli.Execute(); err != nil {
//	        os.Exit(1)
//	    }
//	}
package cli

import (
	"io"
	"os"

	"github.com/spf13/cobra"
	cliapi "github.com/youyo/kintone/internal/cli/api"
	climcp "github.com/youyo/kintone/internal/cli/mcp"
	cliops "github.com/youyo/kintone/internal/cli/ops"
	"github.com/youyo/kintone/internal/output"
)

// NewRootCmd はテスト可能な root コマンドを毎回新規生成する。
// グローバル変数を持たないため、テスト時の状態汚染を防止する。
//
// M02 で PersistentFlags（--profile, --config, --no-color）を登録する。
// --no-color は M02 では宣言のみで挙動は未実装（後続マイルストーンで利用）。
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kintone",
		Short: "kintone CLI ツール",
		Long: `kintone は kintone API を操作するための CLI ツールです。
全コマンドは JSON 形式（{"ok":true,"data":{...}}）で結果を出力します。`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().String("profile", "", "使用する profile 名（KINTONE_PROFILE 環境変数より優先）")
	cmd.PersistentFlags().String("config", "", "config.toml のパス（KINTONE_CONFIG_PATH 環境変数より優先）")
	cmd.PersistentFlags().Bool("no-color", false, "カラー出力を無効化（後続マイルストーンで利用予定）")
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(cliapi.NewCmd())
	cmd.AddCommand(cliops.NewCmd())
	cmd.AddCommand(climcp.NewCmd())
	return cmd
}

// Execute はバイナリ起動エントリポイント。
// 内部で ExecuteWith(os.Args[1:], os.Stdout, os.Stderr) を呼ぶ薄いラッパ。
// main は戻り値が non-nil なら os.Exit(1) を呼ぶ。
func Execute() error {
	return ExecuteWith(os.Args[1:], os.Stdout, os.Stderr)
}

// ExecuteWith はテストおよび Execute() 本体の共通実装。
// args / out / errOut を差し替え可能にし、エラー時は output.Failure を out（stdout）に書く。
// 失敗 JSON は stdout に統一する（Output Policy 参照）。
func ExecuteWith(args []string, out, errOut io.Writer) error {
	cmd := NewRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(out)
	cmd.SetErr(errOut) // cobra 自身のメッセージ用（SilenceErrors=true なので原則出ない）
	if err := cmd.Execute(); err != nil {
		oe := MapToOutputError(err)
		payload, _ := output.Failure(oe)
		_ = output.Write(out, payload) // 失敗 JSON は stdout（= out）に統一
		return err
	}
	return nil
}
