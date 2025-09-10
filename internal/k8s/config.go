package k8s

import "github.com/spf13/viper"

type BlackboxConfig struct {
	Probing    BlackboxProbingConfig    `mapstructure:"probing"`
	Deployment BlackboxDeploymentConfig `mapstructure:"deployment"`
}

type BlackboxProbingConfig struct {
	Interval  string `mapstructure:"interval"`
	Module    string `mapstructure:"module"`
	ProberURL string `mapstructure:"prober_url"`
}

type BlackboxDeploymentConfig struct {
	Image  string            `mapstructure:"image"`
	Cmd    []string          `mapstructure:"command"`
	Args   []string          `mapstructure:"args"`
	Labels map[string]string `mapstructure:"custom_labels"`
}

func LoadBlackboxDefaults() {
	viper.SetDefault("blackbox.probing.interval", "30s")
	viper.SetDefault("blackbox.probing.module", "http_2xx")
	viper.SetDefault("blackbox.probing.prober_url", "synthetics-blackbox-prober-default-service:9115")

	viper.SetDefault("blackbox.deployment.image", DefaultBlackBoxExporterImage)
	viper.SetDefault("blackbox.deployment.command", []string{})
	viper.SetDefault("blackbox.deployment.arguments", []string{})
}
