package auth

import (
	"context"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/tokenstore"
)

// statusEntry は `kintone auth status` の配列 1 要素。
type statusEntry struct {
	PrincipalID     string `json:"principal_id"`
	Domain          string `json:"domain"`
	AccessToken     string `json:"access_token"` // マスク済み
	HasRefreshToken bool   `json:"has_refresh_token"`
	ExpiresAt       string `json:"expires_at"` // RFC3339
	Scope           string `json:"scope,omitempty"`
}

// newStatusCmd は `kintone auth status` コマンドを構築する。
func newStatusCmd() *cobra.Command {
	var principalID string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "OAuth トークンの状態を表示する",
		Long: `TokenStore に保存された OAuth トークンの状態を JSON で出力します。
access_token は先頭4文字 + "..." + 末尾4文字にマスクされます。`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), cmd.OutOrStdout(), principalID)
		},
	}
	cmd.Flags().StringVar(&principalID, "principal-id", "", "特定の principal-id のみ表示（省略時はドメイン内全件）")
	return cmd
}

// runStatus は status コマンドの本体ロジック。
func runStatus(ctx context.Context, out io.Writer, principalID string) error {
	r, err := config.Load(config.LoadOptions{})
	if err != nil {
		return err
	}

	store, err := openTokenStoreFn()
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	entries, err := listOAuthTokens(ctx, store, r.Domain, principalID)
	if err != nil {
		return err
	}

	payload, err := output.Success(entries)
	if err != nil {
		return err
	}
	return output.Write(out, payload)
}

// listOAuthTokens は TokenStore から OAuth エントリを取得して statusEntry に変換する。
// principalID が空の場合は既知の全エントリを返す（M09 では全スキャン API がないため
// 記録済みの principalID を全件スキャンする機能は SQLite の直接 SELECT で実装）。
func listOAuthTokens(ctx context.Context, store tokenstore.Store, domain, principalID string) ([]statusEntry, error) {
	// M09 では tokenstore.Store interface に ListByDomain は存在しない。
	// SQLiteStore の直接 SQL は interface 外なので、Store interface 経由で
	// 個別 Get しか呼べない。
	// → principalID 指定なしの場合は SQLiteStore を型アサーションして直接 SQL を使う。
	// テスト時は mockStore に ListByDomain メソッドがあれば使う（interface 拡張）。

	type listable interface {
		ListByDomain(ctx context.Context, domain string, authType tokenstore.AuthType) ([]*tokenstore.Token, error)
	}

	var tokens []*tokenstore.Token

	if principalID != "" {
		// 特定 principal のみ
		tok, err := store.Get(ctx, domain, principalID, tokenstore.AuthTypeOAuth)
		if err != nil {
			if err == tokenstore.ErrNotFound {
				return []statusEntry{}, nil
			}
			return nil, err
		}
		tokens = []*tokenstore.Token{tok}
	} else if ls, ok := store.(listable); ok {
		// ListByDomain が使えるとき（SQLiteStore + テスト用 store）
		var err error
		tokens, err = ls.ListByDomain(ctx, domain, tokenstore.AuthTypeOAuth)
		if err != nil {
			return nil, err
		}
	} else {
		// interface が ListByDomain を持たない場合は空を返す（M09 制約）
		tokens = []*tokenstore.Token{}
	}

	entries := make([]statusEntry, 0, len(tokens))
	for _, tok := range tokens {
		entries = append(entries, statusEntry{
			PrincipalID:     tok.PrincipalID,
			Domain:          tok.Domain,
			AccessToken:     maskToken(tok.AccessToken),
			HasRefreshToken: tok.RefreshToken != "",
			ExpiresAt:       tok.ExpiresAt.Format(time.RFC3339),
		})
	}
	return entries, nil
}

// maskToken は access_token を先頭4文字 + "..." + 末尾4文字にマスクする。
// 8 文字未満は全マスク "***"。
func maskToken(token string) string {
	if len(token) < 8 {
		return "***"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

// ExecuteStatusWith はテスト用エントリポイント。
func ExecuteStatusWith(args []string, out, errOut io.Writer) error {
	cmd := newStatusCmd()
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
