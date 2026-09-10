package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestNew(t *testing.T) {
	cfg := &Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 30 * time.Second,
		GracefulTimeout: 30 * time.Second,
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	if agent == nil {
		t.Fatal("New() returned nil")
	}

	if agent.config != cfg {
		t.Error("New() did not set config correctly")
	}

	if agent.worker == nil {
		t.Error("New() did not initialize worker")
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
	agent, err := New(nil)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

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
}

func TestAgent_Run_QuickShutdown(t *testing.T) {
	cfg := &Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 100 * time.Millisecond, // Short interval for faster test
		GracefulTimeout: 100 * time.Millisecond,
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	// Test the shutdown channel mechanism directly
	done := make(chan error, 1)
	go func() {
		// Use a longer context but trigger shutdown via channel
		ctx := context.Background()
		done <- agent.worker.Start(ctx, &agent.taskWG, agent.shutdownChan)
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
		MetricsAddr:     ":0",
	}

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// startMetricsServer should block until context is cancelled
	err = agent.startMetricsServer(ctx)

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

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

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

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	// Verify the config string formatting works with the agent
	expected := "LogLevel=debug, LogFormat=text, PollingInterval=45s, GracefulTimeout=1m0s, APIURLs=[]"
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

	agent, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	// Initially, shutdown channel should be open
	if isClosed(agent.shutdownChan) {
		t.Error("shutdownChan should be open initially")
	}

	// Create a new agent to test closing behavior (avoid double close)
	agent2, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	// After closing, it should be closed
	close(agent2.shutdownChan)
	if !isClosed(agent2.shutdownChan) {
		t.Error("shutdownChan should be closed after close()")
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

func TestAgent_handleLiveness(t *testing.T) {
	agent, err := New(nil)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	req := httptest.NewRequest("GET", "/livez", nil)
	w := httptest.NewRecorder()

	agent.handleLiveness(w, req)

	resp := w.Result()
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	expected := "OK"
	if string(body) != expected {
		t.Errorf("Expected body %q, got %q", expected, string(body))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/plain" {
		t.Errorf("Expected Content-Type 'text/plain', got %q", contentType)
	}
}

func TestAgent_handleReadiness_NotReady(t *testing.T) {
	agent, err := New(nil)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}
	// Agent starts as not ready

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()

	agent.handleReadiness(w, req)

	resp := w.Result()
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	expected := "Not Ready"
	if string(body) != expected {
		t.Errorf("Expected body %q, got %q", expected, string(body))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/plain" {
		t.Errorf("Expected Content-Type 'text/plain', got %q", contentType)
	}
}

func TestAgent_handleReadiness_Ready(t *testing.T) {
	agent, err := New(nil)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}
	agent.setReady(true)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()

	agent.handleReadiness(w, req)

	resp := w.Result()
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	expected := "Ready"
	if string(body) != expected {
		t.Errorf("Expected body %q, got %q", expected, string(body))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/plain" {
		t.Errorf("Expected Content-Type 'text/plain', got %q", contentType)
	}
}

func TestAgent_ReadinessStateTransitions(t *testing.T) {
	agent, err := New(nil)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	// Initial state should be not ready
	if agent.isReady() {
		t.Error("Agent should not be ready initially")
	}

	// Set ready
	agent.setReady(true)
	if !agent.isReady() {
		t.Error("Agent should be ready after setReady(true)")
	}

	// Set not ready
	agent.setReady(false)
	if agent.isReady() {
		t.Error("Agent should not be ready after setReady(false)")
	}
}

func TestAgent_ReadinessConcurrency(t *testing.T) {
	agent, err := New(nil)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	var wg sync.WaitGroup

	// Start multiple goroutines that set readiness
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(ready bool) {
			defer wg.Done()
			agent.setReady(ready)
		}(i%2 == 0)
	}

	// Start multiple goroutines that check readiness
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = agent.isReady()
		}()
	}

	wg.Wait()
	// Test should complete without race conditions
}

func TestAgent_HealthEndpointsInMetricsServer(t *testing.T) {
	agent, err := New(nil)
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	// Create a test server using the same mux configuration as the agent
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", agent.handleLiveness)
	mux.HandleFunc("/readyz", agent.handleReadiness)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test liveness endpoint
	resp, err := http.Get(server.URL + "/livez")
	if err != nil {
		t.Fatalf("Failed to call liveness endpoint: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("Failed to close response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Liveness endpoint returned %d, expected %d", resp.StatusCode, http.StatusOK)
	}

	// Test readiness endpoint (should be not ready initially)
	resp, err = http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("Failed to call readiness endpoint: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("Failed to close response body: %v", err)
	}

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Readiness endpoint returned %d, expected %d", resp.StatusCode, http.StatusServiceUnavailable)
	}

	// Set agent as ready and test again
	agent.setReady(true)

	resp, err = http.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("Failed to call readiness endpoint after setting ready: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("Failed to close response body: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Readiness endpoint returned %d after setting ready, expected %d", resp.StatusCode, http.StatusOK)
	}
}

func waitReady(t *testing.T, agent *Agent, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if agent.isReady() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent did not become ready")
}

func TestLeaderCallbacks_ReadinessIndependentOfLeadership(t *testing.T) {
	agent, err := New(&Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: time.Second,
		GracefulTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	cbs := agent.leaderCallbacks("pod-1")

	cbs.OnNewLeader("other-pod")
	if !agent.isReady() {
		t.Fatal("standby should be ready after observing a leader")
	}

	agent.setReady(false)
	cbs.OnNewLeader("pod-1")
	if !agent.isReady() {
		t.Fatal("leader should be ready after OnNewLeader for its own identity")
	}

	cbs.OnStoppedLeading()
	if !agent.isReady() {
		t.Fatal("losing the lease must not clear readiness")
	}
}

func TestRunLeaderElection_MarksReadyAfterRBACCheck(t *testing.T) {
	agent, err := New(&Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: time.Second,
		GracefulTimeout: time.Second,
		LeaderElect:     true,
	})
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	fakeClient := kubefake.NewSimpleClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- agent.runLeaderElection(ctx, fakeClient, "pod-1", "default", nil)
	}()

	waitReady(t, agent, 5*time.Second)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runLeaderElection returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runLeaderElection did not return after cancel")
	}
}

func TestRunLeaderElection_RBACFailureDoesNotMarkReady(t *testing.T) {
	agent, err := New(&Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: time.Second,
		GracefulTimeout: time.Second,
		LeaderElect:     true,
	})
	if err != nil {
		t.Fatalf("unexpected error creating agent: %v", err)
	}

	fakeClient := kubefake.NewSimpleClientset()
	fakeClient.PrependReactor("list", "leases", func(action clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewForbidden(
			schema.GroupResource{Group: "coordination.k8s.io", Resource: "leases"},
			"",
			fmt.Errorf("leases is forbidden"),
		)
	})

	err = agent.runLeaderElection(context.Background(), fakeClient, "pod-1", "default", nil)
	if err == nil {
		t.Fatal("expected RBAC check to fail")
	}
	if agent.isReady() {
		t.Fatal("agent must not be ready when lease RBAC is missing")
	}
}
