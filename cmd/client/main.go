package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"github.com/torchwooddev/torchwood/cmd/client/cmd"
)

// version/commit/date 由 Taskfile build 的 ldflags 注入（与 cmd/server、cmd/worker 一致）。
var version, commit, date string

var (
	_ = commit
	_ = date
)

func main() {
	// 与 cmd/server、cmd/worker 一致：加载仓库根 .env（可选）。
	_ = godotenv.Load()

	if err := cmd.NewRootCmd(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
