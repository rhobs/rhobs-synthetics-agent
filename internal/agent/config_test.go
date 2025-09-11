package agent

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestConfig_String(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name: "default config",
			config: &Config{
				LogLevel:        "info",
				LogFormat:       "json",
				PollingInterval: 30 * time.Second,
				GracefulTimeout: 30 * time.Second,
			},
			expected: "LogLevel=info, LogFormat=json, PollingInterval=30s, GracefulTimeout=30s, APIURLs=[]",
		},
		{
			name: "debug config",
			config: &Config{
				LogLevel:        "debug",
				LogFormat:       "text",
				PollingInterval: 60 * time.Second,
				GracefulTimeout: 45 * time.Second,
			},
			expected: "LogLevel=debug, LogFormat=text, PollingInterval=1m0s, GracefulTimeout=45s, APIURLs=[]",
		},
		{
			name: "custom config",
			config: &Config{
				LogLevel:        "warn",
				LogFormat:       "structured",
				PollingInterval: 2 * time.Minute,
				GracefulTimeout: 90 * time.Second,
			},
			expected: "LogLevel=warn, LogFormat=structured, PollingInterval=2m0s, GracefulTimeout=1m30s, APIURLs=[]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.String()
			if result != tt.expected {
				t.Errorf("Config.String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	// Test default values
	if cfg.LogLevel != "info" {
		t.Errorf("Expected default LogLevel to be 'info', got %q", cfg.LogLevel)
	}

	if cfg.LogFormat != "json" {
		t.Errorf("Expected default LogFormat to be 'json', got %q", cfg.LogFormat)
	}

	if cfg.PollingInterval != 30*time.Second {
		t.Errorf("Expected default PollingInterval to be 30s, got %v", cfg.PollingInterval)
	}

	if cfg.GracefulTimeout != 30*time.Second {
		t.Errorf("Expected default GracefulTimeout to be 30s, got %v", cfg.GracefulTimeout)
	}
}

func TestLoadConfig_EnvironmentVariables(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	// Set environment variables
	_ = os.Setenv("LOG_LEVEL", "debug")
	_ = os.Setenv("LOG_FORMAT", "text")
	_ = os.Setenv("POLLING_INTERVAL", "60s")
	_ = os.Setenv("GRACEFUL_TIMEOUT", "45s")

	defer func() {
		_ = os.Unsetenv("LOG_LEVEL")
		_ = os.Unsetenv("LOG_FORMAT")
		_ = os.Unsetenv("POLLING_INTERVAL")
		_ = os.Unsetenv("GRACEFUL_TIMEOUT")
	}()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("Expected LogLevel to be 'debug', got %q", cfg.LogLevel)
	}

	if cfg.LogFormat != "text" {
		t.Errorf("Expected LogFormat to be 'text', got %q", cfg.LogFormat)
	}

	if cfg.PollingInterval != 60*time.Second {
		t.Errorf("Expected PollingInterval to be 60s, got %v", cfg.PollingInterval)
	}

	if cfg.GracefulTimeout != 45*time.Second {
		t.Errorf("Expected GracefulTimeout to be 45s, got %v", cfg.GracefulTimeout)
	}
}

func TestLoadConfig_ConfigFile(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	// Create a temporary config file
	tmpFile, err := os.CreateTemp("", "agent-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	configContent := `
log_level: error
log_format: structured
polling_interval: 120s
graceful_timeout: 60s
`
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	_ = tmpFile.Close()

	// Set the config file path
	viper.Set("config", tmpFile.Name())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	if cfg.LogLevel != "error" {
		t.Errorf("Expected LogLevel to be 'error', got %q", cfg.LogLevel)
	}

	if cfg.LogFormat != "structured" {
		t.Errorf("Expected LogFormat to be 'structured', got %q", cfg.LogFormat)
	}

	if cfg.PollingInterval != 120*time.Second {
		t.Errorf("Expected PollingInterval to be 120s, got %v", cfg.PollingInterval)
	}

	if cfg.GracefulTimeout != 60*time.Second {
		t.Errorf("Expected GracefulTimeout to be 60s, got %v", cfg.GracefulTimeout)
	}
}

func TestLoadConfig_InvalidConfigFile(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	// Set a non-existent config file
	viper.Set("config", "/nonexistent/path/config.yaml")

	_, err := LoadConfig()
	if err == nil {
		t.Error("Expected LoadConfig() to fail with invalid config file, but it succeeded")
	}
}

func TestLoadConfig_InvalidDuration(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	// Create a temporary config file with invalid duration
	tmpFile, err := os.CreateTemp("", "agent-config-invalid-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	configContent := `
log_level: info
log_format: json
polling_interval: invalid-duration
graceful_timeout: 30s
`
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	_ = tmpFile.Close()

	// Set the config file path
	viper.Set("config", tmpFile.Name())

	_, err = LoadConfig()
	if err == nil {
		t.Error("Expected LoadConfig() to fail with invalid duration, but it succeeded")
	}
}

func TestLoadConfig_EmptyConfig(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	// Create an empty config file
	tmpFile, err := os.CreateTemp("", "agent-config-empty-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	_ = tmpFile.Close()

	// Set the config file path
	viper.Set("config", tmpFile.Name())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	// Should still have defaults even with empty config file
	if cfg.LogLevel != "info" {
		t.Errorf("Expected default LogLevel to be 'info', got %q", cfg.LogLevel)
	}

	if cfg.LogFormat != "json" {
		t.Errorf("Expected default LogFormat to be 'json', got %q", cfg.LogFormat)
	}
}

func TestLoadConfig_MixedSources(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	// Set some environment variables
	_ = os.Setenv("LOG_LEVEL", "debug")
	_ = os.Setenv("POLLING_INTERVAL", "90s")

	defer func() {
		_ = os.Unsetenv("LOG_LEVEL")
		_ = os.Unsetenv("POLLING_INTERVAL")
	}()

	// Create a config file with some values
	tmpFile, err := os.CreateTemp("", "agent-config-mixed-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	configContent := `
log_format: custom
graceful_timeout: 120s
`
	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	_ = tmpFile.Close()

	// Set the config file path
	viper.Set("config", tmpFile.Name())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	// Environment variables should override config file
	if cfg.LogLevel != "debug" {
		t.Errorf("Expected LogLevel to be 'debug' (from env), got %q", cfg.LogLevel)
	}

	if cfg.PollingInterval != 90*time.Second {
		t.Errorf("Expected PollingInterval to be 90s (from env), got %v", cfg.PollingInterval)
	}

	// Config file values should be used when no env var is set
	if cfg.LogFormat != "custom" {
		t.Errorf("Expected LogFormat to be 'custom' (from config), got %q", cfg.LogFormat)
	}

	if cfg.GracefulTimeout != 120*time.Second {
		t.Errorf("Expected GracefulTimeout to be 120s (from config), got %v", cfg.GracefulTimeout)
	}
}

func TestConfig_GetAPIURLs(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected []string
	}{
		{
			name: "empty URLs",
			config: &Config{
				APIURLs: []string{},
			},
			expected: []string{},
		},
		{
			name: "single URL",
			config: &Config{
				APIURLs: []string{"https://api.example.com/api/metrics/v1/tenant/probes"},
			},
			expected: []string{"https://api.example.com/api/metrics/v1/tenant/probes"},
		},
		{
			name: "multiple URLs",
			config: &Config{
				APIURLs: []string{
					"https://api1.example.com/api/metrics/v1/tenant1/probes",
					"https://api2.example.com/api/metrics/v1/tenant2/probes",
					"https://api3.example.com/api/metrics/v1/tenant3/probes",
				},
			},
			expected: []string{
				"https://api1.example.com/api/metrics/v1/tenant1/probes",
				"https://api2.example.com/api/metrics/v1/tenant2/probes",
				"https://api3.example.com/api/metrics/v1/tenant3/probes",
			},
		},
		{
			name: "nil URLs slice",
			config: &Config{
				APIURLs: nil,
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetAPIURLs()
			if len(result) != len(tt.expected) {
				t.Errorf("GetAPIURLs() returned %d URLs, expected %d", len(result), len(tt.expected))
				return
			}
			for i, url := range result {
				if url != tt.expected[i] {
					t.Errorf("GetAPIURLs()[%d] = %q, expected %q", i, url, tt.expected[i])
				}
			}
		})
	}
}

func TestLoadConfig_APIURLsFromEnvironment(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	tests := []struct {
		name     string
		envValue string
		expected []string
	}{
		{
			name:     "single URL",
			envValue: "https://api.example.com/api/metrics/v1/tenant/probes",
			expected: []string{"https://api.example.com/api/metrics/v1/tenant/probes"},
		},
		{
			name:     "multiple URLs",
			envValue: "https://api1.example.com/api/metrics/v1/tenant1/probes,https://api2.example.com/api/metrics/v1/tenant2/probes,https://api3.example.com/api/metrics/v1/tenant3/probes",
			expected: []string{"https://api1.example.com/api/metrics/v1/tenant1/probes", "https://api2.example.com/api/metrics/v1/tenant2/probes", "https://api3.example.com/api/metrics/v1/tenant3/probes"},
		},
		{
			name:     "multiple URLs with spaces",
			envValue: "https://api1.example.com/api/metrics/v1/tenant1/probes, https://api2.example.com/api/metrics/v1/tenant2/probes , https://api3.example.com/api/metrics/v1/tenant3/probes",
			expected: []string{"https://api1.example.com/api/metrics/v1/tenant1/probes", "https://api2.example.com/api/metrics/v1/tenant2/probes", "https://api3.example.com/api/metrics/v1/tenant3/probes"},
		},
		{
			name:     "single URL with spaces",
			envValue: " https://api.example.com/api/metrics/v1/tenant/probes ",
			expected: []string{"https://api.example.com/api/metrics/v1/tenant/probes"},
		},
		{
			name:     "empty string",
			envValue: "",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper for each test
			viper.Reset()

			if tt.envValue != "" {
				_ = os.Setenv("API_URLS", tt.envValue)
			}

			defer func() {
				_ = os.Unsetenv("API_URLS")
			}()

			cfg, err := LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig() failed: %v", err)
			}

			urls := cfg.GetAPIURLs()
			if len(urls) != len(tt.expected) {
				t.Errorf("Expected %d URLs, got %d", len(tt.expected), len(urls))
				return
			}

			for i, url := range urls {
				if url != tt.expected[i] {
					t.Errorf("Expected URL[%d] = %q, got %q", i, tt.expected[i], url)
				}
			}
		})
	}
}

func TestConfig_String_WithAPIURLs(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name: "with single API URL",
			config: &Config{
				LogLevel:        "info",
				LogFormat:       "json",
				PollingInterval: 30 * time.Second,
				GracefulTimeout: 30 * time.Second,
				APIURLs:         []string{"https://api.example.com/api/metrics/v1/tenant/probes"},
			},
			expected: "LogLevel=info, LogFormat=json, PollingInterval=30s, GracefulTimeout=30s, APIURLs=[https://api.example.com/api/metrics/v1/tenant/probes]",
		},
		{
			name: "with multiple API URLs",
			config: &Config{
				LogLevel:        "debug",
				LogFormat:       "text",
				PollingInterval: 60 * time.Second,
				GracefulTimeout: 45 * time.Second,
				APIURLs:         []string{"https://api1.example.com/api/metrics/v1/tenant1/probes", "https://api2.example.com/api/metrics/v1/tenant2/probes"},
			},
			expected: "LogLevel=debug, LogFormat=text, PollingInterval=1m0s, GracefulTimeout=45s, APIURLs=[https://api1.example.com/api/metrics/v1/tenant1/probes https://api2.example.com/api/metrics/v1/tenant2/probes]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.String()
			if result != tt.expected {
				t.Errorf("Config.String() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestPrometheusConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      PrometheusConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config with all fields",
			config: PrometheusConfig{
				RemoteWriteURL:    "http://thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
				CPURequests:       "100m",
				CPULimits:         "500m",
				MemoryRequests:    "256Mi",
				MemoryLimits:      "512Mi",
				ManagedByOperator: "observability-operator",
			},
			expectError: false,
		},
		{
			name: "valid config with empty optional fields",
			config: PrometheusConfig{
				RemoteWriteURL:    "http://thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
			},
			expectError: false,
		},
		{
			name: "invalid CPU requests format",
			config: PrometheusConfig{
				RemoteWriteURL:    "http://thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
				CPURequests:       "invalid-cpu",
			},
			expectError: true,
			errorMsg:    "invalid cpu_requests format",
		},
		{
			name: "invalid CPU limits format",
			config: PrometheusConfig{
				RemoteWriteURL:    "http://thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
				CPULimits:         "999xyz",
			},
			expectError: true,
			errorMsg:    "invalid cpu_limits format",
		},
		{
			name: "invalid memory requests format",
			config: PrometheusConfig{
				RemoteWriteURL:    "http://thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
				MemoryRequests:    "not-a-memory-value",
			},
			expectError: true,
			errorMsg:    "invalid memory_requests format",
		},
		{
			name: "invalid memory limits format",
			config: PrometheusConfig{
				RemoteWriteURL:    "http://thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
				MemoryLimits:      "1Zb",
			},
			expectError: true,
			errorMsg:    "invalid memory_limits format",
		},
		{
			name: "negative CPU requests",
			config: PrometheusConfig{
				RemoteWriteURL:    "http://thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
				CPURequests:       "-100m",
			},
			expectError: true,
			errorMsg:    "cpu_requests must be positive",
		},
		{
			name: "zero memory limits should be invalid",
			config: PrometheusConfig{
				RemoteWriteURL:    "http://thanos:19291/api/v1/receive",
				RemoteWriteTenant: "test-tenant",
				MemoryLimits:      "0Mi",
			},
			expectError: true, // Zero resources don't make sense
			errorMsg:    "memory_limits must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected validation error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected validation error: %v", err)
				}
			}
		})
	}
}

func TestLoadConfig_PrometheusConfig(t *testing.T) {
	// Reset viper to ensure clean state
	viper.Reset()

	// Set Prometheus-related Viper values directly (since LoadConfig doesn't set up env bindings)
	viper.Set("prometheus.remote_write_url", "http://test-thanos:19291/api/v1/receive")
	viper.Set("prometheus.remote_write_tenant", "test-tenant")
	viper.Set("prometheus.cpu_requests", "200m")
	viper.Set("prometheus.cpu_limits", "1000m")
	viper.Set("prometheus.memory_requests", "512Mi")
	viper.Set("prometheus.memory_limits", "1Gi")
	viper.Set("prometheus.managed_by_operator", "custom-operator")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	// Verify Prometheus configuration loaded correctly
	if cfg.Prometheus.RemoteWriteURL != "http://test-thanos:19291/api/v1/receive" {
		t.Errorf("Expected RemoteWriteURL to be 'http://test-thanos:19291/api/v1/receive', got %q", cfg.Prometheus.RemoteWriteURL)
	}

	if cfg.Prometheus.RemoteWriteTenant != "test-tenant" {
		t.Errorf("Expected RemoteWriteTenant to be 'test-tenant', got %q", cfg.Prometheus.RemoteWriteTenant)
	}

	if cfg.Prometheus.CPURequests != "200m" {
		t.Errorf("Expected CPURequests to be '200m', got %q", cfg.Prometheus.CPURequests)
	}

	if cfg.Prometheus.CPULimits != "1000m" {
		t.Errorf("Expected CPULimits to be '1000m', got %q", cfg.Prometheus.CPULimits)
	}

	if cfg.Prometheus.MemoryRequests != "512Mi" {
		t.Errorf("Expected MemoryRequests to be '512Mi', got %q", cfg.Prometheus.MemoryRequests)
	}

	if cfg.Prometheus.MemoryLimits != "1Gi" {
		t.Errorf("Expected MemoryLimits to be '1Gi', got %q", cfg.Prometheus.MemoryLimits)
	}

	if cfg.Prometheus.ManagedByOperator != "custom-operator" {
		t.Errorf("Expected ManagedByOperator to be 'custom-operator', got %q", cfg.Prometheus.ManagedByOperator)
	}
}
