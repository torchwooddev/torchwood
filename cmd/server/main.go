package main

import (
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/lynx-go/lynx"
	lynxzap "github.com/lynx-go/lynx/contrib/zap"
	"github.com/spf13/pflag"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
)

var version, commit, date string

func main() {
	_ = godotenv.Load()

	var cleanup func()
	runner := lynx.NewRunner(func(app lynx.App) error {
		app.SetLogger(lynxzap.MustNewLogger(app))

		bootstrap, c, err := wireBootstrap(app)
		if err != nil {
			return err
		}
		cleanup = c
		bootstrap.Bind(app)
		return nil
	},
		lynx.WithName("Torchwood"),
		lynx.WithVersion(version),
		lynx.WithSetFlagsFunc(func(f *pflag.FlagSet) {
			f.String("config-dir", "./configs", "config file path")
			f.String("log-level", "info", "log level")
		}),
		lynx.WithBindConfigFunc(config.NewBindConfigFunc()),
		// 排水窗口：关停信号到达后先置 readiness 为不健康（LB 摘流），
		// 窗口内服务保持运行让在途请求收尾。
		lynx.WithDrainTimeout(30*time.Second),
		lynx.WithShutdownTimeout(30*time.Second),
	)

	// cleanup（关闭 DB/Redis 等底层资源）不能注册进 OnStop：lynx 在服务
	// GracefulStop 之前执行 OnStop hooks，会先关掉连接池导致排水/关停期间
	// 的在途请求失败。这里等 runner.RunE() 返回（所有服务已停止）后再清理。
	err := runner.RunE()
	if cleanup != nil {
		cleanup()
	}
	if err != nil {
		log.Fatalln(err)
	}
}
