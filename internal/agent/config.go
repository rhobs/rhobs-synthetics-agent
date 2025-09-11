package agent

import (
	"fmt"
	"regexp"
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

	// Prometheus Configuration
	Prometheus PrometheusConfig `mapstructure:"prometheus"`

	// Blackbox Configuration
	Blackbox k8s.BlackboxConfig `mapstructure:"blackbox"`
}

type PrometheusConfig struct {
	RemoteWriteURL    string `mapstructure:"remote_write_url"`
	RemoteWriteTenant string `mapstructure:"remote_write_tenant"`
	CPURequests       string `mapstructure:"cpu_requests"`
	CPULimits         string `mapstructure:"cpu_limits"`
	MemoryRequests    string `mapstructure:"memory_requests"`
	MemoryLimits      string `mapstructure:"memory_limits"`
}

// Validate validates the PrometheusConfig fields
func (pc *PrometheusConfig) Validate() error {

	// Validate resource strings only if they're not empty
	// Empty values will use the defaults from cobra flags
	if pc.CPURequests != "" {
		if err := validateResourceString(pc.CPURequests, "cpu_requests"); err != nil {
			return err
		}
	}
	if pc.CPULimits != "" {
		if err := validateResourceString(pc.CPULimits, "cpu_limits"); err != nil {
			return err
		}
	}
	if pc.MemoryRequests != "" {
		if err := validateResourceString(pc.MemoryRequests, "memory_requests"); err != nil {
			return err
		}
	}
	if pc.MemoryLimits != "" {
		if err := validateResourceString(pc.MemoryLimits, "memory_limits"); err != nil {
			return err
		}
	}

	return nil
}

// validateResourceString validates Kubernetes resource string formats
// Assumes value is not empty (caller should check for emptiness)
func validateResourceString(value, fieldName string) error {
	// Kubernetes resource patterns
	// CPU: millicores (m) or full cores (no suffix), e.g., "100m", "0.5", "2"
	// Memory: bytes with suffixes like Ki, Mi, Gi, Ti, Pi, Ei or K, M, G, T, P, E
	cpuPattern := `^(\d+(\.\d+)?|\d+m)$`
	memoryPattern := `^(\d+(\.\d+)?[KMGTPE]?i?|\d+)$`

	var pattern string
	if strings.Contains(fieldName, "cpu") {
		pattern = cpuPattern
	} else if strings.Contains(fieldName, "memory") {
		pattern = memoryPattern
	} else {
		return nil // Unknown field type, skip validation
	}

	matched, err := regexp.MatchString(pattern, value)
	if err != nil {
		return fmt.Errorf("error validating %s format: %w", fieldName, err)
	}
	if !matched {
		return fmt.Errorf("invalid %s format: %s (expected Kubernetes resource format)", fieldName, value)
	}

	return nil
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
	viper.SetDefault("label_selector", "private=false,rhobs-synthetics/status=pending")
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

	// Validate Prometheus configuration
	if err := cfg.Prometheus.Validate(); err != nil {
		return nil, fmt.Errorf("invalid prometheus configuration: %w", err)
	}

	return &cfg, nil
}
