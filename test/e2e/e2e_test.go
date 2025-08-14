package e2e

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/agent"
	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
)

func TestAgent_E2E_WithAPI(t *testing.T) {
	// Start mock API server
	mockAPI := NewMockAPIServer()
	defer mockAPI.Close()

	// Create agent configuration that points to mock API
	cfg := &agent.Config{
		LogLevel:        "debug",
		LogFormat:       "text",
		PollingInterval: 1 * time.Second,
		GracefulTimeout: 2 * time.Second,
		APIBaseURL:      mockAPI.URL,
		APIEndpoint:     "/probes",
		LabelSelector:   "env=test,private=false",
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

	// Wait a bit for agent to start and process probes
	time.Sleep(2 * time.Second)

	// Verify probes were fetched and processed
	t.Run("VerifyProbesFetched", func(t *testing.T) {
		// The agent should have fetched probes from the API
		// We can verify this by checking if the mock API received requests
		// For now, we'll check that the mock server still has the expected probes
		if mockAPI.GetProbeCount() != 2 {
			t.Errorf("Expected 2 probes in mock API, got %d", mockAPI.GetProbeCount())
		}

		// Check specific probe exists
		probe1 := mockAPI.GetProbe("test-probe-1")
		if probe1 == nil {
			t.Fatal("Expected test-probe-1 to exist")
		}

		if probe1.StaticURL != "https://httpbin.org/status/200" {
			t.Errorf("Expected probe URL to be https://httpbin.org/status/200, got %s", probe1.StaticURL)
		}
	})

	// Test probe status updates
	t.Run("VerifyProbeStatusUpdates", func(t *testing.T) {
		// Simulate probe status update
		client := api.NewClient(mockAPI.URL, "", "/probes", "")
		
		err := client.UpdateProbeStatus("test-probe-1", "running")
		if err != nil {
			t.Fatalf("Failed to update probe status: %v", err)
		}

		// Verify status was updated
		probe := mockAPI.GetProbe("test-probe-1")
		if probe.Status != "running" {
			t.Errorf("Expected probe status to be 'running', got '%s'", probe.Status)
		}
	})

	// Test probe retrieval with label selector
	t.Run("VerifyLabelSelector", func(t *testing.T) {
		client := api.NewClient(mockAPI.URL, "", "/probes", "")
		
		// Get probes with label selector
		probes, err := client.GetProbes("env=test,private=false")
		if err != nil {
			t.Fatalf("Failed to get probes: %v", err)
		}

		if len(probes) != 2 {
			t.Errorf("Expected 2 probes with label selector, got %d", len(probes))
		}

		// Verify all probes have the expected labels
		for _, probe := range probes {
			if probe.Labels["env"] != "test" {
				t.Errorf("Expected probe to have env=test label, got env=%s", probe.Labels["env"])
			}
			if probe.Labels["private"] != "false" {
				t.Errorf("Expected probe to have private=false label, got private=%s", probe.Labels["private"])
			}
		}
	})

	// Test probe filtering
	t.Run("VerifyProbeFiltering", func(t *testing.T) {
		client := api.NewClient(mockAPI.URL, "", "/probes", "")
		
		// Add a probe that shouldn't match the filter
		mockAPI.AddProbe(&api.Probe{
			ID:        "test-probe-excluded",
			StaticURL: "https://httpbin.org/status/500",
			Labels: map[string]string{
				"env":     "production",
				"private": "true",
			},
			Status: "pending",
		})

		// Get probes with test environment filter
		probes, err := client.GetProbes("env=test")
		if err != nil {
			t.Fatalf("Failed to get probes: %v", err)
		}

		// Should still only get 2 probes (the original test ones)
		if len(probes) != 2 {
			t.Errorf("Expected 2 probes with env=test filter, got %d", len(probes))
		}

		// Verify none of the returned probes have env=production
		for _, probe := range probes {
			if probe.Labels["env"] == "production" {
				t.Errorf("Unexpected probe with env=production returned: %s", probe.ID)
			}
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
		// Agent shut down gracefully
		if agentErr != nil {
			t.Logf("Agent returned error (expected for test): %v", agentErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Agent did not shut down within timeout")
	}
}

func TestAgent_E2E_ErrorHandling(t *testing.T) {
	// Test agent behavior when API is unavailable
	t.Run("APIUnavailable", func(t *testing.T) {
		cfg := &agent.Config{
			LogLevel:        "debug",
			LogFormat:       "text", 
			PollingInterval: 1 * time.Second,
			GracefulTimeout: 2 * time.Second,
			APIBaseURL:      "http://localhost:9999", // Non-existent server
			APIEndpoint:     "/probes",
			LabelSelector:   "env=test",
		}

		testAgent := agent.New(cfg)

		// Run agent briefly
		var agentErr error
		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			agentErr = testAgent.Run()
		}()

		// Let it run for a bit then shutdown
		time.Sleep(2 * time.Second)
		testAgent.Shutdown()

		// Wait for agent to shutdown
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Agent should handle API unavailability gracefully
			t.Logf("Agent handled API unavailability, error: %v", agentErr)
		case <-time.After(5 * time.Second):
			t.Fatal("Agent did not shut down within timeout when API unavailable")
		}
	})
}

func TestAgent_E2E_ConfigurationVariations(t *testing.T) {
	// Test different configuration scenarios
	mockAPI := NewMockAPIServer()
	defer mockAPI.Close()

	testCases := []struct {
		name           string
		config         *agent.Config
		expectedProbes int
	}{
		{
			name: "NoLabelSelector",
			config: &agent.Config{
				LogLevel:        "info",
				LogFormat:       "json",
				PollingInterval: 1 * time.Second,
				GracefulTimeout: 2 * time.Second,
				APIBaseURL:      mockAPI.URL,
				APIEndpoint:     "/probes",
				LabelSelector:   "", // No selector - should get all probes
			},
			expectedProbes: 2, // Should get all default test probes
		},
		{
			name: "RestrictiveLabelSelector",
			config: &agent.Config{
				LogLevel:        "info",
				LogFormat:       "json",
				PollingInterval: 1 * time.Second,
				GracefulTimeout: 2 * time.Second,
				APIBaseURL:      mockAPI.URL,
				APIEndpoint:     "/probes",
				LabelSelector:   "env=test,private=false,nonexistent=value",
			},
			expectedProbes: 0, // Should get no probes due to non-existent label
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testAgent := agent.New(tc.config)

			var wg sync.WaitGroup
			wg.Add(1)

			go func() {
				defer wg.Done()
				_ = testAgent.Run()
			}()

			// Let agent run briefly
			time.Sleep(2 * time.Second)

			// Test API client with same configuration
			client := api.NewClient(tc.config.APIBaseURL, "", tc.config.APIEndpoint, "")
			probes, err := client.GetProbes(tc.config.LabelSelector)
			
			if err != nil {
				t.Fatalf("Failed to get probes: %v", err)
			}

			if len(probes) != tc.expectedProbes {
				t.Errorf("Expected %d probes, got %d", tc.expectedProbes, len(probes))
			}

			testAgent.Shutdown()

			// Wait for shutdown
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
		})
	}
}

func TestMain(m *testing.M) {
	// Setup any global test configuration here
	code := m.Run()
	os.Exit(code)
}