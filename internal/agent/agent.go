package agent

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/oklog/run"
	"github.com/rhobs/rhobs-synthetics-agent/internal/logger"
)

// ResourceManager handles cleanup of resources like connections and file handles
type ResourceManager struct {
	httpClient  *http.Client
	openFiles   []io.Closer
	connections []io.Closer
	mu          sync.Mutex
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

	logger.Infof("Cleaning up resources...")

	// Close HTTP client transport
	if transport, ok := rm.httpClient.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}

	// Close all file handles
	for i, file := range rm.openFiles {
		if file != nil {
			if err := file.Close(); err != nil {
				logger.Errorf("Error closing file handle %d: %v", i, err)
			}
		}
	}

	// Close all network connections
	for i, conn := range rm.connections {
		if conn != nil {
			if err := conn.Close(); err != nil {
				logger.Errorf("Error closing connection %d: %v", i, err)
			}
		}
	}

	logger.Infof("Resource cleanup completed")
}

type Agent struct {
	config          *Config
	worker          *Worker
	resourceManager *ResourceManager
	taskWG          sync.WaitGroup
	shutdownChan    chan struct{}
	shutdownOnce    sync.Once
}

func New(cfg *Config) *Agent {
	worker := NewWorker(cfg)
	resourceManager := NewResourceManager()

	return &Agent{
		config:          cfg,
		worker:          worker,
		resourceManager: resourceManager,
		shutdownChan:    make(chan struct{}),
	}
}

func (a *Agent) Run() error {
	// Ensure cleanup happens on exit
	defer a.resourceManager.Cleanup()

	var g run.Group

	// Signal handling
	{
		sig := make(chan os.Signal, 1)
		g.Add(func() error {
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

			select {
			case receivedSig := <-sig:
				logger.Infof("Received signal %v, initiating graceful shutdown...", receivedSig)
				a.shutdownOnce.Do(func() {
					close(a.shutdownChan)
				})
			case <-a.shutdownChan:
				// Shutdown already initiated programmatically
			}

			// Wait for active tasks to complete with timeout
			logger.Infof("Waiting for active tasks to complete (timeout: %v)...", a.config.GracefulTimeout)
			done := make(chan struct{})
			go func() {
				a.taskWG.Wait()
				close(done)
			}()

			select {
			case <-done:
				logger.Info("All active tasks completed gracefully")
			case <-time.After(a.config.GracefulTimeout):
				logger.Info("Graceful shutdown timeout exceeded, forcing shutdown")
			}

			return nil
		}, func(error) {
			signal.Stop(sig)
			close(sig)
		})
	}

	// Main worker goroutine
	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return a.worker.Start(ctx, a.resourceManager, &a.taskWG, a.shutdownChan)
		}, func(error) {
			logger.Info("shutting down worker")
			cancel()
		})
	}

	// Health check server
	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return a.startHealthServer(ctx)
		}, func(error) {
			cancel()
		})
	}

	logger.Infof("RHOBS Synthetic Agent started with config: %s", a.config)

	if err := g.Run(); err != nil {
		logger.Info("RHOBS Synthetic Agent stopped")
		return err
	}

	logger.Info("RHOBS Synthetic Agent shutdown complete")
	return nil
}

// Shutdown gracefully shuts down the agent (useful for testing)
func (a *Agent) Shutdown() {
	a.shutdownOnce.Do(func() {
		logger.Infof("Programmatic shutdown initiated")
		close(a.shutdownChan)
	})
}

func (a *Agent) startHealthServer(ctx context.Context) error {
	// TODO: Implement health check server
	<-ctx.Done()
	return nil
}
