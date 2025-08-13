package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// ResourceManager handles cleanup of resources like connections and file handles
type ResourceManager struct {
	httpClient   *http.Client
	openFiles    []io.Closer
	connections  []io.Closer
	mu           sync.Mutex
}

// NewResourceManager creates a new resource manager
func NewResourceManager() *ResourceManager {
	return &ResourceManager{
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		openFiles:   make([]io.Closer, 0),
		connections: make([]io.Closer, 0),
	}
}

// AddResource adds a resource to be tracked and cleaned up
func (rm *ResourceManager) AddResource(resource io.Closer, resourceType string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	switch resourceType {
	case "file":
		rm.openFiles = append(rm.openFiles, resource)
	case "connection":
		rm.connections = append(rm.connections, resource)
	}
}

// Cleanup releases all held resources
func (rm *ResourceManager) Cleanup() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	log.Printf("Cleaning up resources...")
	
	// Close HTTP client transport
	if transport, ok := rm.httpClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	
	// Close all file handles
	for i, file := range rm.openFiles {
		if file != nil {
			if err := file.Close(); err != nil {
				log.Printf("Error closing file handle %d: %v", i, err)
			}
		}
	}
	
	// Close all network connections
	for i, conn := range rm.connections {
		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Printf("Error closing connection %d: %v", i, err)
			}
		}
	}
	
	log.Printf("Resource cleanup completed")
}

// runAgent contains the main agent logic
func runAgent(ctx context.Context, gracefulTimeout time.Duration) {
	log.Printf("Agent started, waiting for work... (graceful timeout: %v)", gracefulTimeout)
	
	// Initialize resource manager
	resourceMgr := NewResourceManager()
	defer resourceMgr.Cleanup()
	
	// Channel to signal shutdown without canceling in-progress tasks
	shutdownChan := make(chan struct{})
	
	// Listen for context cancellation to trigger shutdown
	go func() {
		<-ctx.Done()
		close(shutdownChan)
	}()
	
	// Main agent loop - polls for new probe lists from API
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	// WaitGroup to track active tasks
	var taskWG sync.WaitGroup
	
	for {
		select {
		case <-shutdownChan:
			log.Printf("Agent received shutdown signal, ceasing initiation of new primary tasks...")
			log.Printf("Waiting for active tasks to complete (timeout: %v)...", gracefulTimeout)
			
			// Wait for all active tasks to complete with timeout
			done := make(chan struct{})
			go func() {
				taskWG.Wait()
				close(done)
			}()
			
			select {
			case <-done:
				log.Printf("All active tasks completed gracefully")
			case <-time.After(gracefulTimeout):
				log.Printf("Graceful shutdown timeout exceeded, forcing shutdown")
			}
			
			log.Printf("Agent stopping...")
			return
			
		case <-ticker.C:
			// Check if shutdown is in progress before starting new tasks
			select {
			case <-shutdownChan:
				log.Printf("Shutdown in progress, skipping new probe list fetch")
				// Wait for active tasks with timeout and return
				done := make(chan struct{})
				go func() {
					taskWG.Wait()
					close(done)
				}()
				
				select {
				case <-done:
					log.Printf("All active tasks completed gracefully")
				case <-time.After(gracefulTimeout):
					log.Printf("Graceful shutdown timeout exceeded, forcing shutdown")
				}
				
				log.Printf("Agent stopping...")
				return
			default:
			}
			
			// Start new primary task: Fetch new probe lists from API
			log.Printf("Fetching new probe list from API...")
			taskWG.Add(1)
			go func() {
				defer taskWG.Done()
				fetchProbeList(resourceMgr)
			}()
		}
	}
}

// fetchProbeList simulates fetching probe lists from API
func fetchProbeList(rm *ResourceManager) {
	log.Printf("Starting probe list fetch...")
	
	// Simulate creating network connection for API call
	// In real implementation, this would be an actual HTTP connection
	// For demo purposes, we'll simulate it
	log.Printf("Establishing connection to API...")
	
	// Simulate API call - allow to complete naturally
	time.Sleep(1 * time.Second)
	log.Printf("Successfully fetched probe list from API")
	
	// Process the fetched probes
	processProbes(rm)
}

// processProbes simulates processing the fetched probes
func processProbes(rm *ResourceManager) {
	log.Printf("Starting probe processing...")
	
	// Simulate opening temporary files for probe data
	// In real implementation, these would be actual file handles
	log.Printf("Creating temporary files for probe data...")
	
	// Simulate probe processing - allow to complete naturally
	time.Sleep(500 * time.Millisecond)
	log.Printf("Processed probes successfully")
}

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
			
			// Get graceful shutdown timeout from flag
			gracefulTimeoutStr := viper.GetString("graceful_timeout")
			gracefulTimeout, err := time.ParseDuration(gracefulTimeoutStr)
			if err != nil {
				log.Printf("Invalid graceful timeout format '%s', using default 30s: %v", gracefulTimeoutStr, err)
				gracefulTimeout = 30 * time.Second
			}
			
			// Create context for graceful shutdown
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			
			// Setup signal handler for graceful shutdown
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
			
			// WaitGroup to track running goroutines
			var wg sync.WaitGroup
			
			// Start the main agent loop in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				runAgent(ctx, gracefulTimeout)
			}()
			
			// Wait for shutdown signal
			sig := <-sigChan
			log.Printf("Received signal %v, initiating graceful shutdown...", sig)
			
			// Cancel context to signal shutdown
			cancel()
			
			// Wait for goroutines to finish with timeout
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()
			
			select {
			case <-done:
				log.Printf("Graceful shutdown completed successfully")
				// Explicitly exit with status code 0
				os.Exit(0)
			case <-time.After(gracefulTimeout + 5*time.Second): // Add buffer for cleanup
				log.Printf("Overall shutdown timeout exceeded, forcing exit")
				// Force exit with non-zero status to indicate forced shutdown
				os.Exit(1)
			}
		},
	}

	// General Config flags
	startCmd.Flags().String("config", "", "Path to Viper config")
	startCmd.Flags().String("log-level", "info", "Log verbosity: debug, info")
	startCmd.Flags().String("graceful-timeout", "30s", "Maximum duration to wait for active tasks to complete during graceful shutdown (e.g., 30s, 1m, 90s)")

	// Bind flags to viper
	viper.BindPFlag("log_level", startCmd.Flags().Lookup("log-level"))
	viper.BindPFlag("graceful_timeout", startCmd.Flags().Lookup("graceful-timeout"))

	// Add commands to the root command
	rootCmd.AddCommand(startCmd)

	// Execute the root command. This parses the arguments and calls the appropriate command's Run function.
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
