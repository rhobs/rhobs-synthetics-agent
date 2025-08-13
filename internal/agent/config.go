package agent

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	LogLevel        string        `mapstructure:"log_level"`
	LogFormat       string        `mapstructure:"log_format"`
	PollingInterval time.Duration `mapstructure:"polling_interval"`
	GracefulTimeout time.Duration `mapstructure:"graceful_timeout"`
}

// String returns a formatted string representation of the configuration
func (c *Config) String() string {
	return fmt.Sprintf("LogLevel=%s, LogFormat=%s, PollingInterval=%v, GracefulTimeout=%v",
		c.LogLevel, c.LogFormat, c.PollingInterval, c.GracefulTimeout)
}

func LoadConfig() (*Config, error) {
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "json")
	viper.SetDefault("polling_interval", "30s")
	viper.SetDefault("graceful_timeout", "30s")

	viper.AutomaticEnv()

	if configFile := viper.GetString("config"); configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
