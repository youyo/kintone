package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

// HomeDirFunc は UserHomeDir を差し替え可能にするためのテスト hook。
type HomeDirFunc func() (string, error)

// GetenvFunc は os.Getenv の差し替え用。
type GetenvFunc func(string) string

// DefaultCachePath はキャッシュ DB の既定パスを返す。
//
// 優先順:
//  1. KINTONE_CACHE_PATH 環境変数
//  2. $HOME/.cache/kintone/cache.db
//
// container 利用時は Dockerfile 側で `ENV KINTONE_CACHE_PATH=/data/kintone/cache.db` を
// 明示すること。CLI 内に /data 自動検出ヒューリスティックは持たない（予測可能性優先）。
func DefaultCachePath(getenv GetenvFunc, userHomeDir HomeDirFunc) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if userHomeDir == nil {
		userHomeDir = os.UserHomeDir
	}
	if v := getenv("KINTONE_CACHE_PATH"); v != "" {
		return v, nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("cache: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".cache", "kintone", "cache.db"), nil
}

// DefaultTokensPath はトークン DB の既定パスを返す。
//
// 優先順:
//  1. KINTONE_TOKENS_PATH 環境変数
//  2. $HOME/.cache/kintone/tokens.db
//
// cache.db とは別ファイル（ライフサイクル分離 / WAL 競合回避）。
func DefaultTokensPath(getenv GetenvFunc, userHomeDir HomeDirFunc) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if userHomeDir == nil {
		userHomeDir = os.UserHomeDir
	}
	if v := getenv("KINTONE_TOKENS_PATH"); v != "" {
		return v, nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("cache: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".cache", "kintone", "tokens.db"), nil
}
