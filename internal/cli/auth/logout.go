package auth

import (
	"context"
	"io"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/cli/clierr"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/tokenstore"
)

// logoutResult は `kintone auth logout` の成功時 data 部分。
type logoutResult struct {
	DeletedCount int `json:"deleted_count"`
}

// newLogoutCmd は `kintone auth logout` コマンドを構築する。
func newLogoutCmd() *cobra.Command {
	var principalID string
	var all bool

	cmd := &cobra.Command{
		Use:   "logout",
		Short: "OAuth トークンを削除する",
		Long: `TokenStore から OAuth トークンを削除します。

--principal-id で特定ユーザーを指定するか、
--all でドメイン内の全 OAuth トークンを削除します。`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogout(cmd.Context(), cmd.OutOrStdout(), principalID, all)
		},
	}
	cmd.Flags().StringVar(&principalID, "principal-id", "", "削除する principal-id（--all と排他）")
	cmd.Flags().BoolVar(&all, "all", false, "ドメイン内の全 OAuth トークンを削除する")
	return cmd
}

// runLogout は logout コマンドの本体ロジック。
func runLogout(ctx context.Context, out io.Writer, principalID string, all bool) error {
	// --principal-id と --all の排他チェック
	if principalID == "" && !all {
		return &clierr.UsageError{Msg: "oauth: --principal-id または --all を指定してください"}
	}

	r, err := config.Load(config.LoadOptions{})
	if err != nil {
		return err
	}

	store, err := openTokenStoreFn()
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	deleted, err := deleteOAuthTokens(ctx, store, r.Domain, principalID, all)
	if err != nil {
		return err
	}

	payload, err := output.Success(logoutResult{DeletedCount: deleted})
	if err != nil {
		return err
	}
	return output.Write(out, payload)
}

// deleteOAuthTokens は TokenStore から OAuth トークンを削除し、削除数を返す。
func deleteOAuthTokens(ctx context.Context, store tokenstore.Store, domain, principalID string, all bool) (int, error) {
	type listable interface {
		ListByDomain(ctx context.Context, domain string, authType tokenstore.AuthType) ([]*tokenstore.Token, error)
	}

	if !all {
		// 特定 principal のみ削除
		// 削除前に存在確認（Delete は no-op で不在でも error を返さないため）
		_, getErr := store.Get(ctx, domain, principalID, tokenstore.AuthTypeOAuth)
		if getErr == tokenstore.ErrNotFound {
			return 0, nil
		}
		if getErr != nil {
			return 0, getErr
		}
		if err := store.Delete(ctx, domain, principalID, tokenstore.AuthTypeOAuth); err != nil {
			return 0, err
		}
		return 1, nil
	}

	// --all: ドメイン内全 OAuth エントリを削除
	ls, ok := store.(listable)
	if !ok {
		// ListByDomain が使えない場合は 0 を返す（Store が限定的な interface 実装の場合）
		return 0, nil
	}

	tokens, err := ls.ListByDomain(ctx, domain, tokenstore.AuthTypeOAuth)
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, tok := range tokens {
		if delErr := store.Delete(ctx, tok.Domain, tok.PrincipalID, tokenstore.AuthTypeOAuth); delErr != nil {
			return deleted, delErr
		}
		deleted++
	}
	return deleted, nil
}

// ExecuteLogoutWith はテスト用エントリポイント。
func ExecuteLogoutWith(args []string, out, errOut io.Writer) error {
	cmd := newLogoutCmd()
	cmd.SetArgs(args)
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	if err := cmd.Execute(); err != nil {
		oe := mapOAuthError(err)
		payload, _ := output.Failure(oe)
		_ = output.Write(out, payload)
		return err
	}
	return nil
}
