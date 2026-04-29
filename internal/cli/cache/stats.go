package cache

import (
	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/cache"
	"github.com/youyo/kintone/internal/output"
)

// newStatsCmd は `kintone cache stats` コマンドを構築する。
//
// DB ファイル不在時は {"ok":true,"data":{"db_exists":false,...}} を返す。
// DB を auto-create しない（advisor 指摘 #5）。
func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "キャッシュの統計情報を JSON で出力する",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, exists, err := NewStoreBuilder()
			if err != nil {
				return err
			}

			var stats cache.Stats
			if !exists || store == nil {
				// DB ファイルが存在しない場合は合成 Stats を返す
				stats = cache.Stats{
					DBPath:   cachePath(),
					DBExists: false,
				}
			} else {
				defer func() { _ = store.Close() }()
				stats, err = store.Stats(cmd.Context())
				if err != nil {
					return err
				}
			}

			payload, err := output.Success(stats)
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
}
