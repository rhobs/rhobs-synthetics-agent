package main

import (
	"fmt"
	"log"
	"os"

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
		Run: func(cmd *cobra.Command, args []string) {
			log.Printf("Starting synthetic agent")
		},
	}

	// General Config flags
	startCmd.Flags().String("config", "", "Path to Viper config")
	startCmd.Flags().String("log-level", "info", "Log verbosity: debug, info")

	// Bind flags to viper
	viper.BindPFlag("log_level", startCmd.Flags().Lookup("log-level"))

	// Add commands to the root command
	rootCmd.AddCommand(startCmd)

	// Execute the root command. This parses the arguments and calls the appropriate command's Run function.
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
