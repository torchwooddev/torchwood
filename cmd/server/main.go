package main

import (
	"context"
	"log"
	"time"

	"github.com/joho/godotenv"
	"github.com/lynx-go/lynx"
	"github.com/lynx-go/lynx/contrib/zap"
	"github.com/spf13/pflag"
	config "github.com/torchwoodio/torchwood/internal/pkg/config"
)

var version string

func main() {
	_ = godotenv.Load()

	o := lynx.NewOptions(
		lynx.WithName("Torchwood"),
		lynx.WithVersion(version),
		lynx.WithSetFlagsFunc(func(f *pflag.FlagSet) {
			f.String("config-dir", "./configs", "config file path")
			f.String("log-level", "info", "log level")
		}),
		lynx.WithBindConfigFunc(config.NewBindConfigFunc()),
		lynx.WithCloseTimeout(30*time.Second),
	)

	app := lynx.New(o, func(ctx context.Context, app lynx.Lynx) error {
		app.SetLogger(zap.MustNewLogger(app))

		bootstrap, cleanup, err := wireBootstrap(app)
		if err != nil {
			log.Fatal(err)
		}
		if err := app.Hooks(lynx.OnStop(func(ctx context.Context) error {
			cleanup()
			return nil
		})); err != nil {
			return err
		}
		return bootstrap.Bind(app)
	})
	app.Run()
}
