package k8s

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
)

func TestProbeManager_ValidateURL(t *testing.T) {
	// Create a test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("test-namespace")

	// Test valid URL
	err := pm.ValidateURL(server.URL)
	if err != nil {
		t.Errorf("ValidateURL() failed for valid URL: %v", err)
	}

	// Test invalid URL
	err = pm.ValidateURL("invalid-url")
	if err == nil {
		t.Error("ValidateURL() should fail for invalid URL")
	}

	// Test unsupported scheme
	err = pm.ValidateURL("ftp://example.com")
	if err == nil {
		t.Error("ValidateURL() should fail for unsupported scheme")
	}
}

func TestProbeManager_CreateProbeResource(t *testing.T) {
	// Create a test server that returns 200 OK for URL validation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("monitoring")

	probe := api.Probe{
		ID:        "test-probe-1",
		StaticURL: server.URL,
		Labels: map[string]string{
			"cluster_id":            "cluster-123",
			"management_cluster_id": "mgmt-cluster-456",
			"private":               "false",
		},
		Status: "pending",
	}

	probeConfig := ProbeConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "http://blackbox-exporter:9115",
	}

	cr, err := pm.CreateProbeResource(probe, probeConfig)
	if err != nil {
		t.Fatalf("CreateProbeResource() failed: %v", err)
	}

	// Verify the Custom Resource structure
	if cr.APIVersion != "monitoring.rhobs/v1" {
		t.Errorf("Expected APIVersion 'monitoring.rhobs/v1', got %s", cr.APIVersion)
	}

	if cr.Kind != "Probe" {
		t.Errorf("Expected Kind 'Probe', got %s", cr.Kind)
	}

	expectedName := "probe-test-probe-1"
	if cr.Metadata["name"] != expectedName {
		t.Errorf("Expected name '%s', got %s", expectedName, cr.Metadata["name"])
	}

	if cr.Metadata["namespace"] != "monitoring" {
		t.Errorf("Expected namespace 'monitoring', got %s", cr.Metadata["namespace"])
	}

	// Verify the spec
	if cr.Spec.Interval != "30s" {
		t.Errorf("Expected interval '30s', got %s", cr.Spec.Interval)
	}

	if cr.Spec.Module != "http_2xx" {
		t.Errorf("Expected module 'http_2xx', got %s", cr.Spec.Module)
	}

	if len(cr.Spec.Targets.StaticConfig.Static) != 1 {
		t.Errorf("Expected 1 target, got %d", len(cr.Spec.Targets.StaticConfig.Static))
	}

	if cr.Spec.Targets.StaticConfig.Static[0] != server.URL {
		t.Errorf("Expected target URL '%s', got %s", server.URL, cr.Spec.Targets.StaticConfig.Static[0])
	}

	// Verify labels
	if cr.Spec.Targets.StaticConfig.Labels["cluster_id"] != "cluster-123" {
		t.Errorf("Expected cluster_id 'cluster-123', got %s", cr.Spec.Targets.StaticConfig.Labels["cluster_id"])
	}
}

func TestProbeManager_CreateProbeResource_InvalidURL(t *testing.T) {
	pm := NewProbeManager("monitoring")

	probe := api.Probe{
		ID:        "test-probe-invalid",
		StaticURL: "https://non-existent-domain-12345.com",
		Labels: map[string]string{
			"cluster_id": "cluster-123",
			"private":    "false",
		},
		Status: "pending",
	}

	probeConfig := ProbeConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "http://blackbox-exporter:9115",
	}

	_, err := pm.CreateProbeResource(probe, probeConfig)
	if err == nil {
		t.Error("CreateProbeResource() should fail for invalid URL")
	}
}