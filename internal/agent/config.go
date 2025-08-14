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
	
	// API Configuration
	APIBaseURL      string `mapstructure:"api_base_url"`
	APITenant       string `mapstructure:"api_tenant"`
	LabelSelector   string `mapstructure:"label_selector"`
	APIEndpoint     string `mapstructure:"api_endpoint"`
	JWTToken        string `mapstructure:"jwt_token"`
	
	// Kubernetes Configuration
	KubeConfig      string `mapstructure:"kube_config"`
	Namespace       string `mapstructure:"namespace"`
	
	// Blackbox Configuration
	Blackbox BlackboxConfig `mapstructure:"blackbox"`
}

// BlackboxConfig holds configuration for blackbox exporter probes
type BlackboxConfig struct {
	Interval  string `mapstructure:"interval"`
	Module    string `mapstructure:"module"`
	ProberURL string `mapstructure:"prober_url"`
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
	viper.SetDefault("api_base_url", "")
	viper.SetDefault("api_tenant", "default")
	viper.SetDefault("label_selector", "private=false,rhobs-synthetics/status=pending")
	viper.SetDefault("api_endpoint", "/api/metrics/v1")
	viper.SetDefault("jwt_token", "")
	viper.SetDefault("namespace", "default")
	
	// Blackbox defaults
	viper.SetDefault("blackbox.interval", "30s")
	viper.SetDefault("blackbox.module", "http_2xx")
	viper.SetDefault("blackbox.prober_url", "http://blackbox-exporter:9115")

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
