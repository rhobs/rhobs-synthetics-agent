package k8s

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rhobs/rhobs-synthetics-agent/internal/api"
	"github.com/rhobs/rhobs-synthetics-api/pkg/kubeclient"
)

func TestProbeManager_ValidateURL(t *testing.T) {
	// Create a test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("test-namespace", "")

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

	pm := NewProbeManager("monitoring", "")
	// Set API group for testing - test with monitoring.rhobs which should be preferred
	pm.SetProbeAPIGroup("monitoring.rhobs")

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

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "synthetics-blackbox-prober-default-service:9115",
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
	if cr.Name != expectedName {
		t.Errorf("Expected name '%s', got %s", expectedName, cr.Name)
	}

	if cr.Namespace != "monitoring" {
		t.Errorf("Expected namespace 'monitoring', got %s", cr.Namespace)
	}

	// Verify the spec
	if cr.Spec.Interval != "30s" {
		t.Errorf("Expected interval '30s', got %s", cr.Spec.Interval)
	}

	if cr.Spec.Module != "http_2xx" {
		t.Errorf("Expected module 'http_2xx', got %s", cr.Spec.Module)
	}

	if len(cr.Spec.Targets.StaticConfig.Targets) != 1 {
		t.Errorf("Expected 1 target, got %d", len(cr.Spec.Targets.StaticConfig.Targets))
	}

	if cr.Spec.Targets.StaticConfig.Targets[0] != server.URL {
		t.Errorf("Expected target URL '%s', got %s", server.URL, cr.Spec.Targets.StaticConfig.Targets[0])
	}

	// Verify labels
	if cr.Spec.Targets.StaticConfig.Labels["cluster_id"] != "cluster-123" {
		t.Errorf("Expected cluster_id 'cluster-123', got %s", cr.Spec.Targets.StaticConfig.Labels["cluster_id"])
	}
}

func TestProbeManager_CreateProbeResource_InvalidURL(t *testing.T) {
	pm := NewProbeManager("monitoring", "")

	probe := api.Probe{
		ID:        "test-probe-invalid",
		StaticURL: "invalid-url-format",
		Labels: map[string]string{
			"cluster_id": "cluster-123",
			"private":    "false",
		},
		Status: "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "synthetics-blackbox-prober-default-service:9115",
	}

	_, err := pm.CreateProbeResource(probe, probeConfig)
	if err == nil {
		t.Error("CreateProbeResource() should fail for invalid URL")
	}
}

func TestProbeManager_isRunningInK8sCluster(t *testing.T) {
	// Test the kubeclient function directly since ProbeManager no longer has this method
	result := kubeclient.IsRunningInK8sCluster()

	// In test environment, should return false since we don't have K8s service account
	if result {
		t.Log("Running in actual Kubernetes environment - this is expected if tests are run in a K8s pod")
	} else {
		t.Log("Not running in Kubernetes environment - this is expected for local testing")
	}
}

func TestProbeManager_APIVersionFallback(t *testing.T) {
	// Create a test server that returns 200 OK for URL validation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("monitoring", "")
	// Force empty probeAPIGroup to test fallback behavior
	pm.SetProbeAPIGroup("")

	probe := api.Probe{
		ID:        "test-probe-fallback",
		StaticURL: server.URL,
		Labels: map[string]string{
			"cluster_id": "cluster-123",
		},
		Status: "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "synthetics-blackbox-prober-default-service:9115",
	}

	cr, err := pm.CreateProbeResource(probe, probeConfig)
	if err != nil {
		t.Fatalf("CreateProbeResource() failed: %v", err)
	}

	// Should fallback to monitoring.coreos.com when probeAPIGroup is empty
	if cr.APIVersion != "monitoring.coreos.com/v1" {
		t.Errorf("Expected fallback APIVersion 'monitoring.coreos.com/v1', got %s", cr.APIVersion)
	}
}

func TestProbeManager_initializeK8sClients(t *testing.T) {
	pm := NewProbeManager("test-namespace", "")

	// After initialization, check state
	if pm.isK8sCluster() {
		// If we're actually in a K8s cluster
		if pm.kubeClient == nil {
			t.Error("kubeClient should not be nil when in K8s cluster")
		}
		if pm.kubeClient.DynamicClient() == nil {
			t.Error("dynamicClient should not be nil when in K8s cluster")
		}
	} else {
		// If we're not in a K8s cluster (normal test case)
		if pm.kubeClient != nil {
			t.Error("kubeClient should be nil when not in K8s cluster")
		}
	}
}

func TestProbeManager_CreateProbeK8sResource_NotInCluster(t *testing.T) {
	// Create a test server for URL validation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := NewProbeManager("test-namespace", "")

	probe := api.Probe{
		ID:        "test-probe-k8s",
		StaticURL: server.URL,
		Labels: map[string]string{
			"cluster_id": "test-cluster",
		},
		Status: "pending",
	}

	probeConfig := BlackboxProbingConfig{
		Interval:  "30s",
		Module:    "http_2xx",
		ProberURL: "synthetics-blackbox-prober-default-service:9115",
	}

	// This should fail either because we're not in a K8s cluster OR due to permissions
	err := pm.CreateProbeK8sResource(probe, probeConfig)
	if err == nil {
		t.Error("CreateProbeK8sResource() should fail when not in K8s cluster or lacking permissions")
	}

	// Accept either error scenario:
	// 1. "not running in a Kubernetes cluster" (when running outside K8s)
	// 2. Permission denied (when running in K8s CI without proper RBAC)
	expectedErrors := []string{
		"not running in a Kubernetes cluster",
		"failed to create Probe resource in Kubernetes:",
	}

	errorMatched := false
	for _, expectedError := range expectedErrors {
		if err.Error() == expectedError ||
			(expectedError == "failed to create Probe resource in Kubernetes:" &&
				strings.Contains(err.Error(), expectedError)) {
			errorMatched = true
			break
		}
	}

	if !errorMatched {
		t.Errorf("Expected error containing one of %v, got '%s'", expectedErrors, err.Error())
	}
}

func TestProbeManager_checkProbeCRDs_NoClient(t *testing.T) {
	pm := &ProbeManager{
		namespace:     "test",
		httpClient:    &http.Client{},
		kubeClient:    nil,
		probeAPIGroup: "",
	}

	pm.checkProbeCRDs()

	if pm.probeAPIGroup != "" {
		t.Error("probeAPIGroup should be empty when kubeClient is nil")
	}
}
