package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	LogLevel        string        `mapstructure:"log_level"`
	LogFormat       string        `mapstructure:"log_format"`
	PollingInterval time.Duration `mapstructure:"polling_interval"`
	GracefulTimeout time.Duration `mapstructure:"graceful_timeout"`
	
	// API Configuration
	APIBaseURLs     []string `mapstructure:"api_base_urls"`     // List of API URLs to poll
	APITenant       string   `mapstructure:"api_tenant"`
	LabelSelector   string   `mapstructure:"label_selector"`
	APIEndpoint     string   `mapstructure:"api_endpoint"`
	JWTToken        string   `mapstructure:"jwt_token"`
	
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

// GetAPIBaseURLs returns the list of API URLs
func (c *Config) GetAPIBaseURLs() []string {
	return c.APIBaseURLs
}

// String returns a formatted string representation of the configuration
func (c *Config) String() string {
	urls := c.GetAPIBaseURLs()
	return fmt.Sprintf("LogLevel=%s, LogFormat=%s, PollingInterval=%v, GracefulTimeout=%v, APIBaseURLs=%v",
		c.LogLevel, c.LogFormat, c.PollingInterval, c.GracefulTimeout, urls)
}

func LoadConfig() (*Config, error) {
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "json")
	viper.SetDefault("polling_interval", "30s")
	viper.SetDefault("graceful_timeout", "30s")
	viper.SetDefault("api_base_urls", []string{})
	viper.SetDefault("api_tenant", "default")
	viper.SetDefault("label_selector", "private=false,rhobs-synthetics/status=pending")
	viper.SetDefault("api_endpoint", "/api/metrics/v1")
	viper.SetDefault("jwt_token", "")
	viper.SetDefault("kube_config", "")
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

	// Handle comma-separated API URLs from environment variables
	if apiURLsStr := viper.GetString("api_base_urls"); apiURLsStr != "" && len(cfg.APIBaseURLs) == 0 {
		urls := strings.Split(apiURLsStr, ",")
		for i, url := range urls {
			urls[i] = strings.TrimSpace(url)
		}
		cfg.APIBaseURLs = urls
	}

	return &cfg, nil
}
