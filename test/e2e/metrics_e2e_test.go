package e2e

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/agent"
	"github.com/rhobs/rhobs-synthetics-agent/internal/version"
)

func TestAgent_E2E_Metrics(t *testing.T) {
	// Start mock API server
	mockAPI := NewMockAPIServer()
	defer mockAPI.Close()

	// Create agent configuration
	cfg := &agent.Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 2 * time.Second,
		GracefulTimeout: 5 * time.Second,
		APIBaseURLs:     []string{mockAPI.URL},
		APIEndpoint:     "/probes",
		LabelSelector:   "env=test",
	}

	// Create and start agent
	testAgent := agent.New(cfg)

	// Run agent in background
	var agentErr error
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		agentErr = testAgent.Run()
	}()

	// Wait for agent to start and perform initial reconciliation
	time.Sleep(4 * time.Second)

	t.Run("MetricsEndpointAccessible", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/metrics")
		if err != nil {
			t.Fatalf("Failed to access metrics endpoint: %v", err)
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Failed to close response body: %v", closeErr)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/plain") {
			t.Errorf("Expected Content-Type to contain 'text/plain', got %s", contentType)
		}
	})

	t.Run("PrometheusFormatValidation", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/metrics")
		if err != nil {
			t.Fatalf("Failed to access metrics endpoint: %v", err)
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Failed to close response body: %v", closeErr)
			}
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		content := string(body)

		// Validate basic Prometheus format
		if !strings.Contains(content, "# HELP") {
			t.Error("Response should contain HELP comments")
		}

		if !strings.Contains(content, "# TYPE") {
			t.Error("Response should contain TYPE comments")
		}

		// Check for standard Go metrics (these should always be present)
		requiredMetrics := []string{
			"go_goroutines",
			"go_memstats_alloc_bytes",
			"process_cpu_seconds_total",
		}

		for _, metric := range requiredMetrics {
			if !strings.Contains(content, metric) {
				t.Errorf("Expected metric %s not found in response", metric)
			}
		}
	})

	t.Run("CustomMetricsPresent", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/metrics")
		if err != nil {
			t.Fatalf("Failed to access metrics endpoint: %v", err)
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Failed to close response body: %v", closeErr)
			}
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		content := string(body)

		// Check for our custom metrics that should always be present
		customMetrics := []string{
			"rhobs_synthetics_agent_info",
			"rhobs_synthetics_agent_reconciliation_duration_seconds",
			"rhobs_synthetics_agent_reconciliation_total",
			"rhobs_synthetics_agent_last_reconciliation_timestamp_seconds",
			"rhobs_synthetics_agent_probe_list_fetch_duration_seconds",
			"rhobs_synthetics_agent_probe_list_fetch_total",
			"rhobs_synthetics_agent_probe_resource_operations_total",
		}

		// Optional metrics that may not appear if no values are recorded
		optionalMetrics := []string{
			"rhobs_synthetics_agent_probe_resources_managed",
		}

		for _, metric := range customMetrics {
			if !strings.Contains(content, metric) {
				t.Errorf("Expected custom metric %s not found in response", metric)
			}
		}

		// Optional metrics - just log if they're not present
		for _, metric := range optionalMetrics {
			if !strings.Contains(content, metric) {
				t.Logf("Optional metric %s not found in response (expected if no values recorded)", metric)
			}
		}
	})

	t.Run("MetricLabelsValidation", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/metrics")
		if err != nil {
			t.Fatalf("Failed to access metrics endpoint: %v", err)
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Failed to close response body: %v", closeErr)
			}
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		content := string(body)

		// Check for expected labels in agent info metric
		if !strings.Contains(content, `rhobs_synthetics_agent_info{namespace="default",version="`+version.Version+`"}`) {
			t.Error("Agent info metric should have namespace and version labels")
		}

		// Check for reconciliation metrics with status labels
		if !strings.Contains(content, `rhobs_synthetics_agent_reconciliation_total{status="success"}`) {
			t.Error("Reconciliation total metric should have status label")
		}

		// Check for API fetch metrics with endpoint and status labels
		if strings.Contains(content, "rhobs_synthetics_agent_probe_list_fetch_total") {
			// This metric should have endpoint and status labels when API calls are made
			if !strings.Contains(content, "api_endpoint=") {
				t.Error("Probe list fetch metric should have api_endpoint label")
			}
		}
	})

	t.Run("MetricValuesReasonable", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/metrics")
		if err != nil {
			t.Fatalf("Failed to access metrics endpoint: %v", err)
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Failed to close response body: %v", closeErr)
			}
		}()

		scanner := bufio.NewScanner(resp.Body)
		
		var reconciliationCount int
		var agentInfoCount int
		
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			
			// Skip comments and empty lines
			if strings.HasPrefix(line, "#") || line == "" {
				continue
			}

			// Count reconciliation total metrics
			if strings.Contains(line, "rhobs_synthetics_agent_reconciliation_total{status=\"success\"}") {
				var value float64
				_, err := fmt.Sscanf(line, "rhobs_synthetics_agent_reconciliation_total{status=\"success\"} %f", &value)
				if err == nil && value >= 1 {
					reconciliationCount++
				}
			}

			// Check agent info metric
			if strings.Contains(line, "rhobs_synthetics_agent_info{") {
				var value float64
				parts := strings.Split(line, " ")
				if len(parts) == 2 {
					_, err := fmt.Sscanf(parts[1], "%f", &value)
					if err == nil && value == 1 {
						agentInfoCount++
					}
				}
			}
		}

		if reconciliationCount == 0 {
			t.Error("Expected at least one successful reconciliation to be recorded")
		}

		if agentInfoCount == 0 {
			t.Error("Expected agent info metric to be present with value 1")
		}
	})

	// Gracefully shutdown the agent
	testAgent.Shutdown()

	// Wait for agent to shutdown
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if agentErr != nil {
			t.Logf("Agent returned error (expected for test): %v", agentErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Agent did not shut down within timeout")
	}
}

func TestAgent_E2E_MetricsWithAPIFailure(t *testing.T) {
	// Test metrics when API is unavailable
	cfg := &agent.Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 1 * time.Second,
		GracefulTimeout: 3 * time.Second,
		APIBaseURLs:     []string{"http://localhost:9999"}, // Non-existent server
		APIEndpoint:     "/probes",
		LabelSelector:   "env=test",
	}

	testAgent := agent.New(cfg)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		_ = testAgent.Run()
	}()

	// Let it run and attempt API calls
	time.Sleep(3 * time.Second)

	t.Run("MetricsWithAPIFailure", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/metrics")
		if err != nil {
			t.Fatalf("Failed to access metrics endpoint: %v", err)
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Failed to close response body: %v", closeErr)
			}
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		content := string(body)

		// Should still have reconciliation metrics (even if API fails)
		if !strings.Contains(content, "rhobs_synthetics_agent_reconciliation_total") {
			t.Error("Reconciliation metrics should be present even with API failures")
		}

		// Check for API failure metrics if any API calls were attempted
		if strings.Contains(content, "rhobs_synthetics_agent_probe_list_fetch_total") {
			// If API metrics are present, there should be error status
			if !strings.Contains(content, `status="error"`) {
				t.Error("Expected error status in API fetch metrics when API is unavailable")
			}
		}
	})

	testAgent.Shutdown()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(8 * time.Second):
		t.Fatal("Agent did not shut down within timeout")
	}
}

func TestAgent_E2E_MetricsStandaloneMode(t *testing.T) {
	// Test metrics when running in standalone mode (no API URLs configured)
	cfg := &agent.Config{
		LogLevel:        "info",
		LogFormat:       "json",
		PollingInterval: 1 * time.Second,
		GracefulTimeout: 2 * time.Second,
		APIBaseURLs:     []string{}, // No API URLs - standalone mode
	}

	testAgent := agent.New(cfg)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		_ = testAgent.Run()
	}()

	// Let it run in standalone mode
	time.Sleep(3 * time.Second)

	t.Run("MetricsInStandaloneMode", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8080/metrics")
		if err != nil {
			t.Fatalf("Failed to access metrics endpoint: %v", err)
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Logf("Failed to close response body: %v", closeErr)
			}
		}()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		content := string(body)

		// Should have basic metrics
		if !strings.Contains(content, "rhobs_synthetics_agent_info") {
			t.Error("Agent info metric should be present in standalone mode")
		}

		if !strings.Contains(content, "rhobs_synthetics_agent_reconciliation_total") {
			t.Error("Reconciliation metrics should be present in standalone mode")
		}

		// Should NOT have API fetch metrics since no API calls are made
		// Note: The metrics will be registered but may have zero values
	})

	testAgent.Shutdown()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Agent did not shut down within timeout")
	}
}