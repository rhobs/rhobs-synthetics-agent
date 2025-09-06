package k8s

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
)

func TestProbeManager_ValidateURL_EdgeCases(t *testing.T) {
	pm := NewProbeManager("test", "")

	tests := []struct {
		name        string
		url         string
		expectError bool
	}{
		{
			name:        "empty URL",
			url:         "",
			expectError: true,
		},
		{
			name:        "malformed URL",
			url:         "not-a-url",
			expectError: true,
		},
		{
			name:        "URL with no scheme",
			url:         "example.com",
			expectError: true,
		},
		{
			name:        "FTP scheme",
			url:         "ftp://example.com",
			expectError: true,
		},
		{
			name:        "HTTP scheme - non-existent domain",
			url:         "http://non-existent-domain-12345.com",
			expectError: false, // URL format is valid, connectivity is not checked
		},
		{
			name:        "HTTPS scheme - non-existent domain",
			url:         "https://non-existent-domain-12345.com",
			expectError: false, // URL format is valid, connectivity is not checked
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pm.ValidateURL(tt.url)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestProbeManager_ValidateURL_ServerErrors(t *testing.T) {
	pm := NewProbeManager("test", "")

	// Test server that returns 500 - should not error since we only validate format
	server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server500.Close()

	err := pm.ValidateURL(server500.URL)
	if err != nil {
		t.Errorf("Did not expect error for server responses, only format validation: %v", err)
	}

	// Test server that returns 404 - should not error since we only validate format
	server404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server404.Close()

	err = pm.ValidateURL(server404.URL)
	if err != nil {
		t.Errorf("Did not expect error for server responses, only format validation: %v", err)
	}
}

func TestProbeManager_CreateProbeResource_MissingLabels(t *testing.T) {
	// Create a test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("monitoring", "")

	// Test probe with missing labels
	probe := api.Probe{
		ID:        "test-probe-missing-labels",
		StaticURL: server.URL,
		Labels:    map[string]string{}, // Empty labels
		Status:    "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "http://blackbox-exporter:9115",
	}

	cr, err := pm.CreateProbeResource(probe, probeConfig)
	if err != nil {
		t.Fatalf("CreateProbeResource() failed: %v", err)
	}

	// Should not have cluster_id or management_cluster_id when not provided
	if _, exists := cr.Spec.Targets.StaticConfig.Labels["cluster_id"]; exists {
		t.Errorf("Did not expect cluster_id label when not provided")
	}

	if _, exists := cr.Spec.Targets.StaticConfig.Labels["management_cluster_id"]; exists {
		t.Errorf("Did not expect management_cluster_id label when not provided")
	}
}

func TestProbeManager_CreateProbeResource_PartialLabels(t *testing.T) {
	// Create a test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("monitoring", "")

	// Test probe with only cluster_id
	probe := api.Probe{
		ID:        "test-probe-partial-labels",
		StaticURL: server.URL,
		Labels: map[string]string{
			"cluster_id": "cluster-123",
			"private":    "false",
		},
		Status: "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "http://blackbox-exporter:9115",
	}

	cr, err := pm.CreateProbeResource(probe, probeConfig)
	if err != nil {
		t.Fatalf("CreateProbeResource() failed: %v", err)
	}

	// Should use provided cluster_id and not have management_cluster_id when not provided
	if cr.Spec.Targets.StaticConfig.Labels["cluster_id"] != "cluster-123" {
		t.Errorf("Expected cluster_id 'cluster-123', got %s", cr.Spec.Targets.StaticConfig.Labels["cluster_id"])
	}

	if _, exists := cr.Spec.Targets.StaticConfig.Labels["management_cluster_id"]; exists {
		t.Errorf("Did not expect management_cluster_id label when not provided")
	}
}

func TestNewProbeManager(t *testing.T) {
	pm := NewProbeManager("test-namespace", "")
	
	if pm.namespace != "test-namespace" {
		t.Errorf("Expected namespace 'test-namespace', got %s", pm.namespace)
	}
	
	if pm.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
}
