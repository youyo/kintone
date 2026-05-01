package cache

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/output"
)

// newStatsCmd は `kintone cache stats` コマンドを構築する。
//
// JSON schema (M12 Phase 6b 以降):
//
//	{
//	  "ok": true,
//	  "data": {
//	    "backend": "memory|sqlite|redis|dynamodb",
//	    "location": "memory:// | file:///... | redis://... | dynamodb://...",
//	    "reachable": true,
//	    "entry_count": N,
//	    "expired_count": N (or null),
//	    "backend_specific": { ... }
//	  }
//	}
//
// 旧 db_path / db_exists / total / oldest_stored は削除（breaking change）。
func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "キャッシュの統計情報を JSON で出力する",
		Long: `Storage backend の cache サブストアから統計情報を取得し、JSON で出力します。

返り値は backend 中立スキーマ:
  backend           memory / sqlite / redis / dynamodb
  location          backend を表す URL 風文字列
  reachable         backend に到達可能か
  entry_count       現在のエントリ数
  expired_count     期限切れエントリ数 (backend が把握できない場合は null)
  backend_specific  backend 固有のメタ情報 (sqlite: db_size_bytes など)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			container, cleanup, err := getContainer(ctx)
			if err != nil {
				return err
			}
			defer cleanup()

			cs, err := container.CacheForAdmin()
			if err != nil {
				return fmt.Errorf("cache: admin accessor: %w", err)
			}

			stats, err := cs.Stats(ctx)
			if err != nil {
				return fmt.Errorf("cache: stats: %w", err)
			}

			payload, err := output.Success(stats)
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}
}

// ExecuteCacheStatsWith はテスト用エントリポイント。
// `kintone cache stats` のサブツリー単独実行 (cli.ExecuteWith の PersistentPreRunE
// による Container 注入を回避し、SetNewContainerBuilder hook を発火させる) を可能にする。
func ExecuteCacheStatsWith(args []string, out, errOut io.Writer) error {
	cmd := newStatsCmd()
	cmd.SetArgs(args)
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	if err := cmd.Execute(); err != nil {
		oe := mapCacheError(err)
		payload, _ := output.Failure(oe)
		_ = output.Write(out, payload)
		return err
	}
	return nil
}

// mapCacheError は cache サブコマンド用の最小限のエラー変換ロジック。
// cli パッケージへの逆依存を避けるため、UsageError と汎用エラーのみを変換する。
func mapCacheError(err error) *output.Error {
	if err == nil {
		return nil
	}
	var ue *clierr.UsageError
	if errors.As(err, &ue) {
		return &output.Error{Code: "USAGE", Message: ue.Error()}
	}
	return &output.Error{Code: "INTERNAL", Message: err.Error()}
}
