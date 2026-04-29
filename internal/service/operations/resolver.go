package operations

import (
	"context"

	"github.com/youyo/kintone/internal/resolver"
)

// resolveAppID は App / AppRef のハイブリッド解決ヘルパ（M08）。
//
// 排他規則:
//   - app > 0 & appRef == ""    → app をそのまま返す（既存 M04 経路の後方互換）
//   - app == 0 & appRef != ""   → resolver で appRef → ID に解決
//   - app > 0 & appRef != ""    → ErrConflictingAppRef
//   - app == 0 & appRef == ""   → ErrInvalidApp
//
// resolver が nil で appRef が指定されている場合は ErrResolverUnavailable。
// 直 ID 指定（app > 0）のときは resolver 不要。
func resolveAppID(ctx context.Context, r *resolver.Resolver, app int64, appRef string) (int64, error) {
	hasApp := app > 0
	hasRef := appRef != ""
	if hasApp && hasRef {
		return 0, ErrConflictingAppRef
	}
	if !hasApp && !hasRef {
		return 0, ErrInvalidApp
	}
	if hasApp {
		return app, nil
	}
	if r == nil {
		return 0, ErrResolverUnavailable
	}
	return r.ResolveApp(ctx, appRef)
}

// resolveUpdateKeyField は UpdateKeyField / UpdateKeyFieldRef のハイブリッド解決ヘルパ（M08）。
//
// 排他規則:
//   - field != "" & fieldRef == ""   → field をそのまま返す（後方互換）
//   - field == "" & fieldRef != ""   → resolver で fieldRef → field code に解決
//   - field != "" & fieldRef != ""   → ErrConflictingUpdateKeyFieldRef
//   - field == "" & fieldRef == ""   → "" を返す（呼び出し側で「未指定」と扱う）
//
// fieldRef が指定されているとき appID は事前に解決済みである必要がある。
func resolveUpdateKeyField(ctx context.Context, r *resolver.Resolver, appID int64, field, fieldRef string) (string, error) {
	hasField := field != ""
	hasRef := fieldRef != ""
	if hasField && hasRef {
		return "", ErrConflictingUpdateKeyFieldRef
	}
	if hasField {
		return field, nil
	}
	if !hasRef {
		return "", nil
	}
	if r == nil {
		return "", ErrResolverUnavailable
	}
	return r.ResolveField(ctx, appID, fieldRef)
}
