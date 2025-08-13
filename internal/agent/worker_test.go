package agent

import (
	"context"
	"sync"
	"testing"
	"time"
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
		APIBaseURL:      "",
	}

	worker := NewWorker(cfg)
	ctx := context.Background()

	// Test that fetchProbeList fails gracefully without API client
	_, err := worker.fetchProbeList(ctx)
	if err == nil {
		t.Error("Expected fetchProbeList to fail when API client is not configured")
	}
}

func TestWorker_fetchProbeList_WithAPI(t *testing.T) {
	cfg := &Config{
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
		APIBaseURL:      "http://example.com",
		APITenant:       "test",
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
