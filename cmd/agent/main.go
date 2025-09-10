package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/agent"
	"github.com/rhobs/rhobs-synthetics-agent/internal/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {

	log.SetOutput(os.Stdout)

	// rootCmd represents the base command when called without any subcommands
	var rootCmd = &cobra.Command{
		Use:   "rhobs-synthetics-agent",
		Short: "RHOBS Synthetics Monitoring Agent.",
		Long:  `This application provides the synthetic monitoring agent to be used within the RHOBS ecosystem.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			configPath := viper.GetString("config")
			if configPath != "" {
				viper.SetConfigFile(configPath)
				if err := viper.ReadInConfig(); err != nil {
					return fmt.Errorf("failed to read config: %w", err)
				}
			}
			return nil
		},
	}

	// startCmd represents the 'start' subcommand
	var startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the agent process",
		Long:  "Starts the synthetic agent. This will run in a loop, polling the API for new probes and will then process them as needed.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := agent.LoadConfig()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Reinitialize logger with viper configuration to catch the log level flag
			logger.ReinitLogger()

			agent, err := agent.New(cfg)
			if err != nil {
				return fmt.Errorf("failed to initialize agent: %w", err)
			}

			return agent.Run()
		},
	}

	// General Config flags
	startCmd.Flags().String("config", "", "Path to Viper config")
	startCmd.Flags().String("log-level", "info", "Log verbosity: debug, info")
	startCmd.Flags().Duration("interval", time.Second*30, "Polling interval")
	startCmd.Flags().String("graceful-timeout", "30s", "Graceful shutdown timeout")
	startCmd.Flags().String("kubeconfig", "", "Path to kubeconfig file (optional, for out-of-cluster development)")
	startCmd.Flags().String("namespace", "default", "The Kubernetes namespace for probe resources")

	// API Config flags
	startCmd.Flags().StringSlice("api-urls", []string{}, "Comma-separated list of complete API URLs (e.g., https://api.example.com/api/metrics/v1/tenant/probes)")

	// Prometheus Config flags
	startCmd.Flags().String("prometheus-remote-write-url", "http://thanos-receive-router-rhobs.rhobs-int.svc.cluster.local:19291/api/v1/receive", "Thanos remote write endpoint URL for Prometheus configuration")
	startCmd.Flags().String("prometheus-remote-write-tenant", "hcp", "Thanos tenant identifier for remote write requests")
	startCmd.Flags().String("prometheus-cpu-requests", "100m", "CPU requests for Prometheus pod")
	startCmd.Flags().String("prometheus-cpu-limits", "500m", "CPU limits for Prometheus pod")
	startCmd.Flags().String("prometheus-memory-requests", "256Mi", "Memory requests for Prometheus pod")
	startCmd.Flags().String("prometheus-memory-limits", "512Mi", "Memory limits for Prometheus pod")

	// Bind flags to viper
	_ = viper.BindPFlag("config", startCmd.Flags().Lookup("config"))
	_ = viper.BindPFlag("log_level", startCmd.Flags().Lookup("log-level"))
	_ = viper.BindPFlag("polling_interval", startCmd.Flags().Lookup("interval"))
	_ = viper.BindPFlag("graceful_timeout", startCmd.Flags().Lookup("graceful-timeout"))
	_ = viper.BindPFlag("kube_config", startCmd.Flags().Lookup("kubeconfig"))
	_ = viper.BindPFlag("namespace", startCmd.Flags().Lookup("namespace"))
	_ = viper.BindPFlag("api_urls", startCmd.Flags().Lookup("api-urls"))
	_ = viper.BindPFlag("prometheus.remote_write_url", startCmd.Flags().Lookup("prometheus-remote-write-url"))
	_ = viper.BindPFlag("prometheus.remote_write_tenant", startCmd.Flags().Lookup("prometheus-remote-write-tenant"))
	_ = viper.BindPFlag("prometheus.cpu_requests", startCmd.Flags().Lookup("prometheus-cpu-requests"))
	_ = viper.BindPFlag("prometheus.cpu_limits", startCmd.Flags().Lookup("prometheus-cpu-limits"))
	_ = viper.BindPFlag("prometheus.memory_requests", startCmd.Flags().Lookup("prometheus-memory-requests"))
	_ = viper.BindPFlag("prometheus.memory_limits", startCmd.Flags().Lookup("prometheus-memory-limits"))

	// Add commands to the root command
	rootCmd.AddCommand(startCmd)

	// Execute the root command. This parses the arguments and calls the appropriate command's Run function.
	if err := rootCmd.Execute(); err != nil {
		logger.Fatalf("Error: %v", err)
	}
}
