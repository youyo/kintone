// Package main は kintone CLI のバイナリエントリポイント。
//
// 4 種の Storage backend（memory / sqlite / redis / dynamodb）を blank import で
// 集約し、init() による [store.RegisterOpener] を強制する。これにより
// [store.OpenFromConfig] / [store.OpenFromEnv] が backend を知らずに dispatch でき、
// factory パッケージは backend を直接 import せずに済む（循環依存を回避）。
package main

import (
	"os"

	"github.com/youyo/kintone/internal/cli"

	// Storage backends を blank import で集約（register パターンの強制策）。
	// 各パッケージの init() が store.RegisterOpener を呼ぶ。
	_ "github.com/youyo/kintone/internal/store/dynamodb"
	_ "github.com/youyo/kintone/internal/store/memory"
	_ "github.com/youyo/kintone/internal/store/redis"
	_ "github.com/youyo/kintone/internal/store/sqlite"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
