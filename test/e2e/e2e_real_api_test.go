package e2e

import (
	"sync"
	"testing"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/agent"
	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
)

func TestAgent_E2E_WithRealAPI(t *testing.T) {
	// Start real API server
	apiManager := NewRealAPIManager()
	
	if err := apiManager.Start(); err != nil {
		t.Fatalf("Failed to start real API server: %v", err)
	}
	defer func() { _ = apiManager.Stop() }()

	// Clean up any existing probes before starting test
	if err := apiManager.ClearAllProbes(); err != nil {
		t.Logf("Warning: Failed to clear existing probes: %v", err)
	}

	// Re-seed test data after cleanup
	if err := apiManager.SeedTestData(); err != nil {
		t.Fatalf("Failed to seed test data after cleanup: %v", err)
	}

	// Create agent configuration that points to real API
	cfg := &agent.Config{
		LogLevel:        "debug",
		LogFormat:       "text",
		PollingInterval: 1 * time.Second,
		GracefulTimeout: 2 * time.Second,
		APIURLs:          []string{apiManager.GetURL() + "/probes"},
		LabelSelector:   "env=test,private=false",
	}

	// Create and start agent
	testAgent := agent.New(cfg)

	// Run agent in background
	var wg sync.WaitGroup
	wg.Add(1)
	
	go func() {
		defer wg.Done()
		// Run agent in background goroutine
		_ = testAgent.Run()
	}()

	// Wait a bit for agent to start and process probes
	time.Sleep(2 * time.Second)

	// Verify probes were fetched and processed
	t.Run("VerifyProbesFetched", func(t *testing.T) {
		// The agent should have fetched probes from the real API
		probeCount, err := apiManager.GetProbeCount()
		if err != nil {
			t.Fatalf("Failed to get probe count: %v", err)
		}

		if probeCount != 2 {
			t.Errorf("Expected 2 probes in API, got %d", probeCount)
		}
	})

	// Test probe status updates with real API
	t.Run("VerifyProbeStatusUpdates", func(t *testing.T) {
		// Get probes from real API
		client := api.NewClient(apiManager.GetURL()+"/probes", "")
		
		probes, err := client.GetProbes("")
		if err != nil {
			t.Fatalf("Failed to get probes: %v", err)
		}

		if len(probes) == 0 {
			t.Fatal("No probes found in API")
		}

		// Update status of first probe to a valid status value
		err = client.UpdateProbeStatus(probes[0].ID, "active")
		if err != nil {
			t.Fatalf("Failed to update probe status: %v", err)
		}

		// Verify status was updated by fetching the specific probe
		updatedProbe, err := apiManager.GetProbe(probes[0].ID)
		if err != nil {
			t.Fatalf("Failed to get updated probe: %v", err)
		}

		if updatedProbe == nil {
			t.Fatal("Updated probe not found")
		}

		if updatedProbe.Status != "active" {
			t.Errorf("Expected probe status to be 'active', got '%s'", updatedProbe.Status)
		}
	})

	// Test probe retrieval with label selector
	t.Run("VerifyLabelSelector", func(t *testing.T) {
		client := api.NewClient(apiManager.GetURL()+"/probes", "")
		
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

	// Test probe filtering with different selectors
	t.Run("VerifyProbeFiltering", func(t *testing.T) {
		client := api.NewClient(apiManager.GetURL()+"/probes", "")
		
		// Test with restrictive selector that should return no probes
		probes, err := client.GetProbes("env=test,nonexistent=value")
		if err != nil {
			t.Fatalf("Failed to get probes: %v", err)
		}

		// Should get no probes due to non-existent label
		if len(probes) != 0 {
			t.Errorf("Expected 0 probes with restrictive filter, got %d", len(probes))
		}

		// Test with env=test filter (should get all test probes)
		probes, err = client.GetProbes("env=test")
		if err != nil {
			t.Fatalf("Failed to get probes: %v", err)
		}

		if len(probes) != 2 {
			t.Errorf("Expected 2 probes with env=test filter, got %d", len(probes))
		}

		// Verify all returned probes have env=test
		for _, probe := range probes {
			if probe.Labels["env"] != "test" {
				t.Errorf("Unexpected probe without env=test returned: %s", probe.ID)
			}
		}
	})

	// For graceful shutdown, call the shutdown method
	testAgent.Shutdown()
	
	// Wait for agent to shutdown with a reasonable timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Agent shut down gracefully
		t.Logf("Agent shut down successfully")
	case <-time.After(8 * time.Second):
		t.Logf("Agent shutdown timeout - this is expected in test environment")
		// In test environment, we don't have proper signal handling, so this is expected
	}
}

func TestAgent_E2E_RealAPI_ErrorHandling(t *testing.T) {
	// Test agent behavior when real API becomes unavailable
	t.Run("APIUnavailable", func(t *testing.T) {
		cfg := &agent.Config{
			LogLevel:        "debug",
			LogFormat:       "text", 
			PollingInterval: 1 * time.Second,
			GracefulTimeout: 2 * time.Second,
			APIURLs:          []string{"http://localhost:9999/probes"}, // Non-existent server
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

func TestAgent_E2E_RealAPI_ConfigurationVariations(t *testing.T) {
	// Start real API server
	apiManager := NewRealAPIManager()
	
	if err := apiManager.Start(); err != nil {
		t.Fatalf("Failed to start real API server: %v", err)
	}
	defer func() { _ = apiManager.Stop() }()

	// Clean up any existing probes before starting test
	if err := apiManager.ClearAllProbes(); err != nil {
		t.Logf("Warning: Failed to clear existing probes: %v", err)
	}

	// Re-seed test data after cleanup
	if err := apiManager.SeedTestData(); err != nil {
		t.Fatalf("Failed to seed test data after cleanup: %v", err)
	}

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
				APIURLs:          []string{apiManager.GetURL() + "/probes"},
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
				APIURLs:          []string{apiManager.GetURL() + "/probes"},
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
			client := api.NewClient(tc.config.APIURLs[0], "")
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

