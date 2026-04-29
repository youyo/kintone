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
	"github.com/youyo/kintone/internal/output"
)

// NewRootCmd はテスト可能な root コマンドを毎回新規生成する。
// グローバル変数を持たないため、テスト時の状態汚染を防止する。
// PersistentFlags（--profile, --config 等）は M2 で追加予定。
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kintone",
		Short: "kintone CLI ツール",
		Long: `kintone は kintone API を操作するための CLI ツールです。
全コマンドは JSON 形式（{"ok":true,"data":{...}}）で結果を出力します。`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(newVersionCmd())
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
