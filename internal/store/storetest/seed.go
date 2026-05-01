package storetest

import (
	"context"
	"fmt"
	"time"

	"github.com/youyo/kintone/internal/store"
)

// SeedTokenForE2E は E2E テスト用に Container.Tokens() に Token を直接書き込む。
//
// このヘルパは E2E テスト専用であり、本番コードから呼んではならない。
// `kintone auth login --oauth` のブラウザ対話フローを CI 上で省略するために提供される。
//
// UpdatedAt が zero の場合は現在時刻で埋める。
func SeedTokenForE2E(ctx context.Context, container store.Container, t store.Token) error {
	if container == nil {
		return fmt.Errorf("storetest: nil container")
	}
	ts, err := container.Tokens()
	if err != nil {
		return fmt.Errorf("storetest: tokens accessor: %w", err)
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = time.Now()
	}
	if err := ts.Put(ctx, t); err != nil {
		return fmt.Errorf("storetest: put token: %w", err)
	}
	return nil
}
