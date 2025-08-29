package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
)

func TestNewWorker(t *testing.T) {
	cfg := &Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
	}

	worker := NewWorker(cfg)

	if worker == nil {
		t.Fatal("NewWorker() returned nil")
	}

	if worker.config != cfg {
		t.Error("NewWorker() did not set config correctly")
	}
}

func TestNewWorker_NilConfig(t *testing.T) {
	worker := NewWorker(nil)

	if worker == nil {
		t.Fatal("NewWorker() returned nil with nil config")
	}

	if worker.config != nil {
		t.Error("NewWorker() should have nil config when passed nil")
	}
}

func TestWorker_Start_ContextCancellation(t *testing.T) {
	cfg := &Config{
		PollingInterval: 100 * time.Millisecond, // Short interval for testing
		GracefulTimeout: 1 * time.Second,
	}

	worker := NewWorker(cfg)
	resourceMgr := NewResourceManager()
	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	// Create a context that will be cancelled quickly
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start the worker
	err := worker.Start(ctx, resourceMgr, &taskWG, shutdownChan)

	// Should return context.DeadlineExceeded or context.Canceled
	if err == nil {
		t.Error("Expected Start() to return an error due to context cancellation")
	}
}

func TestWorker_Start_ShutdownSignal(t *testing.T) {
	cfg := &Config{
		PollingInterval: 100 * time.Millisecond, // Short interval for testing
		GracefulTimeout: 1 * time.Second,
	}

	worker := NewWorker(cfg)
	resourceMgr := NewResourceManager()
	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	ctx := context.Background()

	// Start the worker in a goroutine
	done := make(chan error, 1)
	go func() {
		done <- worker.Start(ctx, resourceMgr, &taskWG, shutdownChan)
	}()

	// Send shutdown signal after a short delay
	time.Sleep(50 * time.Millisecond)
	close(shutdownChan)

	// Wait for worker to stop
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Expected Start() to return nil on shutdown signal, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Worker did not stop within timeout after shutdown signal")
	}
}

func TestWorker_Start_PollingInterval(t *testing.T) {
	cfg := &Config{
		PollingInterval: 50 * time.Millisecond, // Very short for testing
		GracefulTimeout: 1 * time.Second,
	}

	worker := NewWorker(cfg)
	resourceMgr := NewResourceManager()
	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start worker
	startTime := time.Now()
	err := worker.Start(ctx, resourceMgr, &taskWG, shutdownChan)

	// Should have run for approximately the context timeout duration
	elapsed := time.Since(startTime)
	if elapsed < 150*time.Millisecond {
		t.Errorf("Worker stopped too early, elapsed: %v", elapsed)
	}

	if err == nil {
		t.Error("Expected Start() to return context timeout error")
	}
}

func TestWorker_processProbes(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
	}

	worker := NewWorker(cfg)
	resourceMgr := NewResourceManager()
	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	ctx := context.Background()

	// Test normal processing
	err := worker.processProbes(ctx, resourceMgr, &taskWG, shutdownChan)
	if err != nil {
		t.Errorf("processProbes() returned unexpected error: %v", err)
	}

	// Wait for any tasks to complete
	taskWG.Wait()
}

func TestWorker_processProbes_ShutdownSignal(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
	}

	worker := NewWorker(cfg)
	resourceMgr := NewResourceManager()
	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	// Close shutdown channel before calling processProbes
	close(shutdownChan)

	ctx := context.Background()

	err := worker.processProbes(ctx, resourceMgr, &taskWG, shutdownChan)
	if err != nil {
		t.Errorf("processProbes() returned unexpected error during shutdown: %v", err)
	}

	// Should not have added any tasks to the wait group since shutdown was signaled
	// Wait briefly to ensure no tasks were started
	done := make(chan struct{})
	go func() {
		taskWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Good, no tasks were running
	case <-time.After(100 * time.Millisecond):
		t.Error("Tasks may still be running after shutdown signal")
	}
}

func TestWorker_fetchProbeList(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIURLs:         []string{},
	}

	worker := NewWorker(cfg)
	ctx := context.Background()

	// Test that fetchProbeList returns empty slice without API client
	probes, err := worker.fetchProbeList(ctx)
	if err != nil {
		t.Errorf("fetchProbeList should not fail when no API client is configured, got error: %v", err)
	}
	if len(probes) != 0 {
		t.Errorf("fetchProbeList should return empty slice when no API clients configured, got %d probes", len(probes))
	}
}

func TestWorker_fetchProbeList_WithAPI(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIURLs:         []string{"http://example.com/api/metrics/v1/test/probes"},
		LabelSelector:   "test=true",
	}

	worker := NewWorker(cfg)
	ctx := context.Background()

	// This will fail because the API endpoint doesn't exist
	// but it tests that the method signature is correct
	_, err := worker.fetchProbeList(ctx)
	if err == nil {
		t.Error("Expected fetchProbeList to fail when API endpoint doesn't exist")
	}
}

func TestWorker_Start_InitialRun(t *testing.T) {
	cfg := &Config{
		PollingInterval: 1 * time.Second, // Long interval so only initial run happens
		GracefulTimeout: 1 * time.Second,
	}

	worker := NewWorker(cfg)
	resourceMgr := NewResourceManager()
	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	// Use a context with a short timeout to stop after initial run
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	startTime := time.Now()
	err := worker.Start(ctx, resourceMgr, &taskWG, shutdownChan)
	elapsed := time.Since(startTime)

	// Should have completed the initial run (which takes ~1.5s total)
	// but been cancelled by context timeout
	if elapsed < 150*time.Millisecond {
		t.Errorf("Start() stopped too early for initial run, elapsed: %v", elapsed)
	}

	if err == nil {
		t.Error("Expected Start() to return context timeout error")
	}

	// Wait for any remaining tasks
	taskWG.Wait()
}

func TestWorker_ConcurrentShutdown(t *testing.T) {
	cfg := &Config{
		PollingInterval: 50 * time.Millisecond,
		GracefulTimeout: 1 * time.Second,
	}

	worker := NewWorker(cfg)
	resourceMgr := NewResourceManager()
	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	ctx := context.Background()

	// Start the worker
	done := make(chan error, 1)
	go func() {
		done <- worker.Start(ctx, resourceMgr, &taskWG, shutdownChan)
	}()

	// Let it run for a bit to start some tasks
	time.Sleep(100 * time.Millisecond)

	// Signal shutdown
	close(shutdownChan)

	// Worker should stop reasonably quickly
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Expected Start() to return nil on shutdown, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Worker did not stop within timeout after shutdown signal")
	}

	// Wait for all tasks to complete
	taskDone := make(chan struct{})
	go func() {
		taskWG.Wait()
		close(taskDone)
	}()

	select {
	case <-taskDone:
		// Good, all tasks completed
	case <-time.After(3 * time.Second):
		t.Error("Tasks did not complete within timeout after shutdown")
	}
}

func TestWorker_processProbe_K8sIntegration(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		Namespace:       "test-namespace",
		Blackbox: BlackboxConfig{
			Interval:  "30s",
			Module:    "http_2xx",
			ProberURL: "http://blackbox-exporter:9115",
		},
	}

	worker := NewWorker(cfg)
	ctx := context.Background()

	probe := api.Probe{
		ID:        "test-probe-worker",
		StaticURL: "http://example.com",
		Labels: map[string]string{
			"cluster_id": "test-cluster",
		},
		Status: "pending",
	}

	// This should not fail even when not in K8s cluster
	// because it falls back to logging
	err := worker.processProbe(ctx, probe)
	if err != nil {
		t.Errorf("processProbe() should not fail when falling back to logging: %v", err)
	}
}

func TestWorker_processProbe_FallbackLogging(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		Namespace:       "test-namespace",
		Blackbox: BlackboxConfig{
			Interval:  "30s",
			Module:    "http_2xx",
			ProberURL: "http://blackbox-exporter:9115",
		},
	}

	worker := NewWorker(cfg)
	ctx := context.Background()

	// Test with an invalid URL to trigger URL validation failure
	probe := api.Probe{
		ID:        "test-probe-invalid",
		StaticURL: "https://non-existent-domain-12345.com",
		Labels: map[string]string{
			"cluster_id": "test-cluster",
		},
		Status: "pending",
	}

	// This should fail because URL validation fails
	err := worker.processProbe(ctx, probe)
	if err == nil {
		t.Error("processProbe() should fail when URL validation fails")
	}
}

func TestWorker_deduplicateProbes(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
	}
	worker := NewWorker(cfg)

	tests := []struct {
		name     string
		probes   []api.Probe
		expected []api.Probe
	}{
		{
			name:     "empty slice",
			probes:   []api.Probe{},
			expected: []api.Probe{},
		},
		{
			name: "no duplicates",
			probes: []api.Probe{
				{ID: "probe-1", StaticURL: "https://example1.com"},
				{ID: "probe-2", StaticURL: "https://example2.com"},
				{ID: "probe-3", StaticURL: "https://example3.com"},
			},
			expected: []api.Probe{
				{ID: "probe-1", StaticURL: "https://example1.com"},
				{ID: "probe-2", StaticURL: "https://example2.com"},
				{ID: "probe-3", StaticURL: "https://example3.com"},
			},
		},
		{
			name: "with duplicates by URL",
			probes: []api.Probe{
				{ID: "probe-1", StaticURL: "https://example1.com"},
				{ID: "probe-2", StaticURL: "https://example2.com"},
				{ID: "probe-3", StaticURL: "https://example1.com"}, // Same URL as probe-1
				{ID: "probe-4", StaticURL: "https://example3.com"},
				{ID: "probe-5", StaticURL: "https://example2.com"}, // Same URL as probe-2
			},
			expected: []api.Probe{
				{ID: "probe-1", StaticURL: "https://example1.com"},
				{ID: "probe-2", StaticURL: "https://example2.com"},
				{ID: "probe-4", StaticURL: "https://example3.com"},
			},
		},
		{
			name: "all duplicates by URL",
			probes: []api.Probe{
				{ID: "probe-1", StaticURL: "https://example1.com"},
				{ID: "probe-2", StaticURL: "https://example1.com"}, // Same URL
				{ID: "probe-3", StaticURL: "https://example1.com"}, // Same URL
			},
			expected: []api.Probe{
				{ID: "probe-1", StaticURL: "https://example1.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := worker.deduplicateProbes(tt.probes)
			
			if len(result) != len(tt.expected) {
				t.Errorf("deduplicateProbes() returned %d probes, expected %d", len(result), len(tt.expected))
				return
			}

			for i, probe := range result {
				if probe.ID != tt.expected[i].ID {
					t.Errorf("deduplicateProbes()[%d].ID = %q, expected %q", i, probe.ID, tt.expected[i].ID)
				}
				if probe.StaticURL != tt.expected[i].StaticURL {
					t.Errorf("deduplicateProbes()[%d].StaticURL = %q, expected %q", i, probe.StaticURL, tt.expected[i].StaticURL)
				}
			}
		})
	}
}

func TestWorker_MultipleAPIClients(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIURLs:         []string{"https://api1.example.com/api/metrics/v1/test/probes", "https://api2.example.com/api/metrics/v1/test/probes", "https://api3.example.com/api/metrics/v1/test/probes"},
	}

	worker := NewWorker(cfg)

	// Verify that multiple API clients were created
	if len(worker.apiClients) != 3 {
		t.Errorf("NewWorker() created %d API clients, expected 3", len(worker.apiClients))
	}

	// Test with empty API URLs
	cfgEmpty := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIURLs:         []string{},
	}

	workerEmpty := NewWorker(cfgEmpty)
	if len(workerEmpty.apiClients) != 0 {
		t.Errorf("NewWorker() with empty URLs created %d API clients, expected 0", len(workerEmpty.apiClients))
	}

	// Test with nil config
	workerNil := NewWorker(nil)
	if len(workerNil.apiClients) != 0 {
		t.Errorf("NewWorker() with nil config created %d API clients, expected 0", len(workerNil.apiClients))
	}
}

func TestWorker_fetchProbeList_NoAPIClients(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIURLs:         []string{}, // No API URLs
	}

	worker := NewWorker(cfg)
	ctx := context.Background()

	probes, err := worker.fetchProbeList(ctx)
	if err != nil {
		t.Errorf("fetchProbeList() should not fail when no API clients are configured, got error: %v", err)
	}

	if probes == nil {
		t.Error("fetchProbeList() should return empty slice, not nil")
	}

	if len(probes) != 0 {
		t.Errorf("fetchProbeList() should return empty slice when no API clients configured, got %d probes", len(probes))
	}
}

func TestWorker_updateProbeStatus_MultipleClients(t *testing.T) {
	// Create mock servers to simulate multiple API endpoints
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer successServer.Close()

	failureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failureServer.Close()

	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIURLs:         []string{successServer.URL + "/api/metrics/v1/test/probes", failureServer.URL + "/api/metrics/v1/test/probes", successServer.URL + "/api/metrics/v1/test/probes"},
	}

	worker := NewWorker(cfg)

	// Test updateProbeStatus with mixed success/failure
	// This function doesn't return an error, but logs the results
	// We're testing that it doesn't panic and handles the mixed responses
	worker.updateProbeStatus("test-probe", "active")

	// Test with no API clients
	workerNoClients := &Worker{
		config:     cfg,
		apiClients: []*api.Client{},
	}
	workerNoClients.updateProbeStatus("test-probe", "active")
}

func TestNewWorker_EdgeCases(t *testing.T) {
	// Test with config containing empty URL in the slice
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIURLs:         []string{"https://api1.example.com/api/metrics/v1/test/probes", "", "https://api3.example.com/api/metrics/v1/test/probes"},
	}

	worker := NewWorker(cfg)
	
	// Should only create clients for non-empty URLs
	if len(worker.apiClients) != 2 {
		t.Errorf("NewWorker() created %d API clients, expected 2 (empty URL should be skipped)", len(worker.apiClients))
	}

	// Test with default namespace when not specified
	cfgNoNamespace := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIURLs:         []string{"https://api.example.com/api/metrics/v1/tenant/probes"},
		Namespace:       "", // Empty namespace
	}

	workerDefault := NewWorker(cfgNoNamespace)
	if workerDefault.probeManager == nil {
		t.Error("NewWorker() should create probe manager even with empty namespace")
	}
}
