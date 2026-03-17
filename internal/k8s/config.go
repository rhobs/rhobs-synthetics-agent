package k8s

import "github.com/spf13/viper"

type BlackboxConfig struct {
	Probing    BlackboxProbingConfig    `mapstructure:"probing"`
	Deployment BlackboxDeploymentConfig `mapstructure:"deployment"`
}

type BlackboxProbingConfig struct {
	Interval              string `mapstructure:"interval"`
	PrivateInterval       string `mapstructure:"private_interval"`
	Module                string `mapstructure:"module"`
	ProberURL             string `mapstructure:"prober_url"`
	ScrapeTimeout         string `mapstructure:"scrape_timeout"`
	PrivateScrapeTimeout  string `mapstructure:"private_scrape_timeout"`
}

type BlackboxDeploymentConfig struct {
	Image  string            `mapstructure:"image"`
	Cmd    []string          `mapstructure:"command"`
	Args   []string          `mapstructure:"args"`
	Labels map[string]string `mapstructure:"custom_labels"`
}

const (
	// DefaultInterval is the default scrape interval for public probes.
	DefaultInterval = "30s"

	// DefaultPrivateInterval is the default scrape interval for private probes.
	DefaultPrivateInterval = "30s"

	// DefaultScrapeTimeout is the default scrape timeout for public probes.
	DefaultScrapeTimeout = "10s"

	// DefaultPrivateScrapeTimeout is the default scrape timeout for private probes.
	// Private probes go through TGW/PrivateLink which adds latency.
	DefaultPrivateScrapeTimeout = "15s"
)

func LoadBlackboxDefaults() {
	viper.SetDefault("blackbox.probing.interval", DefaultInterval)
	viper.SetDefault("blackbox.probing.private_interval", DefaultPrivateInterval)
	viper.SetDefault("blackbox.probing.module", "http_2xx")
	viper.SetDefault("blackbox.probing.prober_url", "synthetics-blackbox-prober-default-service:9115")
	viper.SetDefault("blackbox.probing.scrape_timeout", DefaultScrapeTimeout)
	viper.SetDefault("blackbox.probing.private_scrape_timeout", DefaultPrivateScrapeTimeout)

	viper.SetDefault("blackbox.deployment.image", DefaultBlackBoxExporterImage)
	viper.SetDefault("blackbox.deployment.command", []string{})
	viper.SetDefault("blackbox.deployment.arguments", []string{})
}
