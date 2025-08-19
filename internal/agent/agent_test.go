package agent

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Mock closer for testing resource cleanup
type mockCloser struct {
	closed bool
	err    error
}

func (m *mockCloser) Close() error {
	m.closed = true
	return m.err
}

func TestNewResourceManager(t *testing.T) {
	rm := NewResourceManager()

	if rm == nil {
		t.Fatal("NewResourceManager() returned nil")
	}

	if rm.httpClient == nil {
		t.Error("NewResourceManager() did not initialize httpClient")
	}

	if rm.httpClient.Timeout != 30*time.Second {
		t.Errorf("Expected httpClient timeout to be 30s, got %v", rm.httpClient.Timeout)
	}

	if rm.openFiles == nil {
		t.Error("NewResourceManager() did not initialize openFiles slice")
	}

	if rm.connections == nil {
		t.Error("NewResourceManager() did not initialize connections slice")
	}

	if len(rm.openFiles) != 0 {
		t.Error("Expected openFiles to be empty initially")
	}

	if len(rm.connections) != 0 {
		t.Error("Expected connections to be empty initially")
	}
}

func TestResourceManager_AddResource(t *testing.T) {
	rm := NewResourceManager()

	fileCloser := &mockCloser{}
	connCloser := &mockCloser{}

	// Test adding file resource
	rm.AddResource(fileCloser, "file")
	if len(rm.openFiles) != 1 {
		t.Errorf("Expected 1 file resource, got %d", len(rm.openFiles))
	}
	if len(rm.connections) != 0 {
		t.Errorf("Expected 0 connection resources, got %d", len(rm.connections))
	}

	// Test adding connection resource
	rm.AddResource(connCloser, "connection")
	if len(rm.openFiles) != 1 {
		t.Errorf("Expected 1 file resource, got %d", len(rm.openFiles))
	}
	if len(rm.connections) != 1 {
		t.Errorf("Expected 1 connection resource, got %d", len(rm.connections))
	}

	// Test adding unknown resource type (should not be added)
	unknownCloser := &mockCloser{}
	rm.AddResource(unknownCloser, "unknown")
	if len(rm.openFiles) != 1 {
		t.Errorf("Expected 1 file resource after unknown type, got %d", len(rm.openFiles))
	}
	if len(rm.connections) != 1 {
		t.Errorf("Expected 1 connection resource after unknown type, got %d", len(rm.connections))
	}
}

func TestResourceManager_Cleanup(t *testing.T) {
	rm := NewResourceManager()

	// Add some mock resources
	fileCloser1 := &mockCloser{}
	fileCloser2 := &mockCloser{}
	connCloser1 := &mockCloser{}
	connCloser2 := &mockCloser{}

	rm.AddResource(fileCloser1, "file")
	rm.AddResource(fileCloser2, "file")
	rm.AddResource(connCloser1, "connection")
	rm.AddResource(connCloser2, "connection")

	// Call cleanup
	rm.Cleanup()

	// Verify all resources were closed
	if !fileCloser1.closed {
		t.Error("fileCloser1 was not closed")
	}
	if !fileCloser2.closed {
		t.Error("fileCloser2 was not closed")
	}
	if !connCloser1.closed {
		t.Error("connCloser1 was not closed")
	}
	if !connCloser2.closed {
		t.Error("connCloser2 was not closed")
	}
}

func TestResourceManager_Cleanup_WithErrors(t *testing.T) {
	rm := NewResourceManager()

	// Add resources that will fail to close
	failingCloser := &mockCloser{err: io.ErrClosedPipe}
	normalCloser := &mockCloser{}

	rm.AddResource(failingCloser, "file")
	rm.AddResource(normalCloser, "connection")

	// Cleanup should not panic even if some resources fail to close
	rm.Cleanup()

	// Both should have been attempted to close
	if !failingCloser.closed {
		t.Error("failingCloser was not attempted to be closed")
	}
	if !normalCloser.closed {
		t.Error("normalCloser was not closed")
	}
}

func TestResourceManager_Cleanup_NilResources(t *testing.T) {
	rm := NewResourceManager()

	// Add nil resources (edge case)
	rm.openFiles = append(rm.openFiles, nil)
	rm.connections = append(rm.connections, nil)

	// Should not panic
	rm.Cleanup()
}

func TestResourceManager_ConcurrentAccess(t *testing.T) {
	rm := NewResourceManager()

	// Test concurrent access to AddResource and Cleanup
	var wg sync.WaitGroup

	// Start multiple goroutines adding resources
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			closer := &mockCloser{}
			rm.AddResource(closer, "file")
		}()
	}

	// Start a cleanup goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond) // Let some resources be added first
		rm.Cleanup()
	}()

	wg.Wait()
	// Test should complete without deadlock or panic
}

func TestResourceManager_HTTPClientTransport(t *testing.T) {
	rm := NewResourceManager()

	// Set a custom transport
	transport := &http.Transport{}
	rm.httpClient.Transport = transport

	// Cleanup should handle the transport
	rm.Cleanup()
	// This mainly tests that the code doesn't panic
}

func TestNew(t *testing.T) {
	cfg := &Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
	}

	agent := New(cfg)

	if agent == nil {
		t.Fatal("New() returned nil")
	}

	if agent.config != cfg {
		t.Error("New() did not set config correctly")
	}

	if agent.worker == nil {
		t.Error("New() did not initialize worker")
	}

	if agent.resourceManager == nil {
		t.Error("New() did not initialize resourceManager")
	}

	if agent.shutdownChan == nil {
		t.Error("New() did not initialize shutdownChan")
	}

	// Test that shutdown channel is not closed initially
	select {
	case <-agent.shutdownChan:
		t.Error("shutdownChan should not be closed initially")
	default:
		// Good, channel is open
	}
}

func TestNew_NilConfig(t *testing.T) {
	agent := New(nil)

	if agent == nil {
		t.Fatal("New() returned nil with nil config")
	}

	if agent.config != nil {
		t.Error("New() should have nil config when passed nil")
	}

	// Other components should still be initialized
	if agent.worker == nil {
		t.Error("New() did not initialize worker even with nil config")
	}

	if agent.resourceManager == nil {
		t.Error("New() did not initialize resourceManager even with nil config")
	}
}

func TestAgent_Run_QuickShutdown(t *testing.T) {
	cfg := &Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 100 * time.Millisecond, // Short interval for faster test
		GracefulTimeout: 100 * time.Millisecond,
	}

	agent := New(cfg)

	// Test the shutdown channel mechanism directly
	done := make(chan error, 1)
	go func() {
		// Use a longer context but trigger shutdown via channel
		ctx := context.Background()
		done <- agent.worker.Start(ctx, agent.resourceManager, &agent.taskWG, agent.shutdownChan)
	}()

	// Let it run briefly to start some work
	time.Sleep(100 * time.Millisecond)

	// Signal shutdown via channel
	close(agent.shutdownChan)

	// Agent should stop within reasonable time
	select {
	case err := <-done:
		// Shutdown via channel should return nil
		if err != nil {
			t.Errorf("Expected nil error on shutdown, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Agent did not stop within timeout after shutdown signal")
	}

	// Wait for any remaining tasks
	agent.taskWG.Wait()
}

func TestAgent_startHealthServer(t *testing.T) {
	cfg := &Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
	}

	agent := New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// startMetricsServer should block until context is cancelled
	err := agent.startMetricsServer(ctx)

	// Should return nil when context is cancelled
	if err != nil {
		t.Errorf("Expected startMetricsServer to return nil, got: %v", err)
	}
}

func TestAgent_TaskWaitGroup(t *testing.T) {
	cfg := &Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 100 * time.Millisecond,
		GracefulTimeout: 200 * time.Millisecond,
	}

	agent := New(cfg)

	// Test that we can track a task with the wait group
	agent.taskWG.Add(1)
	go func() {
		time.Sleep(50 * time.Millisecond) // Simulate some work
		agent.taskWG.Done()
	}()

	// Wait for the task to complete
	done := make(chan struct{})
	go func() {
		agent.taskWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Good, task completed
	case <-time.After(1 * time.Second):
		t.Error("Task did not complete within timeout")
	}
}

func TestAgent_Config_String_Integration(t *testing.T) {
	cfg := &Config{
		LogLevel:        "debug",
		LogFormat:       "text",
		PollingInterval: 45 * time.Second,
		GracefulTimeout: 60 * time.Second,
	}

	agent := New(cfg)

	// Verify the config string formatting works with the agent
	expected := "LogLevel=debug, LogFormat=text, PollingInterval=45s, GracefulTimeout=1m0s, APIBaseURLs=[]"
	actual := agent.config.String()

	if actual != expected {
		t.Errorf("Expected config string %q, got %q", expected, actual)
	}
}

// Test helper function to check if a channel is closed
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestAgent_ShutdownChannel_State(t *testing.T) {
	cfg := &Config{
		PollingInterval: 1 * time.Second,
		GracefulTimeout: 1 * time.Second,
	}

	agent := New(cfg)

	// Initially, shutdown channel should be open
	if isClosed(agent.shutdownChan) {
		t.Error("shutdownChan should be open initially")
	}

	// Create a new agent to test closing behavior (avoid double close)
	agent2 := New(cfg)

	// After closing, it should be closed
	close(agent2.shutdownChan)
	if !isClosed(agent2.shutdownChan) {
		t.Error("shutdownChan should be closed after close()")
	}
}

// Benchmark tests
func BenchmarkResourceManager_AddResource(b *testing.B) {
	rm := NewResourceManager()
	closers := make([]*mockCloser, b.N)

	for i := 0; i < b.N; i++ {
		closers[i] = &mockCloser{}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rm.AddResource(closers[i], "file")
	}
}

func BenchmarkResourceManager_Cleanup(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		rm := NewResourceManager()
		// Add 100 resources
		for j := 0; j < 100; j++ {
			rm.AddResource(&mockCloser{}, "file")
		}
		b.StartTimer()

		rm.Cleanup()
	}
}

func BenchmarkConfig_String(b *testing.B) {
	cfg := &Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = cfg.String()
	}
}
