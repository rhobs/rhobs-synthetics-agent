package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/k8s"
	"github.com/spf13/viper"
)

type Config struct {
	LogLevel        string        `mapstructure:"log_level"`
	LogFormat       string        `mapstructure:"log_format"`
	PollingInterval time.Duration `mapstructure:"polling_interval"`
	GracefulTimeout time.Duration `mapstructure:"graceful_timeout"`

	// API Configuration
	APIURLs       []string `mapstructure:"api_urls"` // List of complete API URLs to poll
	LabelSelector string   `mapstructure:"label_selector"`
	JWTToken      string   `mapstructure:"jwt_token"`

	// Kubernetes Configuration
	KubeConfig string `mapstructure:"kube_config"`
	Namespace  string `mapstructure:"namespace"`

	// Blackbox Configuration
	Blackbox k8s.BlackboxConfig `mapstructure:"blackbox"`
}

// GetAPIURLs returns the list of complete API URLs
func (c *Config) GetAPIURLs() []string {
	return c.APIURLs
}

// String returns a formatted string representation of the configuration
func (c *Config) String() string {
	urls := c.GetAPIURLs()
	return fmt.Sprintf("LogLevel=%s, LogFormat=%s, PollingInterval=%v, GracefulTimeout=%v, APIURLs=%v",
		c.LogLevel, c.LogFormat, c.PollingInterval, c.GracefulTimeout, urls)
}

func LoadConfig() (*Config, error) {
	viper.SetDefault("log_level", "info")
	viper.SetDefault("log_format", "json")
	viper.SetDefault("polling_interval", "30s")
	viper.SetDefault("graceful_timeout", "30s")
	viper.SetDefault("api_urls", []string{})
	viper.SetDefault("label_selector", "private=false")
	viper.SetDefault("jwt_token", "")
	viper.SetDefault("kube_config", "")
	viper.SetDefault("namespace", "default")

	k8s.LoadBlackboxDefaults()

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

	// Handle comma-separated API URLs from environment variables or string values
	if apiURLsStr := viper.GetString("api_urls"); apiURLsStr != "" {
		// Check if this is a comma-separated string (contains commas) or if APIURLs came from string parsing
		if strings.Contains(apiURLsStr, ",") || len(cfg.APIURLs) == 0 {
			urls := strings.Split(apiURLsStr, ",")
			for i, url := range urls {
				urls[i] = strings.TrimSpace(url)
			}
			cfg.APIURLs = urls
		} else if len(cfg.APIURLs) > 0 {
			// APIURLs was populated by viper's automatic parsing, but we should still trim whitespace
			for i, url := range cfg.APIURLs {
				cfg.APIURLs[i] = strings.TrimSpace(url)
			}
		}
	}

	return &cfg, nil
}
