package agent

import (
	"context"
	"fmt"
	"net"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type Agent struct {
	config          *Config
	worker          *Worker
	taskWG          sync.WaitGroup
	shutdownChan    chan struct{}
	shutdownOnce    sync.Once
	ready           bool
	readyMu         sync.RWMutex
	metricsAddr     string
	metricsReady    chan struct{}
}

func New(cfg *Config) (*Agent, error) {
	worker, err := NewWorker(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize worker: %w", err)
	}

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
		metricsReady:    make(chan struct{}),
		ready:           false,
	}

	// Set readiness callback for the worker
	worker.SetReadinessCallback(agent.setReady)

	return agent, nil
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

	// Main worker goroutine (with optional leader election)
	{
		ctx, cancel := context.WithCancel(context.Background())
		if a.config != nil && a.config.LeaderElect {
			g.Add(func() error {
				return a.runWithLeaderElection(ctx)
			}, func(error) {
				logger.Info("shutting down leader election")
				a.setReady(false)
				cancel()
			})
		} else {
			g.Add(func() error {
				return a.worker.Start(ctx, &a.taskWG, a.shutdownChan)
			}, func(error) {
				logger.Info("shutting down worker")
				a.setReady(false)
				cancel()
			})
		}
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

// MetricsAddr blocks until the metrics server has bound its listener,
// then returns the address it is listening on.
func (a *Agent) MetricsAddr() string {
	<-a.metricsReady
	return a.metricsAddr
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
	
	addr := ":8080"
	if a.config != nil && a.config.MetricsAddr != "" {
		addr = a.config.MetricsAddr
	}

	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("metrics server failed: %w", err)
	}
	a.metricsAddr = ln.Addr().String()
	close(a.metricsReady)

	server := &http.Server{
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

	logger.Infof("Starting metrics server on %s with /metrics, /livez, and /readyz endpoints", a.metricsAddr)
	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
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

const (
	leaseName      = "synthetics-agent"
	leaseDuration  = 15 * time.Second
	renewDeadline  = 10 * time.Second
	retryPeriod    = 2 * time.Second
)

func (a *Agent) runWithLeaderElection(ctx context.Context) error {
	var restCfg *rest.Config
	var err error
	if a.config != nil && a.config.KubeConfig != "" {
		restCfg, err = clientcmd.BuildConfigFromFlags("", a.config.KubeConfig)
	} else {
		restCfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return fmt.Errorf("failed to build kubernetes config for leader election: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client for leader election: %w", err)
	}

	id := os.Getenv("POD_NAME")
	if id == "" {
		id, err = os.Hostname()
		if err != nil {
			return fmt.Errorf("failed to determine pod identity: %w", err)
		}
	}

	namespace := "default"
	if envNS := os.Getenv("NAMESPACE"); envNS != "" {
		namespace = envNS
	} else if a.config != nil && a.config.Namespace != "" {
		namespace = a.config.Namespace
	}

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: namespace,
		},
		Client: clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	// Verify lease RBAC before entering the leader election loop.
	// If the service account cannot access leases (e.g. e2e / dev environments),
	// fall back to running without leader election rather than hanging.
	_, rbacErr := clientset.CoordinationV1().Leases(namespace).List(ctx, metav1.ListOptions{Limit: 1})
	if rbacErr != nil {
		logger.Warnf("Leader election RBAC check failed (%v); falling back to single-replica mode", rbacErr)
		return a.worker.Start(ctx, &a.taskWG, a.shutdownChan)
	}

	logger.Infof("Starting leader election (id=%s, namespace=%s)", id, namespace)

	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            lock,
		LeaseDuration:   leaseDuration,
		RenewDeadline:   renewDeadline,
		RetryPeriod:     retryPeriod,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				logger.Info("Acquired leader lease, starting reconciliation")
				metrics.SetLeader(true)
				if err := a.worker.Start(leaderCtx, &a.taskWG, a.shutdownChan); err != nil && leaderCtx.Err() == nil {
					logger.Errorf("Worker error: %v", err)
				}
			},
			OnStoppedLeading: func() {
				logger.Info("Lost leader lease, stopped reconciliation")
				metrics.SetLeader(false)
				a.setReady(false)
			},
			OnNewLeader: func(identity string) {
				if identity == id {
					logger.Info("This instance is the leader")
					return
				}
				logger.Infof("Current leader: %s", identity)
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create leader elector: %w", err)
	}

	le.Run(ctx)
	return nil
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
