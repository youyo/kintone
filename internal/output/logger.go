// Package output の logger は CLI / MCP 共通の構造化ロガーを提供する。
//
// 出力先は os.Stderr 固定（stdout は JSON envelope 専用）。
// レベルは KINTONE_LOG_LEVEL 環境変数（debug/info/warn/error）で制御する。
// 既定は warn（ノイズ最小）。
package output

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	loggerOnce sync.Once
	logger     *slog.Logger
)

// Logger はプロセスグローバルな slog.Logger を返す。
// 初回呼び出し時に KINTONE_LOG_LEVEL を解釈して構築される。
func Logger() *slog.Logger {
	loggerOnce.Do(func() { logger = newLogger(os.Stderr, os.Getenv("KINTONE_LOG_LEVEL")) })
	return logger
}

// newLogger は w / levelEnv からテキストハンドラベースの slog.Logger を構築する。
// テスト用にエクスポートしない。
func newLogger(w io.Writer, levelEnv string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(strings.TrimSpace(levelEnv)) {
	case "debug":
		lv = slog.LevelDebug
	case "info":
		lv = slog.LevelInfo
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelWarn
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lv}))
}

// NewLoggerForTest は test 専用のロガー構築ファクトリ。
// 任意の Writer / レベル文字列でロガーを作る（プロセスグローバルには影響しない）。
//
// Phase 1 レビュー L-2 対応: test seam の提供。
func NewLoggerForTest(w io.Writer, levelEnv string) *slog.Logger {
	return newLogger(w, levelEnv)
}

// SetForTest は test 専用にプロセスグローバル logger を l に差し替え、復元 func を返す。
//
// 復元 func は呼び出し時点の prev に戻す。並列テストで使うのは避けること
// （プロセスグローバルへの差し替えのため）。
//
// 使い方:
//
//	var buf bytes.Buffer
//	restore := output.SetForTest(slog.New(slog.NewTextHandler(&buf, nil)))
//	defer restore()
//	... output.Logger().Warn(...) ...
func SetForTest(l *slog.Logger) (restore func()) {
	// 既存のグローバル logger を確実に初期化してから差し替える。
	// これにより restore 後に Logger() が nil を返さないことを保証する。
	prev := Logger()
	logger = l
	return func() { logger = prev }
}
