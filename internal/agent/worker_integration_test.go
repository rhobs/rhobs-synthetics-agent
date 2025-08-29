package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	"github.com/rhobs/rhobs-synthetics-agent/internal/k8s"
)

func TestWorker_processProbe_Success(t *testing.T) {
	// Create a mock server for URL validation
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer validationServer.Close()

	cfg := &Config{
		APIURLs:        []string{"http://example.com/api/metrics/v1/test/probes"},
		LabelSelector: "test=true",
		Namespace:     "monitoring",
		Blackbox: k8s.BlackboxConfig{
			Probing: k8s.BlackboxProbingConfig{
				Interval:  "30s",
				Module:    "http_2xx",
				ProberURL: "http://blackbox-exporter:9115",
			},
		},
	}

	worker, err := NewWorker(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	probe := api.Probe{
		ID:        "test-probe",
		StaticURL: validationServer.URL,
		Labels: map[string]string{
			"cluster_id":            "cluster-123",
			"management_cluster_id": "mgmt-456",
			"private":               "false",
		},
		Status: "pending",
	}

	ctx := context.Background()
	err = worker.processProbe(ctx, probe)
	if err != nil {
		t.Errorf("processProbe() failed: %v", err)
	}
}

func TestWorker_processProbe_ValidationFailure(t *testing.T) {
	cfg := &Config{
		APIURLs:        []string{"http://example.com/api/metrics/v1/test/probes"},
		LabelSelector: "test=true",
		Namespace:     "monitoring",
		Blackbox: k8s.BlackboxConfig{
			Probing: k8s.BlackboxProbingConfig{
				Interval:  "30s",
				Module:    "http_2xx",
				ProberURL: "http://blackbox-exporter:9115",
			},
		},
	}

	worker, err := NewWorker(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	// Probe with invalid URL
	probe := api.Probe{
		ID:        "test-probe-invalid",
		StaticURL: "https://non-existent-domain-12345.com",
		Labels: map[string]string{
			"cluster_id": "cluster-123",
			"private":    "false",
		},
		Status: "pending",
	}

	ctx := context.Background()
	err = worker.processProbe(ctx, probe)
	if err == nil {
		t.Error("processProbe() should fail for invalid URL")
	}
}

func TestWorker_FullIntegration(t *testing.T) {
	// Create a mock validation server
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer validationServer.Close()

	// Create a mock API server
	var updateCalls []string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/metrics/v1/test/probes" {
			// Return test probes
			response := api.ProbeListResponse{
				Probes: []api.Probe{
					{
						ID:        "probe-1",
						StaticURL: validationServer.URL,
						Labels: map[string]string{
							"cluster_id": "cluster-123",
							"private":    "false",
						},
						Status: "pending",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(response)
		} else if r.Method == "PATCH" {
			// Record status updates
			var statusUpdate api.ProbeStatusUpdate
			_ = json.NewDecoder(r.Body).Decode(&statusUpdate)
			updateCalls = append(updateCalls, statusUpdate.Status)
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer apiServer.Close()

	cfg := &Config{
		PollingInterval: 100 * time.Millisecond,
		GracefulTimeout: 1 * time.Second,
		APIURLs:          []string{apiServer.URL + "/api/metrics/v1/test/probes"},
		LabelSelector:   "test=true",
		Namespace:       "monitoring",
		Blackbox: k8s.BlackboxConfig{
			Probing: k8s.BlackboxProbingConfig{
				Interval:  "30s",
				Module:    "http_2xx",
				ProberURL: "http://blackbox-exporter:9115",
			},
		},
	}

	worker, err := NewWorker(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Run worker for a short time
	err = worker.Start(ctx, &taskWG, shutdownChan)
	if err == nil {
		t.Error("Expected context timeout error")
	}

	// Wait for tasks to complete
	taskWG.Wait()

	// Should have at least one status update call
	if len(updateCalls) == 0 {
		t.Error("Expected at least one status update call")
	}

	// The status should be "active" for successful processing
	found := false
	for _, status := range updateCalls {
		if status == "active" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected at least one 'active' status update")
	}
}

func TestWorker_processProbes_WithValidConfig(t *testing.T) {
	// Create a mock validation server
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer validationServer.Close()

	// Create a mock API server that returns empty probe list
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := api.ProbeListResponse{Probes: []api.Probe{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer apiServer.Close()

	cfg := &Config{
		APIURLs:        []string{apiServer.URL + "/api/metrics/v1/test/probes"},
		LabelSelector: "test=true",
		Namespace:     "monitoring",
		Blackbox: k8s.BlackboxConfig{
			Probing: k8s.BlackboxProbingConfig{
				Interval:  "30s",
				Module:    "http_2xx",
				ProberURL: "http://blackbox-exporter:9115",
			},
		},
	}

	worker, err := NewWorker(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	var taskWG sync.WaitGroup
	shutdownChan := make(chan struct{})

	ctx := context.Background()
	err = worker.processProbes(ctx, &taskWG, shutdownChan)
	if err != nil {
		t.Errorf("processProbes() failed: %v", err)
	}

	// Wait for task to complete
	taskWG.Wait()
}
