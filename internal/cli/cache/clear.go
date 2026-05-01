package cache

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/store"
)

// scopePrefixMap は --scope 値をキャッシュキーの prefix に変換するマップ。
//
// プレフィックスは v1 スキーマ（store.KeyPrefix*）に準拠する。
var scopePrefixMap = map[string]string{
	"apps":      store.KeyPrefixApps,
	"fields":    store.KeyPrefixFields,
	"list_apps": store.KeyPrefixListApps,
	"all":       "v1:",
}

// clearResult は `kintone cache clear` の JSON 出力形式。
type clearResult struct {
	Scope   string `json:"scope,omitempty"`
	Key     string `json:"key,omitempty"`
	Deleted int    `json:"deleted"`
}

// newClearCmd は `kintone cache clear` コマンドを構築する。
//
// --scope <apps|fields|list_apps|all> で論理スコープを指定するか、
// --key <prefix> で raw キープレフィックスを直接指定する（高度デバッグ用）。
// 両方指定は USAGE エラー。
func newClearCmd() *cobra.Command {
	var scope string
	var key string

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "キャッシュエントリを削除する",
		Long: `キャッシュエントリを削除します。

スコープ指定:
  --scope apps       アプリ情報キャッシュ（v1:app: prefix）
  --scope fields     フィールド定義キャッシュ（v1:fields: prefix）
  --scope list_apps  アプリ一覧キャッシュ（v1:list_apps: prefix）
  --scope all        全キャッシュ（デフォルト）

キー指定（高度デバッグ用、--scope と排他）:
  --key <prefix>     任意の raw キープレフィックスで削除`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// --scope と --key の排他チェック（--scope はデフォルト値 "all" だが、
			// flag が明示的に変更されたかどうかで判定する）
			scopeChanged := cmd.Flags().Changed("scope")
			keyChanged := cmd.Flags().Changed("key")
			if scopeChanged && keyChanged {
				return clierr.NewUsageError("--scope and --key are mutually exclusive")
			}

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

			var prefix string
			var result clearResult
			if keyChanged {
				if key == "" {
					return clierr.NewUsageError("--key must not be empty")
				}
				prefix = key
				result.Key = key
			} else {
				p, ok := scopePrefixMap[scope]
				if !ok {
					return clierr.NewUsageError("--scope must be one of: apps, fields, list_apps, all")
				}
				prefix = p
				result.Scope = scope
			}

			deleted, err := cs.DeleteByPrefix(ctx, prefix)
			if err != nil {
				return fmt.Errorf("cache: delete by prefix: %w", err)
			}
			result.Deleted = deleted

			payload, err := output.Success(result)
			if err != nil {
				return err
			}
			return output.Write(cmd.OutOrStdout(), payload)
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "all", "削除スコープ: apps|fields|list_apps|all (--key と排他)")
	cmd.Flags().StringVar(&key, "key", "", "raw キープレフィックスで削除する（高度デバッグ用、--scope と排他）")
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd
}

// ExecuteCacheClearWith はテスト用エントリポイント。
// `kintone cache clear` のサブツリー単独実行を可能にする。
func ExecuteCacheClearWith(args []string, out, errOut io.Writer) error {
	cmd := newClearCmd()
	cmd.SetArgs(args)
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	if err := cmd.Execute(); err != nil {
		oe := mapCacheError(err)
		payload, _ := output.Failure(oe)
		_ = output.Write(out, payload)
		return err
	}
	return nil
}
