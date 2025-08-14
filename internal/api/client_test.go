package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetProbes(t *testing.T) {
	// Mock server with sample probe data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		if r.URL.Path != "/api/metrics/v1/test-tenant/probes" {
			t.Errorf("Expected path /api/metrics/v1/test-tenant/probes, got %s", r.URL.Path)
		}

		labelSelector := r.URL.Query().Get("label_selector")
		if labelSelector != "private=false,rhobs-synthetics/status=pending" {
			t.Errorf("Expected label selector 'private=false,rhobs-synthetics/status=pending', got %s", labelSelector)
		}

		// Return sample probes
		response := ProbeListResponse{
			Probes: []Probe{
				{
					ID:        "probe-1",
					StaticURL: "https://api.openshift.com/health",
					Labels: map[string]string{
						"cluster_id":            "cluster-123",
						"management_cluster_id": "mgmt-cluster-456",
						"private":               "false",
					},
					Status: "pending",
				},
				{
					ID:        "probe-2",
					StaticURL: "https://console.redhat.com/api/health",
					Labels: map[string]string{
						"cluster_id":            "cluster-789",
						"management_cluster_id": "mgmt-cluster-456",
						"private":               "false",
					},
					Status: "pending",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-tenant", "/api/metrics/v1", "")
	probes, err := client.GetProbes("private=false,rhobs-synthetics/status=pending")

	if err != nil {
		t.Fatalf("GetProbes() failed: %v", err)
	}

	if len(probes) != 2 {
		t.Errorf("Expected 2 probes, got %d", len(probes))
	}

	probe1 := probes[0]
	if probe1.ID != "probe-1" {
		t.Errorf("Expected probe ID 'probe-1', got %s", probe1.ID)
	}

	if probe1.StaticURL != "https://api.openshift.com/health" {
		t.Errorf("Expected URL 'https://api.openshift.com/health', got %s", probe1.StaticURL)
	}

	if probe1.Labels["cluster_id"] != "cluster-123" {
		t.Errorf("Expected cluster_id 'cluster-123', got %s", probe1.Labels["cluster_id"])
	}
}

func TestClient_UpdateProbeStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("Expected PATCH request, got %s", r.Method)
		}

		if r.URL.Path != "/api/metrics/v1/test-tenant/probes/probe-1" {
			t.Errorf("Expected path /api/metrics/v1/test-tenant/probes/probe-1, got %s", r.URL.Path)
		}

		var statusUpdate ProbeStatusUpdate
		if err := json.NewDecoder(r.Body).Decode(&statusUpdate); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
			return
		}

		if statusUpdate.Status != "active" {
			t.Errorf("Expected status 'active', got %s", statusUpdate.Status)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-tenant", "/api/metrics/v1", "")
	err := client.UpdateProbeStatus("probe-1", "active")

	if err != nil {
		t.Fatalf("UpdateProbeStatus() failed: %v", err)
	}
}