package main

import (
	"context"
	"time"

	"github.com/joho/godotenv"
	"github.com/lynx-go/lynx"
	lynxzap "github.com/lynx-go/lynx/contrib/zap"
	"github.com/spf13/pflag"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
)

var version string

func main() {
	_ = godotenv.Load()

	runner := lynx.NewRunner(func(app lynx.App) error {
		app.SetLogger(lynxzap.MustNewLogger(app))

		bootstrap, cleanup, err := wireBootstrap(app)
		if err != nil {
			return err
		}
		app.OnStop(func(ctx context.Context) error {
			cleanup()
			return nil
		})
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
		lynx.WithShutdownTimeout(30*time.Second),
	)
	runner.Run()
}
