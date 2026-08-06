package config

import (
	"strings"

	"github.com/lynx-go/lynx"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const EnvPrefix = "TORCHWOOD"

var envBoundKeys = []string{
	"security.jwt.secret",
	"data.database.source",
	"data.redis.addr",
	"data.redis.password",
	"storage.s3.access_key_id",
	"storage.s3.secret_access_key",
	"messaging.smtp.host",
	"messaging.smtp.password",
	"messaging.sms.twilio.account_sid",
	"messaging.sms.twilio.auth_token",
	"telemetry.otlp_endpoint",
}

func ConfigureViper(f *pflag.FlagSet, v *viper.Viper, extraPaths ...string) error {
	if err := lynx.DefaultBindConfigFunc(f, v); err != nil {
		return err
	}

	for _, path := range extraPaths {
		if path == "" {
			continue
		}
		v.AddConfigPath(path)
	}

	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	for _, key := range envBoundKeys {
		if err := v.BindEnv(key); err != nil {
			return err
		}
	}

	return nil
}

func NewBindConfigFunc(extraPaths ...string) lynx.BindConfigFunc {
	return func(f *pflag.FlagSet, v *viper.Viper) error {
		return ConfigureViper(f, v, extraPaths...)
	}
}
