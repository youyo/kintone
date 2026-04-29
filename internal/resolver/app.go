package resolver

import (
	"context"
	"strconv"

	"github.com/youyo/kintone/internal/kintoneapi"
)

// ResolveApp は ref（数値文字列・code・name・partial）から App ID を返す。
//
// 解決順序:
//  1. ID 直接（strconv.ParseInt 成功 & id > 0）
//  2. code 完全一致（ListApps Codes パラメータ）
//  3. name 完全一致（ListApps Name パラメータの結果から `Name == ref` を抽出）
//  4. name 部分一致（同レスポンスの残り）
//
// 各段階でヒットしたら即 return（fallback しない / predictability 優先 / M08 計画 #4）。
//
// 戻り値:
//   - 成功: appID > 0
//   - not found: *NotFoundError
//   - ambiguous: *AmbiguousError（candidates: 全候補）
//   - empty ref: ErrEmptyRef
//   - 多数ヒット: ErrAppListTooLarge
//   - kintone REST エラー: *kintoneapi.APIError 透過
func (r *Resolver) ResolveApp(ctx context.Context, ref string) (int64, error) {
	if ref == "" {
		return 0, ErrEmptyRef
	}

	// step 1: ID 直接（数値変換成功 & id > 0 のときのみ採用）
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 {
		return id, nil
	}

	// step 2: code 完全一致
	codeMatches, err := r.listAllAppsByCode(ctx, ref)
	if err != nil {
		return 0, err
	}
	if len(codeMatches) == 1 {
		return parseAppID(codeMatches[0].AppID)
	}
	if len(codeMatches) > 1 {
		return 0, ambiguousAppError(ref, codeMatches)
	}

	// step 3-4: name 検索（kintone REST `name=` は部分一致なので、
	// 自前で完全一致 / 部分一致のみに振り分ける）
	nameMatches, err := r.listAllAppsByName(ctx, ref)
	if err != nil {
		return 0, err
	}
	var exact, partial []kintoneapi.AppListEntry
	for _, a := range nameMatches {
		if a.Name == ref {
			exact = append(exact, a)
		} else {
			partial = append(partial, a)
		}
	}
	if len(exact) == 1 {
		return parseAppID(exact[0].AppID)
	}
	if len(exact) > 1 {
		return 0, ambiguousAppError(ref, exact)
	}
	if len(partial) == 1 {
		return parseAppID(partial[0].AppID)
	}
	if len(partial) > 1 {
		return 0, ambiguousAppError(ref, partial)
	}
	return 0, &NotFoundError{Kind: "app", Ref: ref}
}

// listAllAppsByCode は codes[] パラメータで完全一致検索を行う（1 リクエストで十分）。
func (r *Resolver) listAllAppsByCode(ctx context.Context, code string) ([]kintoneapi.AppListEntry, error) {
	resp, err := r.api.ListApps(ctx, kintoneapi.ListAppsRequest{
		Codes: []string{code},
		Limit: 100,
	})
	if err != nil {
		return nil, err
	}
	return resp.Apps, nil
}

// listAllAppsByName は name= パラメータで部分一致検索を行い、ページングして全件取得する。
//
// 1 ページ 100 件 × maxAppPagesForResolver ページで打ち切り（ErrAppListTooLarge）。
// このメソッドは「最初のページが 100 件未満」を継続条件として使う（< 100 で完了）。
func (r *Resolver) listAllAppsByName(ctx context.Context, name string) ([]kintoneapi.AppListEntry, error) {
	var all []kintoneapi.AppListEntry
	for page := 0; page < maxAppPagesForResolver; page++ {
		resp, err := r.api.ListApps(ctx, kintoneapi.ListAppsRequest{
			Name:   name,
			Limit:  100,
			Offset: int64(page) * 100,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, resp.Apps...)
		if len(resp.Apps) < 100 {
			return all, nil
		}
	}
	return nil, ErrAppListTooLarge
}

// parseAppID は AppListEntry.AppID（kintone REST が文字列で返す）を int64 に変換する。
//
// kintone REST の app id は文字列だが内部的に整数。変換失敗は内部不整合エラー。
func parseAppID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, &NotFoundError{Kind: "app", Ref: s}
	}
	return id, nil
}

// ambiguousAppError は ListApps レスポンスから AmbiguousError を構築する。
func ambiguousAppError(ref string, apps []kintoneapi.AppListEntry) error {
	candidates := make([]Candidate, 0, len(apps))
	for _, a := range apps {
		candidates = append(candidates, Candidate{
			ID:   a.AppID,
			Code: a.Code,
			Name: a.Name,
		})
	}
	return &AmbiguousError{Kind: "app", Ref: ref, Candidates: candidates}
}
