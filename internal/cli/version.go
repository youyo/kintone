package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/output"
)

// Version はバイナリのバージョン文字列。
// var として宣言することで GoReleaser が ldflags 経由で注入できる。
//
//	go build -ldflags "-X github.com/youyo/kintone/internal/cli.Version=v1.2.3"
//
// const にしない理由: const は ldflags で書き換え不可のため。
var Version = "0.1.0"

// Commit はビルド時の git commit hash。M11（GoReleaser）で ldflags 注入予定。
// M1 では空文字のため JSON に出現しない（omitempty）。
var Commit = ""

// Date はビルド日時。M11（GoReleaser）で ldflags 注入予定。
// M1 では空文字のため JSON に出現しない（omitempty）。
var Date = ""

// versionPayload は kintone version コマンドの data 部分。
// map ではなく struct を使うことで JSON フィールド順序を保証する。
// Commit / Date は ldflags 注入時のみ含まれる（omitempty）。
type versionPayload struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

// newVersionCmd は version サブコマンドを構築して返す。
func newVersionCmd() *cobra.Command {
	var short bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "バージョン情報を表示する",
		Long:  "kintone のバージョン情報を JSON 形式で表示する。--short フラグでプレーンテキスト出力に切り替えられる。",
		RunE: func(cmd *cobra.Command, args []string) error {
			if short {
				// 規約例外: プレーンテキスト出力（人間/シェルスクリプト向け）
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), Version); err != nil {
					return err
				}
				return nil
			}
			payload, err := output.Success(versionPayload{
				Version: Version,
				Commit:  Commit,
				Date:    Date,
			})
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
	cmd.Flags().BoolVar(&short, "short", false, "バージョン番号のみをプレーンテキストで出力する")
	return cmd
}
