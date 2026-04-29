package resolver

import (
	"context"
	"sort"
	"strings"

	"github.com/youyo/kintone/internal/kintoneapi"
)

// ResolveField は appID と ref（code・label・partial）から field code を返す。
//
// 解決順序:
//  1. code 完全一致（properties のキー）
//  2. label 完全一致
//  3. label 部分一致（strings.Contains）
//
// 各段階でヒットしたら即 return（fallback しない）。
//
// 戻り値:
//   - 成功: fieldCode != ""
//   - not found: *NotFoundError
//   - ambiguous: *AmbiguousError
//   - empty ref: ErrEmptyRef
//   - invalid appID: ErrInvalidAppID
//   - kintone REST エラー: *kintoneapi.APIError 透過
func (r *Resolver) ResolveField(ctx context.Context, appID int64, ref string) (string, error) {
	if ref == "" {
		return "", ErrEmptyRef
	}
	if appID <= 0 {
		return "", ErrInvalidAppID
	}
	resp, err := r.api.GetFormFields(ctx, kintoneapi.GetFormFieldsRequest{App: appID})
	if err != nil {
		return "", err
	}
	// step 1: code 完全一致
	if _, ok := resp.Properties[ref]; ok {
		return ref, nil
	}
	// step 2-3: label 検索
	var exact, partial []Candidate
	for code, prop := range resp.Properties {
		label, ok := prop["label"].(string)
		if !ok || label == "" {
			continue
		}
		switch {
		case label == ref:
			exact = append(exact, Candidate{Code: code, Label: label})
		case strings.Contains(label, ref):
			partial = append(partial, Candidate{Code: code, Label: label})
		}
	}
	// map iteration order は非決定的なので Code でソートして安定化
	sortCandidates(exact)
	sortCandidates(partial)

	if len(exact) == 1 {
		return exact[0].Code, nil
	}
	if len(exact) > 1 {
		return "", &AmbiguousError{Kind: "field", Ref: ref, Candidates: exact}
	}
	if len(partial) == 1 {
		return partial[0].Code, nil
	}
	if len(partial) > 1 {
		return "", &AmbiguousError{Kind: "field", Ref: ref, Candidates: partial}
	}
	return "", &NotFoundError{Kind: "field", Ref: ref}
}

// sortCandidates は Candidate スライスを Code 昇順でソートする（決定論的順序のため）。
func sortCandidates(cs []Candidate) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].Code < cs[j].Code })
}
