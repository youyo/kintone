package main

import (
	"os"

	"github.com/youyo/kintone/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
