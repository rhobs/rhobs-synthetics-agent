package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/oklog/run"
	"github.com/rhobs/rhobs-synthetics-agent/internal/logger"
	"github.com/rhobs/rhobs-synthetics-agent/internal/metrics"
	"github.com/rhobs/rhobs-synthetics-agent/internal/version"
)

type Agent struct {
	config          *Config
	worker          *Worker
	taskWG          sync.WaitGroup
	shutdownChan    chan struct{}
	shutdownOnce    sync.Once
	ready           bool
	readyMu         sync.RWMutex
}

func New(cfg *Config) *Agent {
	worker := NewWorker(cfg)

	// Initialize agent info metrics
	namespace := "default"
	if cfg != nil && cfg.Namespace != "" {
		namespace = cfg.Namespace
	}
	metrics.SetAgentInfo(version.Version, namespace)

	agent := &Agent{
		config:          cfg,
		worker:          worker,
		shutdownChan:    make(chan struct{}),
		ready:           false,
	}

	// Set readiness callback for the worker
	worker.SetReadinessCallback(agent.setReady)

	return agent
}

func (a *Agent) Run() error {
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
			return a.worker.Start(ctx, &a.taskWG, a.shutdownChan)
		}, func(error) {
			logger.Info("shutting down worker")
			a.setReady(false)
			cancel()
		})
	}

	// Metrics server
	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			return a.startMetricsServer(ctx)
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

func (a *Agent) startMetricsServer(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/livez", a.handleLiveness)
	mux.HandleFunc("/readyz", a.handleReadiness)
	
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Errorf("Metrics server shutdown error: %v", err)
		}
	}()

	logger.Info("Starting metrics server on :8080 with /metrics, /livez, and /readyz endpoints")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("metrics server failed: %w", err)
	}
	
	return nil
}

// setReady sets the agent readiness state
func (a *Agent) setReady(ready bool) {
	a.readyMu.Lock()
	defer a.readyMu.Unlock()
	a.ready = ready
}

// isReady returns the current readiness state
func (a *Agent) isReady() bool {
	a.readyMu.RLock()
	defer a.readyMu.RUnlock()
	return a.ready
}

// handleLiveness implements the liveness endpoint
// Returns 200 OK as long as the process is running and responsive
func (a *Agent) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("OK")); err != nil {
		logger.Errorf("Failed to write liveness response: %v", err)
	}
}

// handleReadiness implements the readiness endpoint
// Returns 200 OK only when the agent is initialized and ready to perform its duties
func (a *Agent) handleReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	
	if a.isReady() {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("Ready")); err != nil {
			logger.Errorf("Failed to write readiness response: %v", err)
		}
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("Not Ready")); err != nil {
			logger.Errorf("Failed to write readiness response: %v", err)
		}
	}
}
