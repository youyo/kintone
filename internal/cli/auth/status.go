package auth

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/youyo/kintone/internal/config"
	"github.com/youyo/kintone/internal/output"
	"github.com/youyo/kintone/internal/store"
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

	ts, cleanup, err := getTokenStore(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	entries, err := listOAuthTokens(ctx, ts, r.Domain, principalID)
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
//
// principalID 指定時は単一 Get、未指定時は ListByDomain で全件取得する。
// store.TokenStore interface は ListByDomain をサポートするため、旧 SQLite 直接 SQL を
// 経由する必要は無い。
func listOAuthTokens(ctx context.Context, ts store.TokenStore, domain, principalID string) ([]statusEntry, error) {
	var tokens []*store.Token

	if principalID != "" {
		tok, err := ts.Get(ctx, domain, principalID, store.AuthTypeOAuth)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return []statusEntry{}, nil
			}
			return nil, err
		}
		tokens = []*store.Token{tok}
	} else {
		ts2, err := ts.ListByDomain(ctx, domain, store.AuthTypeOAuth)
		if err != nil {
			return nil, err
		}
		tokens = ts2
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
